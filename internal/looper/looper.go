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

func Start(ctx context.Context, promptFile, tasksFile string) error {
	prompt, err := LoadPrompt(promptFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %s", err)
	}

	tasklist, err := LoadTasks(tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		fmt.Println("Some tasks failed previous runs:")
		printTasks(os.Stdout, tasklist)
		return err
	}
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %s", err)
	}

	if err := Run(ctx, prompt, tasklist, tasksFile, nil); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

// DryRun validates tasksFile with the same checks Start uses and reports on
// it to w without launching claude or writing any file.
func DryRun(w io.Writer, tasksFile string) error {
	tasklist, err := LoadTasks(tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		_, _ = fmt.Fprintln(w, "Some tasks failed previous runs:")
		printTasks(w, tasklist)
		return nil
	}
	if err != nil {
		return err
	}

	printTasks(w, tasklist)
	_, _ = fmt.Fprintf(w, "%s is valid\n", tasksFile)
	return nil
}

// printTasks writes a summary table of tl's tasks to w: id, state (green when
// completed, red when failed), and name, with failed rows flagged for review.
func printTasks(w io.Writer, tl *tasks.TaskList) {
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
		emoji, color, note := "  ", "", ""
		switch task.State {
		case tasks.FailedState:
			emoji, color = "❌", colorRed
			note = "  " + colorRed + "← Needs review!" + colorRst
		case tasks.CompletedState:
			emoji, color = "✅", colorGrn
		}
		_, _ = fmt.Fprintf(w, "%s %*d  %s%-*s%s  %s%s\n", emoji, idWidth, task.ID, color, stateWidth, strings.ToUpper(task.State), colorRst, task.Name, note)
	}
}

// LoadTasks reads and parses the tasks file at path. When the file parses but
// any task is failed, it returns the list and ErrFailedTasks.
func LoadTasks(path string) (*tasks.TaskList, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("tasks file %q is not accessible: %w", path, err)
	}

	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("yaml tasks file could not be read: %w", err)
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

// LoadPrompt reads the prompt file at path and returns it trimmed; an empty or
// whitespace-only prompt is an error.
func LoadPrompt(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("prompt file %q is not accessible: %w", path, err)
	}

	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("the prompt file could not be read: %w", err)
	}

	if len(bytes) == 0 {
		return "", errors.New("the prompt file is empty")
	}

	prompt := strings.TrimSpace(string(bytes))

	if prompt == "" {
		return "", errors.New("the prompt file is just whitespace")
	}

	return string(prompt), nil

}

// Run runs the loop over tl. When report is non-nil it receives a copy of
// each task's progress and, last, a RunDone event carrying Run's error.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile string, report func(Event)) error {
	err := runLoop(ctx, prompt, tl, tasksFile, report)
	if report != nil {
		report(Event{Kind: RunDone, Err: err})
	}
	return err
}

func runLoop(ctx context.Context, p string, tl *tasks.TaskList, tasksFile string, report func(Event)) error {
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

		state, errMsg, err := runTaskPlain(ctx, p, *task)
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

// runTaskPlain runs one task as plain mode always has: the combined prompt
// echoed to stdout, claude's stdout teed to the terminal, stderr inherited.
// It returns the task's outcome, or an error when ctx was cancelled.
func runTaskPlain(ctx context.Context, p string, task tasks.Task) (state, errMsg string, err error) {
	const claude = "claude"
	const print = "--print"
	const skipPermissions = "--dangerously-skip-permissions"

	// formatting it to make it easier to read by a human
	prompt := fmt.Sprintf("%s\n\n%s\n", p, task.String())

	fmt.Println(prompt)

	cmd := exec.CommandContext(ctx, claude, print, skipPermissions)
	configureProcessTree(cmd)
	cmd.Stdin = strings.NewReader(prompt)

	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()

	if runErr != nil && ctx.Err() != nil {
		// Cancelled by SIGINT/SIGTERM: leave the task's state and the
		// tasks file untouched so a re-run picks up where it left off.
		return "", "", fmt.Errorf("task %d: %s failed: %w", task.ID, task.Name, runErr)
	}

	state, errMsg = outcome(runErr, lastResultLine(out.String()))
	return state, errMsg, nil
}
