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

func Start(ctx context.Context, promptFile, tasksFile string) error {
	prompt, err := getPrompt(promptFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %s", err)
	}

	tasklist, err := getTasks(tasksFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %s", err)
	}

	for _, task := range tasklist.Tasks {
		if task.State == tasks.FailedState {
			fmt.Println("Some tasks failed previous runs:")
			printTasks(os.Stdout, tasklist)
			return errors.New("fix the failed tasks and set their state to pending before running")
		}
	}

	if err := runLoop(ctx, prompt, tasklist, tasksFile); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

// DryRun validates tasksFile with the same checks Start uses and reports on
// it to w without launching claude or writing any file.
func DryRun(w io.Writer, tasksFile string) error {
	tasklist, err := getTasks(tasksFile)
	if err != nil {
		return err
	}

	for _, task := range tasklist.Tasks {
		if task.State == tasks.FailedState {
			_, _ = fmt.Fprintln(w, "Some tasks failed previous runs:")
			printTasks(w, tasklist)
			return nil
		}
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

		var out bytes.Buffer
		cmd.Stdout = io.MultiWriter(os.Stdout, &out)
		cmd.Stderr = os.Stderr

		runErr := cmd.Run()

		if runErr != nil && ctx.Err() != nil {
			// Cancelled by SIGINT/SIGTERM: leave the task's state and the
			// tasks file untouched so a re-run picks up where it left off.
			return fmt.Errorf("task %d: %s failed: %w", task.ID, task.Name, runErr)
		}

		state, errMsg := outcome(runErr, lastResultLine(out.String()))
		task.State = state
		task.Error = errMsg

		if state == tasks.CompletedState {
			if err := tasks.SaveTasks(tasksFile, *tl); err != nil {
				return fmt.Errorf("task %d: %s: failed to save task state: %w", task.ID, task.Name, err)
			}
			continue
		}

		runFailure := fmt.Errorf("task %d: %s failed: %s", task.ID, task.Name, errMsg)
		if saveErr := tasks.SaveTasks(tasksFile, *tl); saveErr != nil {
			return errors.Join(runFailure, saveErr)
		}
		return runFailure
	}

	return nil
}
