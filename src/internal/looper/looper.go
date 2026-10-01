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
// that set what a session may do: SandboxArgs or BypassArgs. commit is the
// --commit flag: each completed task is committed when gralph runs in a git
// work tree, which must then be clean.
func Start(ctx context.Context, promptFile, tasksFile, gateTimeout string, sessionArgs []string, commit bool) error {
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

	repo, err := repoFor(os.Stdout, tasksFile, commit)
	if err != nil {
		return err
	}

	if err := Run(ctx, prompt, tasklist, tasksFile, gateTimeout, sessionArgs, repo, nil); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

// DryRun validates tasksFile with the same checks Start uses and reports on
// it to w without launching claude or writing any file. It also lists the
// timeout each gate would run under, given gateTimeout as for Start, names
// sandboxFile, when not empty, as the settings a run would use, and, when
// commit is set, applies Start's work tree check and names the tree.
func DryRun(w io.Writer, tasksFile, gateTimeout, sandboxFile string, commit bool) error {
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

	repo, err := repoFor(w, tasksFile, commit)
	if err != nil {
		return err
	}

	if repo != nil {
		repoRoot := repo.Root()
		_, _ = fmt.Fprintf(w, "commit: %s\n", repoRoot)
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
			gateName := firstLine(gate.Cmd)
			_, _ = fmt.Fprintf(w, "task %d gate: %s: %s (%s)\n", task.ID, gateName, limit, source)
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
		idText := fmt.Sprint(task.ID)
		idWidth = max(idWidth, len(idText))
	}
	stateWidth := len(tasks.CompletedState)

	_, _ = fmt.Fprintf(w, "   %*s  %-*s  NAME\n", idWidth, "ID", stateWidth, "STATE")
	idRule := strings.Repeat("-", idWidth)
	stateRule := strings.Repeat("-", stateWidth)
	nameRule := strings.Repeat("-", 4)
	_, _ = fmt.Fprintf(w, "   %s  %s  %s\n", idRule, stateRule, nameRule)
	for _, task := range tl.Tasks {
		emoji, color, reset, note := "  ", "", "", ""
		switch task.State {
		case tasks.FailedState:
			emoji, color, reset = "❌", colorRed, colorRst
			note = "  " + colorRed + "← Needs review!" + colorRst
		case tasks.CompletedState:
			emoji, color, reset = "✅", colorGrn, colorRst
		}
		stateText := strings.ToUpper(task.State)
		_, _ = fmt.Fprintf(w, "%s %*d  %s%-*s%s  %s%s\n", emoji, idWidth, task.ID, color, stateWidth, stateText, reset, task.Name, note)
	}
}

// LoadTasks reads and parses the tasks file at path. When the file parses but
// any task is failed, it returns the list and ErrFailedTasks.
func LoadTasks(path string) (*tasks.TaskList, error) {
	cleanPath := filepath.Clean(path)
	bytes, err := os.ReadFile(cleanPath)
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
	cleanPath := filepath.Clean(path)
	bytes, err := os.ReadFile(cleanPath)
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
// sessionArgs is as for Start. repo, when not nil, is where each completed
// task is committed.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) error {
	err := runLoop(ctx, prompt, tl, tasksFile, gateTimeout, sessionArgs, repo, report)
	if report != nil {
		report(Event{Kind: RunDone, Err: err})
	}

	return err
}

func runLoop(ctx context.Context, p string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) error {
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

		state, errMsg, err := runTask(ctx, p, *task, gateTimeout, sessionArgs, repo, report)
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

// runTask runs one task's session on the path report selects, then, only
// while the task is still completed, its gates and the commit into repo. It
// returns the task's outcome, or an error when ctx was cancelled.
func runTask(ctx context.Context, p string, task tasks.Task, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) (state, errMsg string, err error) {
	if report == nil {
		state, errMsg, err = runTaskPlain(ctx, p, task, sessionArgs)
	} else {
		state, errMsg, err = runTaskStream(ctx, p, task, sessionArgs, report)
	}

	if err != nil || state != tasks.CompletedState {
		return state, errMsg, err
	}

	state, errMsg, err = runGates(ctx, task, gateTimeout, report)
	if err != nil || state != tasks.CompletedState || repo == nil {
		return state, errMsg, err
	}

	return repo.commit(ctx, task, report)
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

	runErr := cmd.Run()
	output := out.String()

	return finishTask(ctx, task, runErr, output)
}

// claudeCmd builds the claude command for task, in its own process group
// with the combined prompt on stdin, and also returns that prompt text. The
// argv is --print, then the stream-json flags when stream is set, then
// sessionArgs.
func claudeCmd(ctx context.Context, p string, task tasks.Task, sessionArgs []string, stream bool) (*exec.Cmd, string) {
	taskText := task.String()
	prompt := fmt.Sprintf("%s\n\n%s\n", p, taskText)
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

	lastLine := lastResultLine(output)
	state, errMsg = outcome(runErr, lastLine)
	return state, errMsg, nil
}
