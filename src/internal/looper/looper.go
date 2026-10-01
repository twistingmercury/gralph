package looper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// ErrFailedTasks is returned by LoadTasks, along with the parsed list, when
// any task in the file is failed.
var ErrFailedTasks = errors.New("fix the failed tasks and set their state to pending before running")

// Start runs the loop in plain mode. gateTimeout is the --gate-timeout value as
// given, or "" when the flag was not passed. sessionArgs is the claude flags
// that set what a session may do: SandboxArgs or BypassArgs.
func Start(ctx context.Context, promptFile, tasksFile, gateTimeout string, sessionArgs []string) error {
	prompt, err := LoadPrompt(promptFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %w", err)
	}

	tasklist, err := LoadTasksReport(os.Stdout, tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		return err
	}

	if err != nil {
		return fmt.Errorf("failed to start loop runner: %w", err)
	}

	if err := Run(ctx, prompt, tasklist, tasksFile, gateTimeout, sessionArgs, nil); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

// DryRun validates tasksFile with the same checks Start uses and reports on
// it to w without launching claude or writing any file. It also lists the
// timeout each gate would run under, given gateTimeout as for Start, and
// names sandboxFile, when not empty, as the settings a run would use.
func DryRun(w io.Writer, tasksFile, gateTimeout, sandboxFile string) error {
	tasklist, err := LoadTasksReport(w, tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		return nil
	}

	if err != nil {
		return err
	}

	PrintTasks(w, tasklist)
	printGateLimits(w, tasklist, gateTimeout)
	if sandboxFile != "" {
		_, _ = fmt.Fprintf(w, "sandbox settings: %s\n", sandboxFile)
	}

	_, _ = fmt.Fprintf(w, "%s is valid\n", tasksFile)
	return nil
}

// printGateLimits writes one line per gate with the timeout it would run
// under and where that timeout came from. It writes nothing for a list with
// no gates.
func printGateLimits(w io.Writer, tl *tasks.TaskList, override string) {
	for _, task := range tl.Tasks {
		for _, gate := range task.Gates {
			limit, source := gateLimit(gate, override)
			_, _ = fmt.Fprintf(w, "task %d gate: %s: %s (%s)\n", task.ID, firstLine(gate.Cmd), limit, source)
		}
	}
}

// PrintTasks writes a summary table of tl's tasks to w: id, state (green when
// completed, red when failed), and name, with failed rows flagged for review.
func PrintTasks(w io.Writer, tl *tasks.TaskList) {
	const colorRed = "\033[1;91m"
	const colorGrn = "\033[92m"
	const colorRst = "\033[0m"

	idWidth := len("ID")
	for _, task := range tl.Tasks {
		idWidth = max(idWidth, len(fmt.Sprint(task.ID)))
	}
	stateWidth := len(tasks.CompletedState)

	_, _ = fmt.Fprintf(w, "   %*s  %-*s  NAME\n", idWidth, "ID", stateWidth, "STATE")
	_, _ = fmt.Fprintf(w, "   %s  %s  %s\n", strings.Repeat("-", idWidth), strings.Repeat("-", stateWidth), strings.Repeat("-", 4))
	for _, task := range tl.Tasks {
		emoji, color, reset, note := "  ", "", "", ""
		switch task.State {
		case tasks.FailedState:
			emoji, color, reset = "❌", colorRed, colorRst
			note = "  " + colorRed + "← Needs review!" + colorRst
		case tasks.CompletedState:
			emoji, color, reset = "✅", colorGrn, colorRst
		}
		_, _ = fmt.Fprintf(w, "%s %*d  %s%-*s%s  %s%s\n", emoji, idWidth, task.ID, color, stateWidth, strings.ToUpper(task.State), reset, task.Name, note)
	}
}

// LoadTasks reads and parses the tasks file at path. When the file parses but
// any task is failed, it returns the list and ErrFailedTasks.
func LoadTasks(path string) (*tasks.TaskList, error) {
	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("tasks file %q is not accessible: %w", path, err)
	}

	taskList, err := tasks.ParseTasks(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tasks yaml: %w", err)
	}

	for _, task := range taskList.Tasks {
		if task.State == tasks.FailedState {
			return &taskList, ErrFailedTasks
		}
	}

	return &taskList, nil
}

// LoadTasksReport loads tasksFile and, when any task is failed, writes the
// failed-tasks notice and table to w; err is then ErrFailedTasks.
func LoadTasksReport(w io.Writer, tasksFile string) (*tasks.TaskList, error) {
	tl, err := LoadTasks(tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		_, _ = fmt.Fprintln(w, "Some tasks failed previous runs:")
		PrintTasks(w, tl)
	}

	return tl, err
}

// LoadPrompt reads the prompt file at path and returns it trimmed; an empty or
// whitespace-only prompt is an error.
func LoadPrompt(path string) (string, error) {
	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("prompt file %q is not accessible: %w", path, err)
	}

	if len(bytes) == 0 {
		return "", errors.New("the prompt file is empty")
	}

	prompt := strings.TrimSpace(string(bytes))

	if prompt == "" {
		return "", errors.New("the prompt file is just whitespace")
	}

	return prompt, nil
}

// Run runs the loop over tl. When report is non-nil it receives a copy of
// each task's progress and, last, a RunDone event carrying Run's error;
// claude then runs with stream-json output and its activity is reported
// live. report may be called from more than one goroutine. gateTimeout, when
// not empty, replaces every gate's timeout for this run; it is never saved.
// sessionArgs is as for Start.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, report func(Event)) error {
	err := runLoop(ctx, prompt, tl, tasksFile, gateTimeout, sessionArgs, report)
	if report != nil {
		report(Event{Kind: RunDone, Err: err})
	}

	return err
}

func runLoop(ctx context.Context, p string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, report func(Event)) error {
	for i := range tl.Tasks {
		task := &tl.Tasks[i]

		if task.State == tasks.CompletedState {
			if report == nil {
				fmt.Printf("task %d: %s already completed, skipping\n", task.ID, task.Name)
			}

			continue
		}

		if report != nil {
			report(Event{Kind: TaskStarted, Task: *task})
		}

		state, errMsg, err := runTask(ctx, p, *task, gateTimeout, sessionArgs, report)
		if err != nil {
			return err
		}

		task.State = state
		task.Error = errMsg

		if state == tasks.CompletedState {
			if err := tasks.SaveTasks(tasksFile, *tl); err != nil {
				return fmt.Errorf("task %d: %s: failed to save task state: %w", task.ID, task.Name, err)
			}

			if report != nil {
				report(Event{Kind: TaskFinished, Task: *task})
			}

			continue
		}

		runFailure := fmt.Errorf("task %d: %s failed: %s", task.ID, task.Name, errMsg)
		if saveErr := tasks.SaveTasks(tasksFile, *tl); saveErr != nil {
			return errors.Join(runFailure, saveErr)
		}

		if report != nil {
			report(Event{Kind: TaskFinished, Task: *task, Err: runFailure})
		}

		return runFailure
	}

	return nil
}

// runTask runs one task's session on the path report selects and, only when
// the session completed, the task's gates. It returns the task's outcome, or
// an error when ctx was cancelled.
func runTask(ctx context.Context, p string, task tasks.Task, gateTimeout string, sessionArgs []string, report func(Event)) (state, errMsg string, err error) {
	if report == nil {
		state, errMsg, err = runTaskPlain(ctx, p, task, sessionArgs)
	} else {
		state, errMsg, err = runTaskStream(ctx, p, task, sessionArgs, report)
	}

	if err != nil || state != tasks.CompletedState {
		return state, errMsg, err
	}

	return runGates(ctx, task, gateTimeout, report)
}

// runTaskPlain runs one task as plain mode always has: the combined prompt
// echoed to stdout, claude's stdout teed to the terminal, stderr inherited.
// It returns the task's outcome, or an error when ctx was cancelled.
func runTaskPlain(ctx context.Context, p string, task tasks.Task, sessionArgs []string) (state, errMsg string, err error) {
	cmd, prompt := claudeCmd(ctx, p, task, sessionArgs, false)

	fmt.Println(prompt)

	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = os.Stderr

	return finishTask(ctx, task, cmd.Run(), out.String())
}

// claudeCmd builds the claude command for task, in its own process group
// with the combined prompt on stdin, and also returns that prompt text. The
// argv is --print, then the stream-json flags when stream is set, then
// sessionArgs.
func claudeCmd(ctx context.Context, p string, task tasks.Task, sessionArgs []string, stream bool) (*exec.Cmd, string) {
	prompt := fmt.Sprintf("%s\n\n%s\n", p, task.String())
	cmd := exec.CommandContext(ctx, "claude", "--print")
	if stream {
		cmd.Args = append(cmd.Args, "--output-format", "stream-json", "--verbose")
	}

	cmd.Args = append(cmd.Args, sessionArgs...)
	configureProcessTree(cmd)
	cmd.Stdin = strings.NewReader(prompt)
	return cmd, prompt
}

// finishTask turns claude's exit and output into the task's outcome, or an
// error when ctx was cancelled.
func finishTask(ctx context.Context, task tasks.Task, runErr error, output string) (state, errMsg string, err error) {
	if runErr != nil && ctx.Err() != nil {
		// Cancelled by SIGINT/SIGTERM: leave the task's state and the
		// tasks file untouched so a re-run picks up where it left off.
		return "", "", fmt.Errorf("task %d: %s failed: %w", task.ID, task.Name, runErr)
	}

	state, errMsg = outcome(runErr, lastResultLine(output))
	return state, errMsg, nil
}
