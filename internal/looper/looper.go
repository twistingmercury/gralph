package looper

import (
	"context"
	"errors"
	"fmt"
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

	if err := runLoop(ctx, prompt, tasklist); err != nil {
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

func runLoop(ctx context.Context, p string, tl *tasks.TaskList) error {
	const claude = "claude"
	const print = "--print"
	const skipPermissions = "--dangerously-skip-permissions"

	for _, task := range tl.Tasks {
		// formatting it to make it easier to read by a human
		prompt := fmt.Sprintf("%s\n\n%s\n", p, task.String())

		fmt.Println(prompt)

		cmd := exec.CommandContext(ctx, claude, print, skipPermissions)
		configureProcessTree(cmd)
		cmd.Stdin = strings.NewReader(prompt)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("task %d: %s failed: %w", task.ID, task.Name, err)
		}
	}

	return nil
}
