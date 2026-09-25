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

// allowedTags are the tags yaml.v3 resolves on its own; any other tag,
// explicit or custom, is rejected before anything is decoded.
var allowedTags = map[string]bool{
	"!!str": true, "!!int": true, "!!float": true, "!!bool": true, "!!null": true,
	"!!map": true, "!!seq": true, "!!timestamp": true, "!!merge": true,
}

// ParseTasks parses and validates a task file. Any invalid element rejects the
// whole file; errors name the task as tasks[<index>] (id <id>) and the field.
func ParseTasks(yml []byte) (TaskList, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(yml, &doc); err != nil {
		return TaskList{}, fmt.Errorf("failed to parse yaml tasks: %w", err)
	}

	if err := checkTags(&doc); err != nil {
		return TaskList{}, err
	}

	var seq *yaml.Node
	if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		root := doc.Content[0].Content
		for i := 0; i+1 < len(root); i += 2 {
			if root[i].Value != "tasks" {
				continue
			}
			if seq != nil {
				return TaskList{}, errors.New("tasks: duplicate key")
			}
			seq = root[i+1]
		}
	}
	switch {
	case seq == nil:
		return TaskList{}, errors.New("tasks: is required")
	case seq.Kind != yaml.SequenceNode:
		return TaskList{}, errors.New("tasks: must be a sequence")
	case len(seq.Content) == 0:
		return TaskList{}, errors.New("tasks: must contain at least one task")
	}

	var taskList TaskList
	ids := make(map[int16]int)
	names := make(map[string]int)
	for i, el := range seq.Content {
		task, where, err := parseTask(i, el)
		if err != nil {
			return TaskList{}, err
		}
		if j, dup := ids[task.ID]; dup {
			return TaskList{}, fmt.Errorf("%s: id: duplicates tasks[%d]", where, j)
		}
		ids[task.ID] = i
		name := strings.TrimSpace(strings.ToLower(task.Name))
		if j, dup := names[name]; dup {
			return TaskList{}, fmt.Errorf("%s: name: duplicates the name of tasks[%d]", where, j)
		}
		names[name] = i
		taskList.Tasks = append(taskList.Tasks, task)
	}

	return taskList, nil
}

// parseTask checks element i of the tasks sequence and decodes it. It returns
// the task's location, tasks[<i>] (id <id>), for the caller's own errors.
func parseTask(i int, el *yaml.Node) (Task, string, error) {
	where := fmt.Sprintf("tasks[%d]", i)
	if el.Kind == yaml.AliasNode {
		el = el.Alias
	}
	if el.Kind != yaml.MappingNode {
		return Task{}, where, fmt.Errorf("%s: must be a mapping", where)
	}

	fields := make(map[string]*yaml.Node)
	for j := 0; j+1 < len(el.Content); j += 2 {
		key, value := el.Content[j].Value, el.Content[j+1]
		if _, dup := fields[key]; dup {
			return Task{}, where, fmt.Errorf("%s: %s: duplicate key", where, key)
		}
		if value.Kind == yaml.AliasNode {
			value = value.Alias
		}
		fields[key] = value
	}

	var id int16
	idNode := fields["id"]
	if idNode == nil {
		return Task{}, where, fmt.Errorf("%s: id: is required", where)
	}
	if idNode.Kind != yaml.ScalarNode || idNode.Tag != "!!int" || idNode.Decode(&id) != nil || id <= 0 {
		return Task{}, where, fmt.Errorf("%s: id: must be a positive integer no greater than 32767", where)
	}
	where = fmt.Sprintf("%s (id %d)", where, id)

	for _, field := range []string{"name", "prompt", "state", "error"} {
		node := fields[field]
		required := field == "name" || field == "prompt"
		switch {
		case node == nil && required:
			return Task{}, where, fmt.Errorf("%s: %s: is required", where, field)
		case node == nil:
		case node.Kind != yaml.ScalarNode || node.Tag != "!!str":
			return Task{}, where, fmt.Errorf("%s: %s: must be a string", where, field)
		case required && strings.TrimSpace(node.Value) == "":
			return Task{}, where, fmt.Errorf("%s: %s: must not be empty or whitespace", where, field)
		case field == "state" && node.Value != "" && node.Value != PendingState &&
			node.Value != CompletedState && node.Value != FailedState:
			return Task{}, where, fmt.Errorf("%s: state: must be pending, completed, or failed, got %q", where, node.Value)
		}
	}

	var task Task
	if err := el.Decode(&task); err != nil {
		return Task{}, where, fmt.Errorf("%s: %w", where, err)
	}
	if task.State == "" {
		task.State = PendingState
	}

	return task, where, nil
}

// checkTags rejects any node carrying a tag outside allowedTags.
func checkTags(n *yaml.Node) error {
	if n.Kind != yaml.DocumentNode && n.Kind != yaml.AliasNode && n.Kind != 0 && !allowedTags[n.Tag] {
		return fmt.Errorf("line %d: tag %s is not allowed", n.Line, n.Tag)
	}
	for _, c := range n.Content {
		if err := checkTags(c); err != nil {
			return err
		}
	}
	return nil
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
