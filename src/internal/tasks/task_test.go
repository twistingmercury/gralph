package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sharedYAML = "shared:\n  prompt: shared prompt\n"

// testShared is what sharedYAML parses to.
var testShared = Shared{Prompt: "shared prompt"}

func TestTaskString(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want string
	}{
		{
			name: "exact format",
			task: Task{ID: 1, Name: "First task", Prompt: "Do it."},
			want: "1: First task\n\nDo it.",
		},
		{
			name: "trailing newline of a block-scalar prompt is trimmed",
			task: Task{ID: 7, Name: "Second task", Prompt: "Objective:\nDo the second thing.\n"},
			want: "7: Second task\n\nObjective:\nDo the second thing.",
		},
		{
			name: "interior indentation of the prompt's first line is preserved",
			task: Task{ID: 2, Name: "Indented", Prompt: "  Step one\nStep two\n"},
			want: "2: Indented\n\n  Step one\nStep two",
		},
		{
			name: "empty prompt",
			task: Task{ID: 3, Name: "No prompt"},
			want: "3: No prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.task.String())
		})
	}
}

func TestParseTasks_Valid(t *testing.T) {
	yml := []byte(sharedYAML + `tasks:
  - id: 1
    name: First task
    prompt: |
      Objective:
      Do the first thing.

      Verification:
      - Command: go test ./...
    state: pending
  - id: 7
    name: Second task
    prompt: Do the second thing.
    state: completed
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)

	want := TaskList{Shared: testShared, Tasks: []Task{
		{
			ID:     1,
			Name:   "First task",
			Prompt: "Objective:\nDo the first thing.\n\nVerification:\n- Command: go test ./...\n",
			State:  PendingState,
		},
		{ID: 7, Name: "Second task", Prompt: "Do the second thing.", State: CompletedState},
	}}
	assert.Equal(t, want, got, "block-scalar prompts must keep their newlines verbatim")
}

func TestParseTasks_PreservesFileOrder(t *testing.T) {
	yml := []byte(sharedYAML + `tasks:
  - {id: 30, name: c, prompt: p, state: pending}
  - {id: 10, name: a, prompt: p, state: completed}
  - {id: 20, name: b, prompt: p, state: failed}
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)

	ids := make([]int16, 0, len(got.Tasks))
	for _, task := range got.Tasks {
		ids = append(ids, task.ID)
	}
	assert.Equal(t, []int16{30, 10, 20}, ids, "IDs are identity, not order")
}

func TestParseTasks_IgnoresUnknownFields(t *testing.T) {
	yml := []byte(sharedYAML + `tasks:
  - id: 1
    name: Only task
    agent: go software engineer
    checkpoint: ""
    prompt: Do it.
    state: pending
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)
	assert.Equal(t, TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "Only task", Prompt: "Do it.", State: PendingState}}}, got)
}

func TestParseTasks_MaxID(t *testing.T) {
	got, err := ParseTasks([]byte(sharedYAML + "tasks:\n  - {id: 32767, name: a, prompt: p, state: pending}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, int16(32767), got.Tasks[0].ID)
}

func TestStateConstants(t *testing.T) {
	// These strings are the on-disk YAML contract; renaming one breaks every
	// existing task file.
	assert.Equal(t, "pending", PendingState)
	assert.Equal(t, "completed", CompletedState)
	assert.Equal(t, "failed", FailedState)
}

func TestParseTasks_AcceptsEveryValidState(t *testing.T) {
	for _, state := range []string{PendingState, CompletedState, FailedState} {
		t.Run(state, func(t *testing.T) {
			got, err := ParseTasks([]byte(sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: " + state + "}\n"))
			require.NoError(t, err)
			require.Len(t, got.Tasks, 1)
			assert.Equal(t, state, got.Tasks[0].State)
		})
	}
}

func TestParseTasks_EmptyStateDefaultsToPending(t *testing.T) {
	// A task file does not have to spell out `state: pending` for new work.
	tests := []struct {
		name string
		yml  string
	}{
		{name: "state omitted", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p}\n"},
		{name: "state empty string", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: \"\"}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTasks([]byte(tt.yml))
			require.NoError(t, err)
			require.Len(t, got.Tasks, 1)
			assert.Equal(t, PendingState, got.Tasks[0].State)
		})
	}
}

func TestParseTasks_DefaultsOnlyTheEmptyStates(t *testing.T) {
	yml := []byte(sharedYAML + `tasks:
  - {id: 1, name: a, prompt: p, state: completed}
  - {id: 2, name: b, prompt: p}
  - {id: 3, name: c, prompt: p, state: failed}
  - {id: 4, name: d, prompt: p}
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)

	states := make([]string, 0, len(got.Tasks))
	for _, task := range got.Tasks {
		states = append(states, task.State)
	}
	assert.Equal(t, []string{CompletedState, PendingState, FailedState, PendingState}, states)
}

func TestParseTasks_DoesNotAlterNameOrPrompt(t *testing.T) {
	// Trimming is for validation only; the stored values stay as written.
	got, err := ParseTasks([]byte(sharedYAML + "tasks:\n  - {id: 1, name: \" Build \", prompt: \"  do it  \", state: pending}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, " Build ", got.Tasks[0].Name)
	assert.Equal(t, "  do it  ", got.Tasks[0].Prompt)
}

func TestParseTasks_Errors(t *testing.T) {
	const valid = sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p}\n"

	tests := []struct {
		name    string
		yml     string
		wantErr string
	}{
		// File-level errors never name a task index or id.
		{name: "malformed yaml", yml: sharedYAML + "tasks:\n  - id: 1\n   name: bad indent\n", wantErr: "failed to parse yaml tasks"},
		{name: "empty input", yml: "", wantErr: "shared: is required"},
		{name: "comment-only input", yml: "# nothing\n", wantErr: "shared: is required"},
		{name: "missing tasks key", yml: sharedYAML, wantErr: "tasks: is required"},
		{name: "unknown top-level key", yml: sharedYAML + "items:\n  - {id: 1, name: a, prompt: p}\n", wantErr: "items: unknown key; a task file has only shared and tasks"},
		{name: "root is a sequence", yml: "- {id: 1, name: a, prompt: p}\n", wantErr: "shared: is required"},
		{name: "null tasks", yml: sharedYAML + "tasks:\n", wantErr: "tasks: must be a sequence"},
		{name: "tilde tasks", yml: sharedYAML + "tasks: ~\n", wantErr: "tasks: must be a sequence"},
		{name: "scalar tasks", yml: sharedYAML + "tasks: nope\n", wantErr: "tasks: must be a sequence"},
		{name: "mapping tasks", yml: sharedYAML + "tasks: {id: 1, name: a, prompt: p}\n", wantErr: "tasks: must be a sequence"},
		{name: "empty tasks sequence", yml: sharedYAML + "tasks: []\n", wantErr: "tasks: must contain at least one task"},
		{name: "duplicate tasks key", yml: valid + "tasks:\n  - {id: 2, name: b, prompt: p}\n", wantErr: "tasks: duplicate key"},

		// Tags outside the core set are rejected anywhere in the document.
		{name: "python object tag", yml: sharedYAML + "tasks:\n  - {id: 1, name: !!python/object:os.system a, prompt: p}\n", wantErr: "line 4: tag !!python/object:os.system is not allowed"},
		{name: "custom tag on an element", yml: valid + "  - !foo {id: 2, name: b, prompt: p}\n", wantErr: "line 5: tag !foo is not allowed"},
		{name: "custom tag on an unknown key", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, extra: !foo x}\n", wantErr: "tag !foo is not allowed"},
		{name: "custom tag outside tasks", yml: valid + "meta: !foo x\n", wantErr: "tag !foo is not allowed"},
		{name: "explicit binary tag", yml: sharedYAML + "tasks:\n  - {id: 1, name: !!binary YQ==, prompt: p}\n", wantErr: "tag !!binary is not allowed"},

		// Every element must be a mapping, even among valid tasks.
		{name: "bare dash element", yml: valid + "  -\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "null element", yml: valid + "  - null\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "tilde element", yml: valid + "  - ~\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "empty mapping element", yml: valid + "  - {}\n", wantErr: "tasks[1]: id: is required"},
		{name: "empty sequence element", yml: valid + "  - []\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "empty string element", yml: valid + "  - \"\"\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "scalar element", yml: valid + "  - just a string\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "invalid first element before a valid one", yml: sharedYAML + "tasks:\n  - ~\n  - {id: 1, name: a, prompt: p}\n", wantErr: "tasks[0]: must be a mapping"},

		// id
		{name: "missing id", yml: sharedYAML + "tasks:\n  - {name: a, prompt: p}\n", wantErr: "tasks[0]: id: is required"},
		{name: "null id", yml: sharedYAML + "tasks:\n  - {id: ~, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer no greater than 32767"},
		{name: "float id", yml: sharedYAML + "tasks:\n  - {id: 1.5, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "quoted id", yml: sharedYAML + "tasks:\n  - {id: \"1\", name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "word id", yml: sharedYAML + "tasks:\n  - {id: one, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "zero id", yml: sharedYAML + "tasks:\n  - {id: 0, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "negative id", yml: sharedYAML + "tasks:\n  - {id: -3, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "id overflows int16", yml: sharedYAML + "tasks:\n  - {id: 32768, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer no greater than 32767"},
		{name: "duplicate id", yml: valid + "  - {id: 1, name: b, prompt: p}\n", wantErr: "tasks[1] (id 1): id: duplicates tasks[0]"},
		{name: "aliased task repeats its id", yml: sharedYAML + "tasks:\n  - &t {id: 1, name: a, prompt: p}\n  - *t\n", wantErr: "tasks[1] (id 1): id: duplicates tasks[0]"},
		{name: "duplicate id key within a task", yml: sharedYAML + "tasks:\n  - {id: 1, id: 2, name: a, prompt: p}\n", wantErr: "tasks[0]: id: duplicate key"},

		// name
		{name: "missing name", yml: sharedYAML + "tasks:\n  - {id: 1, prompt: p}\n", wantErr: "tasks[0] (id 1): name: is required"},
		{name: "null name", yml: sharedYAML + "tasks:\n  - {id: 1, name: ~, prompt: p}\n", wantErr: "tasks[0] (id 1): name: must be a string"},
		{name: "integer name", yml: sharedYAML + "tasks:\n  - {id: 1, name: 42, prompt: p}\n", wantErr: "tasks[0] (id 1): name: must be a string"},
		{name: "sequence name", yml: sharedYAML + "tasks:\n  - {id: 1, name: [a], prompt: p}\n", wantErr: "tasks[0] (id 1): name: must be a string"},
		{name: "empty name", yml: sharedYAML + "tasks:\n  - {id: 1, name: \"\", prompt: p}\n", wantErr: "tasks[0] (id 1): name: must not be empty or whitespace"},
		{name: "whitespace-only name", yml: sharedYAML + "tasks:\n  - {id: 1, name: \" \\t \", prompt: p}\n", wantErr: "tasks[0] (id 1): name: must not be empty or whitespace"},
		{name: "duplicate name", yml: valid + "  - {id: 2, name: a, prompt: p}\n", wantErr: "tasks[1] (id 2): name: duplicates the name of tasks[0]"},
		{name: "duplicate name differing only by case", yml: sharedYAML + "tasks:\n  - {id: 1, name: Build, prompt: p}\n  - {id: 2, name: bUILD, prompt: p}\n", wantErr: "tasks[1] (id 2): name: duplicates the name of tasks[0]"},
		{name: "duplicate name differing only by surrounding whitespace", yml: sharedYAML + "tasks:\n  - {id: 1, name: Build, prompt: p}\n  - {id: 2, name: \"  build\\t\", prompt: p}\n", wantErr: "tasks[1] (id 2): name: duplicates the name of tasks[0]"},

		// prompt
		{name: "missing prompt", yml: sharedYAML + "tasks:\n  - {id: 1, name: a}\n", wantErr: "tasks[0] (id 1): prompt: is required"},
		{name: "null prompt", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: ~}\n", wantErr: "tasks[0] (id 1): prompt: must be a string"},
		{name: "mapping prompt", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: {x: y}}\n", wantErr: "tasks[0] (id 1): prompt: must be a string"},
		{name: "empty prompt", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: \"\"}\n", wantErr: "tasks[0] (id 1): prompt: must not be empty or whitespace"},
		{name: "whitespace-only prompt", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: \" \\t\\n \"}\n", wantErr: "tasks[0] (id 1): prompt: must not be empty or whitespace"},
		{name: "blank prompt on a later task", yml: valid + "  - {id: 2, name: b, prompt: \"  \"}\n", wantErr: "tasks[1] (id 2): prompt: must not be empty or whitespace"},

		// state
		{name: "null state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: ~}\n", wantErr: "tasks[0] (id 1): state: must be a string"},
		{name: "state key with no value", yml: sharedYAML + "tasks:\n  - id: 1\n    name: a\n    prompt: p\n    state:\n", wantErr: "tasks[0] (id 1): state: must be a string"},
		{name: "whitespace-only state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: \" \"}\n", wantErr: `tasks[0] (id 1): state: must be pending, completed, or failed, got " "`},
		{name: "padded state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: \" pending \"}\n", wantErr: `state: must be pending, completed, or failed, got " pending "`},
		{name: "capitalized state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: Completed}\n", wantErr: `tasks[0] (id 1): state: must be pending, completed, or failed, got "Completed"`},
		{name: "upper-case state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: PENDING}\n", wantErr: `got "PENDING"`},
		{name: "abandoned state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: abandoned}\n", wantErr: `tasks[0] (id 1): state: must be pending, completed, or failed, got "abandoned"`},
		{name: "state typo", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: complete}\n", wantErr: `got "complete"`},
		{name: "boolean state", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: true}\n", wantErr: "tasks[0] (id 1): state: must be a string"},
		{name: "invalid state on a later task", yml: valid + "  - {id: 2, name: b, prompt: p, state: done}\n", wantErr: `tasks[1] (id 2): state: must be pending, completed, or failed, got "done"`},

		// error
		{name: "non-string error", yml: sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: failed, error: [x]}\n", wantErr: "tasks[0] (id 1): error: must be a string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTasks([]byte(tt.yml))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, TaskList{}, got, "an error must return the zero TaskList, never a partial one")
		})
	}
}

// TestParseTasks_FileErrorsNameNoTask pins that syntax, tag, and top-level
// errors do not invent a task index or id.
func TestParseTasks_FileErrorsNameNoTask(t *testing.T) {
	for _, yml := range []string{
		sharedYAML + "tasks:\n  - id: 1\n   name: bad indent\n",
		sharedYAML + "tasks:\n  - {id: 1, name: !foo a, prompt: p}\n",
		sharedYAML + "tasks: nope\n",
		sharedYAML + "tasks: []\n",
	} {
		_, err := ParseTasks([]byte(yml))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "tasks[", "yaml %q", yml)
		assert.NotContains(t, err.Error(), "(id", "yaml %q", yml)
	}
}

// TestParseTasks_ReportsFirstInvalidTask pins that elements are checked in
// file order, and within one element id, name, prompt, then state.
func TestParseTasks_ReportsFirstInvalidTask(t *testing.T) {
	tests := []struct {
		name    string
		yml     string
		wantErr string
	}{
		{
			name:    "earlier element wins",
			yml:     sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n  - {id: 2, name: \"\", prompt: p}\n",
			wantErr: "tasks[0] (id 1): state:",
		},
		{
			name:    "id before name",
			yml:     sharedYAML + "tasks:\n  - {id: 0, name: \"\", prompt: p}\n",
			wantErr: "tasks[0]: id:",
		},
		{
			name:    "name before prompt",
			yml:     sharedYAML + "tasks:\n  - {id: 1, name: \"\", prompt: \"\"}\n",
			wantErr: "tasks[0] (id 1): name:",
		},
		{
			name:    "prompt before state",
			yml:     sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: \"\", state: bogus}\n",
			wantErr: "tasks[0] (id 1): prompt:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTasks([]byte(tt.yml))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestParseTasks_AcceptsExplicitCoreTags(t *testing.T) {
	got, err := ParseTasks([]byte(sharedYAML + "tasks:\n  - {id: !!int 1, name: !!str 42, prompt: p}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, "42", got.Tasks[0].Name)
}

func TestParseTasks_ReadsErrorField(t *testing.T) {
	got, err := ParseTasks([]byte(sharedYAML + "tasks:\n  - {id: 1, name: a, prompt: p, state: failed, error: boom}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, "boom", got.Tasks[0].Error)
}

func TestSaveTasks_ErrorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	want := TaskList{Shared: testShared, Tasks: []Task{
		{ID: 1, Name: "First", Prompt: "p", State: FailedState, Error: "exit status 1"},
	}}

	require.NoError(t, SaveTasks(path, want))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got, "the error message must round-trip verbatim")
}

func TestSaveTasks_OmitsEmptyError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	want := TaskList{Shared: testShared, Tasks: []Task{
		{ID: 1, Name: "First", Prompt: "p", State: CompletedState, Error: ""},
	}}
	require.NoError(t, SaveTasks(path, want))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "error:", "an empty error must be omitted from the saved yaml entirely")

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSaveTasks_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	want := TaskList{Shared: testShared, Tasks: []Task{
		{ID: 3, Name: "Third", Prompt: "Do the third thing.\nAcross\nmultiple lines.\n", State: FailedState},
		{ID: 1, Name: "First", Prompt: "Do the first thing.", State: PendingState},
		{ID: 2, Name: "Second", Prompt: "Do the second thing.", State: CompletedState},
	}}

	require.NoError(t, SaveTasks(path, want))

	_, statErr := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "no .tmp file should remain after a successful save")

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got, "ids, names, multi-line prompts, states, and file order must round-trip")
}

func TestSaveTasks_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(path, []byte("stale contents"), 0o600))

	want := TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "Only", Prompt: "p", State: CompletedState}}}
	require.NoError(t, SaveTasks(path, want))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSaveTasks_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.Mkdir(target, 0o700))

	err := SaveTasks(target, TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "a", Prompt: "p", State: PendingState}}})
	require.Error(t, err)

	_, statErr := os.Stat(target + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "no .tmp file should remain after a failed save")
}

func TestSaveTasks_TargetDirNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}

	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(roDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) })

	path := filepath.Join(roDir, "tasks.yaml")
	err := SaveTasks(path, TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "a", Prompt: "p", State: PendingState}}})
	require.Error(t, err)

	_, statErr := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "no .tmp file should remain after a failed save")
}

// The file itself stays writable; only the temporary file's name is taken, by
// a directory. A save that wrote to the target directly would therefore
// succeed and replace the contents, so this passes only when the write goes to
// a sibling file first. A read-only directory would not do: the release build
// runs the tests as root, which ignores directory permissions.
func TestSaveTasks_FailedSaveLeavesExistingFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")
	original := TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "Original", Prompt: "p", State: PendingState}}}
	require.NoError(t, SaveTasks(path, original))

	before, err := os.ReadFile(path)
	require.NoError(t, err)

	require.NoError(t, os.Mkdir(path+".tmp", 0o700))

	changed := TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "Changed", Prompt: "p", State: FailedState, Error: "boom"}}}
	err = SaveTasks(path, changed)
	require.Error(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a failed save must leave the existing task file byte for byte as it was")
}

// People edit the task file by hand between runs, so its layout is part of
// what a save promises; a round-trip through ParseTasks cannot see indentation.
func TestSaveTasks_WritesTwoSpaceIndent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")

	tl := TaskList{
		Shared: Shared{Prompt: "shared prompt", Gates: &[]Gate{
			{Cmd: "go test ./...", Timeout: "90s"},
			{Cmd: "echo one"},
		}},
		Tasks: []Task{
			{ID: 1, Name: "First", Prompt: "p", State: PendingState},
			{ID: 2, Name: "Second", Prompt: "q", State: FailedState, Error: "boom"},
		},
	}
	require.NoError(t, SaveTasks(path, tl))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	want := `shared:
  prompt: shared prompt
  gates:
    - cmd: go test ./...
      timeout: 90s
    - cmd: echo one
tasks:
  - id: 1
    name: First
    prompt: p
    state: pending
  - id: 2
    name: Second
    prompt: q
    state: failed
    error: boom
`
	assert.Equal(t, want, string(data), "the saved file must use a 2-space indent at every level")
}

func TestParseTasks_SharedGates(t *testing.T) {
	yml := "shared:\n  prompt: p\n  gates:\n    - cmd: make test\n      timeout: 10m\n    - cmd: go vet ./...\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n"
	tl, err := ParseTasks([]byte(yml))
	require.NoError(t, err)
	require.NotNil(t, tl.Shared.Gates)
	assert.Equal(t, []Gate{{Cmd: "make test", Timeout: "10m"}, {Cmd: "go vet ./..."}}, *tl.Shared.Gates)
}

func TestParseTasks_Shared(t *testing.T) {
	tl, err := ParseTasks([]byte("shared:\n  prompt: |\n    one\n\n    two\n\ntasks:\n  - id: 1\n    name: a\n    prompt: p\n"))
	require.NoError(t, err)
	assert.Equal(t, "one\n\ntwo", tl.Shared.Prompt)
	assert.Nil(t, tl.Shared.Gates)
}

func TestParseTasks_SharedErrors(t *testing.T) {
	task := "tasks:\n  - id: 1\n    name: a\n    prompt: p\n"
	cases := map[string]struct{ yml, want string }{
		"no shared":         {task, "shared: is required"},
		"shared not map":    {"shared: x\n" + task, "shared: must be a mapping"},
		"no prompt":         {"shared:\n  gates: []\n" + task, "shared.prompt: is required"},
		"blank prompt":      {"shared:\n  prompt: |\n    \n\n" + task, "shared.prompt: must not be empty or whitespace"},
		"prompt not string": {"shared:\n  prompt: 5\n" + task, "shared.prompt: must be a string"},
		"typo in shared":    {"shared:\n  prompt: p\n  promt: x\n" + task, "shared.promt: unknown key"},
		"unknown top level": {sharedYAML + "shard: x\n" + task, "shard: unknown key"},
		"old top gates":     {sharedYAML + "gates:\n  - cmd: x\n" + task, "gates: gates are set under shared now"},
		"bad gate":          {sharedYAML + "  gates:\n    - cmd: ''\n" + task, "shared.gates[0]: cmd: must not be empty or whitespace"},
		"duplicate shared":  {sharedYAML + sharedYAML + task, "shared: duplicate key"},
		"duplicate tasks":   {sharedYAML + task + task, "tasks: duplicate key"},
		"no tasks":          {sharedYAML, "tasks: is required"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseTasks([]byte(c.yml))
			require.Error(t, err)
			assert.ErrorContains(t, err, c.want)
			assert.Equal(t, TaskList{}, got, "an error must return the zero TaskList, never a partial one")
		})
	}
}

func TestSaveTasks_KeepsShared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")
	empty := []Gate{}
	in := TaskList{
		Shared: Shared{Prompt: "line one\n\nline two", Gates: &empty},
		Tasks:  []Task{{ID: 1, Name: "a", Prompt: "p", State: CompletedState}},
	}
	require.NoError(t, SaveTasks(path, in))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Less(t, strings.Index(string(data), "shared:"), strings.Index(string(data), "tasks:"))
	assert.Contains(t, string(data), "gates: []")

	out, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, in.Shared.Prompt, out.Shared.Prompt)
	require.NotNil(t, out.Shared.Gates)
	assert.Empty(t, out.GateList())
}

func TestParseTasks_ReadsGates(t *testing.T) {
	yml := []byte(`shared:
  prompt: p
  gates:
    - cmd: go test ./...
    - cmd: |
        test -z "$(gofmt -l .)"
tasks:
  - id: 1
    name: First
    prompt: p
  - id: 2
    name: Second
    prompt: p
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)
	require.Len(t, got.Tasks, 2)

	want := []Gate{{Cmd: "go test ./..."}, {Cmd: "test -z \"$(gofmt -l .)\"\n"}}
	assert.Equal(t, want, got.GateList(), "cmd text must be stored unaltered, in file order")
}

func TestParseTasks_ReadsGateTimeout(t *testing.T) {
	yml := []byte(`shared:
  prompt: p
  gates:
    - cmd: go test ./...
      timeout: 10m
    - cmd: echo hi
    - cmd: sleep 1
      timeout: 1h30m
tasks:
  - id: 1
    name: Gated
    prompt: p
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)

	want := []Gate{{Cmd: "go test ./...", Timeout: "10m"}, {Cmd: "echo hi"}, {Cmd: "sleep 1", Timeout: "1h30m"}}
	assert.Equal(t, want, got.GateList(), "timeout text must be stored as written")
}

func TestParseTasks_GatesAbsentVersusEmpty(t *testing.T) {
	absent, err := ParseTasks([]byte(sharedYAML + "tasks:\n  - id: 1\n    name: First\n    prompt: do it\n"))
	require.NoError(t, err)
	assert.Nil(t, absent.Shared.Gates, "no gates key means not decided")
	assert.Empty(t, absent.GateList())

	empty, err := ParseTasks([]byte("shared:\n  prompt: p\n  gates: []\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n"))
	require.NoError(t, err)
	require.NotNil(t, empty.Shared.Gates, "gates: [] means decided: none")
	assert.Empty(t, *empty.Shared.Gates)
	assert.Empty(t, empty.GateList())
}

// An old file's per-task gates must stop the run rather than be dropped:
// dropping them would run every task with no checks at all.
func TestParseTasks_TaskLevelGatesAreAnError(t *testing.T) {
	cases := map[string]string{
		"list":  sharedYAML + "tasks:\n  - id: 3\n    name: First\n    prompt: do it\n    gates:\n      - cmd: make test\n",
		"empty": sharedYAML + "tasks:\n  - id: 3\n    name: First\n    prompt: do it\n    gates: []\n",
	}
	for name, yml := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseTasks([]byte(yml))
			require.Error(t, err)
			assert.EqualError(t, err, "tasks[0] (id 3): gates: gates are set once for the whole file, as shared.gates; see the HOWTO")
			assert.Equal(t, TaskList{}, got, "an error must return the zero TaskList, never a partial one")
		})
	}
}

func TestTaskString_OmitsGates(t *testing.T) {
	task := Task{ID: 1, Name: "First task", Prompt: "Do it."}
	assert.Equal(t, "1: First task\n\nDo it.", task.String(), "gates are gralph's check and must never reach the session")
}

func TestParseTasks_GateErrors(t *testing.T) {
	tests := []struct {
		name    string
		gates   string
		wantErr string
	}{
		{name: "null gates", gates: "gates:", wantErr: "shared.gates: must be a sequence"},
		{name: "scalar gates", gates: "gates: go test ./...", wantErr: "shared.gates: must be a sequence"},
		{name: "mapping gates", gates: "gates: {cmd: go test ./...}", wantErr: "shared.gates: must be a sequence"},
		{name: "duplicate gates key", gates: "gates: []\ngates: []", wantErr: "shared: gates: duplicate key"},
		{name: "string element", gates: "gates: [go test ./...]", wantErr: "shared.gates[0]: must be a mapping"},
		{name: "missing cmd", gates: "gates: [{}]", wantErr: "shared.gates[0]: cmd: is required"},
		{name: "timeout without cmd", gates: "gates:\n  - timeout: 5m", wantErr: "shared.gates[0]: cmd: is required"},
		{name: "misspelled key", gates: "gates: [{command: a}]", wantErr: "shared.gates[0]: command: unknown key; a gate has only cmd and timeout"},
		{name: "extra key", gates: "gates: [{cmd: a, retries: 3}]", wantErr: "shared.gates[0]: retries: unknown key; a gate has only cmd and timeout"},
		{name: "duplicate cmd", gates: "gates: [{cmd: a, cmd: b}]", wantErr: "shared.gates[0]: cmd: duplicate key"},
		{name: "integer cmd", gates: "gates: [{cmd: 5}]", wantErr: "shared.gates[0]: cmd: must be a string"},
		{name: "null cmd", gates: "gates: [{cmd: }]", wantErr: "shared.gates[0]: cmd: must be a string"},
		{name: "blank cmd", gates: `gates: [{cmd: "  "}]`, wantErr: "shared.gates[0]: cmd: must not be empty or whitespace"},
		{name: "integer timeout", gates: "gates: [{cmd: a, timeout: 30}]", wantErr: "shared.gates[0]: timeout: must be a duration string such as 90s or 10m"},
		{name: "null timeout", gates: "gates: [{cmd: a, timeout: }]", wantErr: "shared.gates[0]: timeout: must be a duration string such as 90s or 10m"},
		{name: "sequence timeout", gates: "gates: [{cmd: a, timeout: [10m]}]", wantErr: "shared.gates[0]: timeout: must be a duration string such as 90s or 10m"},
		{name: "unparseable timeout", gates: "gates: [{cmd: a, timeout: soon}]", wantErr: "shared.gates[0]: timeout: must be a duration string such as 90s or 10m"},
		{name: "unitless timeout", gates: `gates: [{cmd: a, timeout: "30"}]`, wantErr: "shared.gates[0]: timeout: must be a duration string such as 90s or 10m"},
		{name: "blank timeout", gates: `gates: [{cmd: a, timeout: "  "}]`, wantErr: "shared.gates[0]: timeout: must be a duration string such as 90s or 10m"},
		{name: "zero timeout", gates: "gates: [{cmd: a, timeout: 0s}]", wantErr: "shared.gates[0]: timeout: must be greater than zero"},
		{name: "negative timeout", gates: "gates: [{cmd: a, timeout: -5m}]", wantErr: "shared.gates[0]: timeout: must be greater than zero"},
		{name: "duplicate timeout", gates: "gates: [{cmd: a, timeout: 1s, timeout: 2s}]", wantErr: "shared.gates[0]: timeout: duplicate key"},
		{name: "second gate invalid", gates: "gates: [{cmd: a}, {cmd: ''}]", wantErr: "shared.gates[1]: cmd: must not be empty or whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nested := "  " + strings.ReplaceAll(tt.gates, "\n", "\n  ")
			yml := "shared:\n  prompt: p\n" + nested + "\ntasks:\n  - id: 1\n    name: a\n    prompt: p\n"
			got, err := ParseTasks([]byte(yml))
			require.Error(t, err)
			assert.EqualError(t, err, tt.wantErr)
			assert.Equal(t, TaskList{}, got, "an error must return the zero TaskList, never a partial one")
		})
	}
}

// Gates are checked before the tasks so a file with an error in each always
// reports the same one, whatever order the keys are in.
func TestParseTasks_GateErrorReportedBeforeTaskError(t *testing.T) {
	yml := "shared:\n  prompt: p\n  gates: [{}]\ntasks:\n  - id: 1\n    prompt: p\n"
	_, err := ParseTasks([]byte(yml))
	assert.EqualError(t, err, "shared.gates[0]: cmd: is required")
}

func TestSaveTasks_KeepsGatesAbsentEmptyOrSet(t *testing.T) {
	cases := map[string]string{
		"absent": "shared:\n  prompt: p\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n    state: pending\n",
		"empty":  "shared:\n  prompt: p\n  gates: []\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n    state: pending\n",
		"set":    "shared:\n  prompt: p\n  gates:\n    - cmd: make test\n      timeout: 10m\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n    state: pending\n",
	}
	for name, yml := range cases {
		t.Run(name, func(t *testing.T) {
			tl, err := ParseTasks([]byte(yml))
			require.NoError(t, err)
			path := filepath.Join(t.TempDir(), "tasks.yaml")
			require.NoError(t, SaveTasks(path, tl))
			saved, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, yml, string(saved))
		})
	}
}

func TestSaveTasks_GatesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")

	want := TaskList{
		Shared: Shared{Prompt: "shared prompt", Gates: &[]Gate{
			{Cmd: "go test ./..."},
			{Cmd: "echo one\necho \"two\"\n"},
		}},
		Tasks: []Task{{ID: 1, Name: "First", Prompt: "p", State: PendingState}},
	}
	require.NoError(t, SaveTasks(path, want))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got, "gates must round-trip verbatim and in order")
}

func TestSaveTasks_OmitsAbsentGates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")

	require.NoError(t, SaveTasks(path, TaskList{Shared: testShared, Tasks: []Task{{ID: 1, Name: "First", Prompt: "p", State: PendingState}}}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "gates", "a file without a gates key must not gain one on save")
}

func TestSaveTasks_GateTimeoutKeptAsWrittenAndOmittedWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")

	want := TaskList{
		Shared: Shared{Prompt: "shared prompt", Gates: &[]Gate{
			{Cmd: "go test ./...", Timeout: "90s"},
			{Cmd: "echo one"},
		}},
		Tasks: []Task{{ID: 1, Name: "First", Prompt: "p", State: PendingState}},
	}
	require.NoError(t, SaveTasks(path, want))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), "timeout:"), "only the gate that set a timeout may carry the key")
	assert.Contains(t, string(data), "timeout: 90s", "the duration must not be normalized to 1m30s")

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr string
	}{
		{name: "minutes", in: "10m", want: 10 * time.Minute},
		{name: "compound", in: "1h30m", want: 90 * time.Minute},
		{name: "milliseconds", in: "200ms", want: 200 * time.Millisecond},
		{name: "unitless", in: "30", wantErr: "must be a duration string such as 90s or 10m"},
		{name: "blank", in: " ", wantErr: "must be a duration string such as 90s or 10m"},
		{name: "empty", in: "", wantErr: "must be a duration string such as 90s or 10m"},
		{name: "words", in: "soon", wantErr: "must be a duration string such as 90s or 10m"},
		{name: "zero", in: "0s", wantErr: "must be greater than zero"},
		{name: "negative", in: "-1m", wantErr: "must be greater than zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimeout(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
