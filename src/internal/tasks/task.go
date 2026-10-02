package tasks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	PendingState   = "pending"
	FailedState    = "failed"
	CompletedState = "completed"
)

const durationHint = "must be a duration string such as 90s or 10m"

// Gate is one command gralph runs itself after each task's session reports
// completed. It is never sent to the session.
type Gate struct {
	Cmd     string `yaml:"cmd"`
	Timeout string `yaml:"timeout,omitempty"`
}

type Task struct {
	ID     int16  `yaml:"id"`
	Name   string `yaml:"name"`
	Prompt string `yaml:"prompt"`
	State  string `yaml:"state"`
	Error  string `yaml:"error,omitempty"`
}

// TaskList is a whole task file. Gates is a pointer so a save can tell a file
// with no gates key (not decided yet; the wizard asks) from gates: [] (decided:
// none). It comes first so the list is written above the tasks.
type TaskList struct {
	Gates *[]Gate `yaml:"gates,omitempty"`
	Tasks []Task  `yaml:"tasks"`
}

// GateList is the file's gates, empty when the key is absent.
func (tl TaskList) GateList() []Gate {
	if tl.Gates == nil {
		return nil
	}

	return *tl.Gates
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

	seq, gatesNode, err := rootNodes(&doc)
	if err != nil {
		return TaskList{}, err
	}

	// Gates are checked before the tasks so a file with an error in each
	// reports the same one whatever order its keys are in.
	gates, err := decodeGates(gatesNode)
	if err != nil {
		return TaskList{}, err
	}

	taskList := TaskList{Gates: gates}
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
	lowered := strings.ToLower(task.Name)
	name := strings.TrimSpace(lowered)
	if j, dup := names[name]; dup {
		return fmt.Errorf("%s: name: duplicates the name of tasks[%d]", where, j)
	}

	names[name] = i
	return nil
}

// rootNodes walks the document's top-level mapping once and returns the
// non-empty sequence under its single tasks key and the node under its gates
// key, nil when the file has none.
func rootNodes(doc *yaml.Node) (seq, gates *yaml.Node, err error) {
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, errors.New("tasks: is required")
	}

	root := doc.Content[0].Content
	for i := 0; i+1 < len(root); i += 2 {
		switch root[i].Value {
		case "tasks":
			if seq != nil {
				return nil, nil, errors.New("tasks: duplicate key")
			}

			seq = root[i+1]
		case "gates":
			if gates != nil {
				return nil, nil, errors.New("gates: duplicate key")
			}

			gates = root[i+1]
		}
	}

	switch {
	case seq == nil:
		return nil, nil, errors.New("tasks: is required")
	case seq.Kind != yaml.SequenceNode:
		return nil, nil, errors.New("tasks: must be a sequence")
	case len(seq.Content) == 0:
		return nil, nil, errors.New("tasks: must contain at least one task")
	}

	return seq, gates, nil
}

// decodeGates returns a non-nil slice for gates: [] so a save writes the key
// back and the file keeps saying its author decided on no gates.
func decodeGates(node *yaml.Node) (*[]Gate, error) {
	if node == nil {
		return nil, nil
	}

	if err := checkGates(node); err != nil {
		return nil, err
	}

	gates := []Gate{}
	if err := node.Decode(&gates); err != nil {
		return nil, fmt.Errorf("gates: %w", err)
	}

	return &gates, nil
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

	// An error rather than a quietly dropped key: dropping it would leave an
	// old file running every task with no checks at all (ADR-019).
	if fields["gates"] != nil {
		return Task{}, where, fmt.Errorf("%s: gates: gates are set once for the whole file now, as a top-level gates: list; see the HOWTO", where)
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

// checkGates checks the file's gates sequence before it is decoded. A
// malformed gate must reject the file: decoding would silently drop what it
// does not recognize, and the next save would then erase it.
func checkGates(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}

	if node.Kind != yaml.SequenceNode {
		return errors.New("gates: must be a sequence")
	}

	for j, el := range node.Content {
		gateWhere := fmt.Sprintf("gates[%d]", j)
		if err := checkGate(gateWhere, el); err != nil {
			return err
		}
	}

	return nil
}

// checkGate allows only the cmd and timeout keys so a misspelled or invented
// option is an error instead of a gate that quietly does less than its author
// meant.
func checkGate(where string, el *yaml.Node) error {
	if el.Kind == yaml.AliasNode {
		el = el.Alias
	}

	if el.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: must be a mapping", where)
	}

	fields, err := taskFields(where, el)
	if err != nil {
		return err
	}

	for j := 0; j+1 < len(el.Content); j += 2 {
		if key := el.Content[j].Value; key != "cmd" && key != "timeout" {
			return fmt.Errorf("%s: %s: unknown key; a gate has only cmd and timeout", where, key)
		}
	}

	cmd := fields["cmd"]
	switch {
	case cmd == nil:
		return fmt.Errorf("%s: cmd: is required", where)
	case cmd.Kind != yaml.ScalarNode || cmd.Tag != "!!str":
		return fmt.Errorf("%s: cmd: must be a string", where)
	case strings.TrimSpace(cmd.Value) == "":
		return fmt.Errorf("%s: cmd: must not be empty or whitespace", where)
	}

	return checkGateTimeout(where, fields["timeout"])
}

// checkGateTimeout checks the tag as well as the text because `timeout: 30`
// is a YAML integer, and a bare number has no unit for ParseDuration to read.
// The text is stored as written; the looper parses it again when the gate runs.
func checkGateTimeout(where string, node *yaml.Node) error {
	if node == nil {
		return nil
	}

	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("%s: timeout: %s", where, durationHint)
	}

	if _, err := ParseTimeout(node.Value); err != nil {
		return fmt.Errorf("%s: timeout: %w", where, err)
	}

	return nil
}

// ParseTimeout parses a gate timeout as written in the tasks file or on the
// --gate-timeout flag, so both places apply one set of rules. The error text
// is the bare problem; callers prefix where the value came from.
func ParseTimeout(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New(durationHint)
	}

	if d <= 0 {
		return 0, errors.New("must be greater than zero")
	}

	return d, nil
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
	encoded := buf.Bytes()
	if err := os.WriteFile(tmpPath, encoded, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save tasks to %q: %w", cleanPath, err)
	}

	if err := os.Rename(tmpPath, cleanPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to save tasks to %q: %w", cleanPath, err)
	}

	return nil
}
