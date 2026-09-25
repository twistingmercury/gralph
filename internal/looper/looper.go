package looper

import (
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

func Start(ctx context.Context, promptFile, tasksFile string) error {
	prompt, err := getPrompt(promptFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %s", err)
	}

	tasklist, err := getTasks(tasksFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %s", err)
	}

	if err := runLoop(ctx, prompt, tasklist, tasksFile); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

func getTasks(path string) (*tasks.TaskList, error) {
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

	return &taskList, nil
}

func getPrompt(path string) (string, error) {
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

func runLoop(ctx context.Context, p string, tl *tasks.TaskList, tasksFile string) error {
	const claude = "claude"
	const print = "--print"
	const skipPermissions = "--dangerously-skip-permissions"

	for i := range tl.Tasks {
		task := &tl.Tasks[i]

		if task.State == tasks.CompletedState {
			fmt.Printf("task %d: %s already completed, skipping\n", task.ID, task.Name)
			continue
		}

		// formatting it to make it easier to read by a human
		prompt := fmt.Sprintf("%s\n\n%s\n", p, task.String())

		fmt.Println(prompt)

		cmd := exec.CommandContext(ctx, claude, print, skipPermissions)
		configureProcessTree(cmd)
		cmd.Stdin = strings.NewReader(prompt)

		lastLine := newLastLineWriter()
		cmd.Stdout = io.MultiWriter(os.Stdout, lastLine)
		cmd.Stderr = os.Stderr

		runErr := cmd.Run()
		lastLine.Finalize()

		if runErr != nil && ctx.Err() != nil {
			// Cancelled by SIGINT/SIGTERM: leave the task's state and the
			// tasks file untouched so a re-run picks up where it left off.
			return fmt.Errorf("task %d: %s failed: %w", task.ID, task.Name, runErr)
		}

		state, errMsg := outcome(runErr, lastLine.LastLine())
		task.State = state
		task.Error = errMsg

		if state == tasks.CompletedState {
			if err := tasks.SaveTasks(tasksFile, *tl); err != nil {
				return fmt.Errorf("task %d: %s: failed to save task state: %w", task.ID, task.Name, err)
			}
			continue
		}

		runFailure := newTaskFailure(task.ID, task.Name, errMsg, runErr)
		if saveErr := tasks.SaveTasks(tasksFile, *tl); saveErr != nil {
			return errors.Join(runFailure, saveErr)
		}
		return runFailure
	}

	return nil
}

// taskFailure is the error runLoop returns for a failed task. Its message is
// always built from the same errMsg that was saved to task.Error, so the
// returned error and the persisted state never disagree; Unwrap exposes the
// underlying exec error (if any), so errors.Is/As (e.g. *exec.ExitError,
// exec.ErrNotFound) stay reachable through it even when errMsg came from the
// session's JSON result line instead of runErr.
type taskFailure struct {
	msg    string
	runErr error
}

func newTaskFailure(id int16, name, errMsg string, runErr error) error {
	return &taskFailure{
		msg:    fmt.Sprintf("task %d: %s failed: %s", id, name, errMsg),
		runErr: runErr,
	}
}

func (e *taskFailure) Error() string { return e.msg }

func (e *taskFailure) Unwrap() error { return e.runErr }
