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
	ID     int16  `yaml:"id"`
	Name   string `yaml:"name"`
	Prompt string `yaml:"prompt"`
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

	seq, err := tasksSeq(&doc)
	if err != nil {
		return TaskList{}, err
	}

	var taskList TaskList
	ids := make(map[int16]int)
	names := make(map[string]int)
	for i, el := range seq.Content {
		task, where, err := parseTask(i, el)
		if err != nil {
			return TaskList{}, err
		}

		if err := checkDuplicate(ids, names, i, task, where); err != nil {
			return TaskList{}, err
		}

		taskList.Tasks = append(taskList.Tasks, task)
	}

	return taskList, nil
}

// checkDuplicate rejects a task whose id or name repeats an earlier one,
// recording both so later tasks are checked against it. Names compare
// ignoring case and surrounding whitespace.
func checkDuplicate(ids map[int16]int, names map[string]int, i int, task Task, where string) error {
	if j, dup := ids[task.ID]; dup {
		return fmt.Errorf("%s: id: duplicates tasks[%d]", where, j)
	}

	ids[task.ID] = i
	name := strings.TrimSpace(strings.ToLower(task.Name))
	if j, dup := names[name]; dup {
		return fmt.Errorf("%s: name: duplicates the name of tasks[%d]", where, j)
	}

	names[name] = i
	return nil
}

// tasksSeq returns the non-empty sequence under the document's single
// top-level tasks key.
func tasksSeq(doc *yaml.Node) (*yaml.Node, error) {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("tasks: is required")
	}

	var seq *yaml.Node
	root := doc.Content[0].Content
	for i := 0; i+1 < len(root); i += 2 {
		if root[i].Value != "tasks" {
			continue
		}

		if seq != nil {
			return nil, errors.New("tasks: duplicate key")
		}

		seq = root[i+1]
	}

	switch {
	case seq == nil:
		return nil, errors.New("tasks: is required")
	case seq.Kind != yaml.SequenceNode:
		return nil, errors.New("tasks: must be a sequence")
	case len(seq.Content) == 0:
		return nil, errors.New("tasks: must contain at least one task")
	}

	return seq, nil
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

	fields, err := taskFields(where, el)
	if err != nil {
		return Task{}, where, err
	}

	id, err := taskID(where, fields)
	if err != nil {
		return Task{}, where, err
	}

	where = fmt.Sprintf("%s (id %d)", where, id)
	if err := checkStringFields(where, fields); err != nil {
		return Task{}, where, err
	}

	task, err := decodeTask(where, el)
	return task, where, err
}

// taskFields maps each key of the task mapping to its value, with aliases
// resolved so the field checks see the node that will be decoded.
func taskFields(where string, el *yaml.Node) (map[string]*yaml.Node, error) {
	fields := make(map[string]*yaml.Node)
	for j := 0; j+1 < len(el.Content); j += 2 {
		key, value := el.Content[j].Value, el.Content[j+1]
		if _, dup := fields[key]; dup {
			return nil, fmt.Errorf("%s: %s: duplicate key", where, key)
		}

		if value.Kind == yaml.AliasNode {
			value = value.Alias
		}

		fields[key] = value
	}

	return fields, nil
}

// taskID checks the node's tag as well as its decoded value so that only a
// YAML integer is accepted as an id.
func taskID(where string, fields map[string]*yaml.Node) (int16, error) {
	var id int16
	idNode := fields["id"]
	if idNode == nil {
		return 0, fmt.Errorf("%s: id: is required", where)
	}

	if idNode.Kind != yaml.ScalarNode || idNode.Tag != "!!int" || idNode.Decode(&id) != nil || id <= 0 {
		return 0, fmt.Errorf("%s: id: must be a positive integer no greater than 32767", where)
	}

	return id, nil
}

// checkStringFields checks the string fields in a fixed order so the first
// error reported does not depend on key order in the file.
func checkStringFields(where string, fields map[string]*yaml.Node) error {
	for _, field := range []string{"name", "prompt", "state", "error"} {
		if err := checkStringField(where, field, fields[field]); err != nil {
			return err
		}
	}

	return nil
}

func checkStringField(where, field string, node *yaml.Node) error {
	required := field == "name" || field == "prompt"
	switch {
	case node == nil && required:
		return fmt.Errorf("%s: %s: is required", where, field)
	case node == nil:
	case node.Kind != yaml.ScalarNode || node.Tag != "!!str":
		return fmt.Errorf("%s: %s: must be a string", where, field)
	case required && strings.TrimSpace(node.Value) == "":
		return fmt.Errorf("%s: %s: must not be empty or whitespace", where, field)
	case field == "state" && node.Value != "" && node.Value != PendingState &&
		node.Value != CompletedState && node.Value != FailedState:
		return fmt.Errorf("%s: state: must be pending, completed, or failed, got %q", where, node.Value)
	}

	return nil
}

func decodeTask(where string, el *yaml.Node) (Task, error) {
	var task Task
	if err := el.Decode(&task); err != nil {
		return Task{}, fmt.Errorf("%s: %w", where, err)
	}

	if task.State == "" {
		task.State = PendingState
	}

	return task, nil
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
