package tasks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	PendingState   = "pending"
	FailedState    = "failed"
	CompletedState = "completed"
)

type Task struct {
	ID     int16  `yaml:"id" validate:"required"`
	Name   string `yaml:"name" validate:"required"`
	Prompt string `yaml:"prompt" validate:"required"`
	State  string `yaml:"state"`
	Error  string `yaml:"error,omitempty"`
}

type TaskList struct {
	Tasks []Task `yaml:"tasks"`
}

func (t Task) String() string {
	str := fmt.Sprintf("%d: %s\n\n%s", t.ID, t.Name, t.Prompt)
	return strings.TrimSpace(str)
}

func taskEmpty() Task {
	return Task{
		ID:     0,
		Name:   "",
		Prompt: "",
		State:  "",
	}
}

func ParseTasks(yml []byte) (TaskList, error) {
	var taskList TaskList
	err := yaml.Unmarshal(yml, &taskList)

	if err != nil {
		return TaskList{}, fmt.Errorf("failed to parse yaml tasks: %w", err)
	}

	if len(taskList.Tasks) == 0 {
		return TaskList{}, errors.New("the list of tasks is empty")
	}

	if ok, task := idsArePositive(taskList); !ok {
		return TaskList{}, fmt.Errorf("task named '%s' has an invalid ID of %d", task.Name, task.ID)
	}

	if ok, task := idsAreUnique(taskList); !ok {
		return TaskList{}, fmt.Errorf("task named '%s' duplicates id %d", task.Name, task.ID)
	}

	if ok, task := namesAreNotWhitespace(taskList); !ok {
		return TaskList{}, fmt.Errorf("task id %d name is empty or whitespace", task.ID)
	}

	if ok, task := namesAreUnique(taskList); !ok {
		return TaskList{}, fmt.Errorf("task id %d duplicates the task name '%s'", task.ID, task.Name)
	}

	if ok, task := promptsAreNotWhitespace(taskList); !ok {
		return TaskList{}, fmt.Errorf("task id %d prompt is empty or whitespace", task.ID)
	}

	normalizeState(&taskList)

	if ok, task := statesAreValid(taskList); !ok {
		return TaskList{}, fmt.Errorf("task id %d has an invalid state: %s", task.ID, task.State)
	}

	return taskList, nil
}

// SaveTasks writes tl to path as YAML via a temporary file that is renamed
// over path, so a failed save leaves the original file untouched.
func SaveTasks(path string, tl TaskList) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(tl); err != nil {
		return fmt.Errorf("failed to encode tasks: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to encode tasks: %w", err)
	}

	cleanPath := filepath.Clean(path)
	tmpPath := cleanPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save tasks to %q: %w", cleanPath, err)
	}
	if err := os.Rename(tmpPath, cleanPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save tasks to %q: %w", cleanPath, err)
	}

	return nil
}

func idsAreUnique(t TaskList) (bool, Task) {
	seen := make(map[int16]struct{})

	for _, task := range t.Tasks {
		if _, exists := seen[task.ID]; exists {
			return false, task
		}
		seen[task.ID] = struct{}{}
	}

	return true, taskEmpty()
}

func namesAreUnique(t TaskList) (bool, Task) {
	seen := make(map[string]struct{})

	for _, task := range t.Tasks {
		name := strings.TrimSpace(strings.ToLower(task.Name))
		if _, exists := seen[name]; exists {
			return false, task
		}
		seen[name] = struct{}{}
	}

	return true, taskEmpty()
}

func namesAreNotWhitespace(t TaskList) (bool, Task) {
	for _, task := range t.Tasks {
		prompt := strings.TrimSpace(task.Name)
		if len(prompt) == 0 {
			return false, task
		}
	}

	return true, taskEmpty()
}

func promptsAreNotWhitespace(t TaskList) (bool, Task) {
	for _, task := range t.Tasks {
		prompt := strings.TrimSpace(task.Prompt)
		if len(prompt) == 0 {
			return false, task
		}
	}

	return true, taskEmpty()
}

func idsArePositive(t TaskList) (bool, Task) {
	for _, task := range t.Tasks {
		if task.ID <= 0 {
			return false, task
		}
	}

	return true, taskEmpty()
}

func statesAreValid(t TaskList) (bool, Task) {
	for _, task := range t.Tasks {
		switch {
		case task.State != PendingState && task.State != CompletedState && task.State != FailedState:
			return false, task
		}
	}

	return true, taskEmpty()
}

func normalizeState(t *TaskList) {
	for i, task := range t.Tasks {
		state := strings.ToLower(strings.TrimSpace(task.State))
		if len(state) == 0 {
			state = PendingState
		}
		t.Tasks[i].State = state
	}
}
