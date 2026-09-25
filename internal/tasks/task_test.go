package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	yml := []byte(`tasks:
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

	want := TaskList{Tasks: []Task{
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
	yml := []byte(`tasks:
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
	yml := []byte(`tasks:
  - id: 1
    name: Only task
    agent: go software engineer
    checkpoint: ""
    prompt: Do it.
    state: pending
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)
	assert.Equal(t, TaskList{Tasks: []Task{{ID: 1, Name: "Only task", Prompt: "Do it.", State: PendingState}}}, got)
}

func TestParseTasks_MaxID(t *testing.T) {
	got, err := ParseTasks([]byte("tasks:\n  - {id: 32767, name: a, prompt: p, state: pending}\n"))
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
			got, err := ParseTasks([]byte("tasks:\n  - {id: 1, name: a, prompt: p, state: " + state + "}\n"))
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
		{name: "state omitted", yml: "tasks:\n  - {id: 1, name: a, prompt: p}\n"},
		{name: "state empty string", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: \"\"}\n"},
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
	yml := []byte(`tasks:
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
	got, err := ParseTasks([]byte("tasks:\n  - {id: 1, name: \" Build \", prompt: \"  do it  \", state: pending}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, " Build ", got.Tasks[0].Name)
	assert.Equal(t, "  do it  ", got.Tasks[0].Prompt)
}

func TestParseTasks_Errors(t *testing.T) {
	const valid = "tasks:\n  - {id: 1, name: a, prompt: p}\n"

	tests := []struct {
		name    string
		yml     string
		wantErr string
	}{
		// File-level errors never name a task index or id.
		{name: "malformed yaml", yml: "tasks:\n  - id: 1\n   name: bad indent\n", wantErr: "failed to parse yaml tasks"},
		{name: "empty input", yml: "", wantErr: "tasks: is required"},
		{name: "comment-only input", yml: "# nothing\n", wantErr: "tasks: is required"},
		{name: "missing tasks key", yml: "items:\n  - {id: 1, name: a, prompt: p}\n", wantErr: "tasks: is required"},
		{name: "root is a sequence", yml: "- {id: 1, name: a, prompt: p}\n", wantErr: "tasks: is required"},
		{name: "null tasks", yml: "tasks:\n", wantErr: "tasks: must be a sequence"},
		{name: "tilde tasks", yml: "tasks: ~\n", wantErr: "tasks: must be a sequence"},
		{name: "scalar tasks", yml: "tasks: nope\n", wantErr: "tasks: must be a sequence"},
		{name: "mapping tasks", yml: "tasks: {id: 1, name: a, prompt: p}\n", wantErr: "tasks: must be a sequence"},
		{name: "empty tasks sequence", yml: "tasks: []\n", wantErr: "tasks: must contain at least one task"},
		{name: "duplicate tasks key", yml: valid + "tasks:\n  - {id: 2, name: b, prompt: p}\n", wantErr: "tasks: duplicate key"},

		// Tags outside the core set are rejected anywhere in the document.
		{name: "python object tag", yml: "tasks:\n  - {id: 1, name: !!python/object:os.system a, prompt: p}\n", wantErr: "line 2: tag !!python/object:os.system is not allowed"},
		{name: "custom tag on an element", yml: valid + "  - !foo {id: 2, name: b, prompt: p}\n", wantErr: "line 3: tag !foo is not allowed"},
		{name: "custom tag on an unknown key", yml: "tasks:\n  - {id: 1, name: a, prompt: p, extra: !foo x}\n", wantErr: "tag !foo is not allowed"},
		{name: "custom tag outside tasks", yml: valid + "meta: !foo x\n", wantErr: "tag !foo is not allowed"},
		{name: "explicit binary tag", yml: "tasks:\n  - {id: 1, name: !!binary YQ==, prompt: p}\n", wantErr: "tag !!binary is not allowed"},

		// Every element must be a mapping, even among valid tasks.
		{name: "bare dash element", yml: valid + "  -\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "null element", yml: valid + "  - null\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "tilde element", yml: valid + "  - ~\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "empty mapping element", yml: valid + "  - {}\n", wantErr: "tasks[1]: id: is required"},
		{name: "empty sequence element", yml: valid + "  - []\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "empty string element", yml: valid + "  - \"\"\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "scalar element", yml: valid + "  - just a string\n", wantErr: "tasks[1]: must be a mapping"},
		{name: "invalid first element before a valid one", yml: "tasks:\n  - ~\n  - {id: 1, name: a, prompt: p}\n", wantErr: "tasks[0]: must be a mapping"},

		// id
		{name: "missing id", yml: "tasks:\n  - {name: a, prompt: p}\n", wantErr: "tasks[0]: id: is required"},
		{name: "null id", yml: "tasks:\n  - {id: ~, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer no greater than 32767"},
		{name: "float id", yml: "tasks:\n  - {id: 1.5, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "quoted id", yml: "tasks:\n  - {id: \"1\", name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "word id", yml: "tasks:\n  - {id: one, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "zero id", yml: "tasks:\n  - {id: 0, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "negative id", yml: "tasks:\n  - {id: -3, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer"},
		{name: "id overflows int16", yml: "tasks:\n  - {id: 32768, name: a, prompt: p}\n", wantErr: "tasks[0]: id: must be a positive integer no greater than 32767"},
		{name: "duplicate id", yml: valid + "  - {id: 1, name: b, prompt: p}\n", wantErr: "tasks[1] (id 1): id: duplicates tasks[0]"},
		{name: "aliased task repeats its id", yml: "tasks:\n  - &t {id: 1, name: a, prompt: p}\n  - *t\n", wantErr: "tasks[1] (id 1): id: duplicates tasks[0]"},
		{name: "duplicate id key within a task", yml: "tasks:\n  - {id: 1, id: 2, name: a, prompt: p}\n", wantErr: "tasks[0]: id: duplicate key"},

		// name
		{name: "missing name", yml: "tasks:\n  - {id: 1, prompt: p}\n", wantErr: "tasks[0] (id 1): name: is required"},
		{name: "null name", yml: "tasks:\n  - {id: 1, name: ~, prompt: p}\n", wantErr: "tasks[0] (id 1): name: must be a string"},
		{name: "integer name", yml: "tasks:\n  - {id: 1, name: 42, prompt: p}\n", wantErr: "tasks[0] (id 1): name: must be a string"},
		{name: "sequence name", yml: "tasks:\n  - {id: 1, name: [a], prompt: p}\n", wantErr: "tasks[0] (id 1): name: must be a string"},
		{name: "empty name", yml: "tasks:\n  - {id: 1, name: \"\", prompt: p}\n", wantErr: "tasks[0] (id 1): name: must not be empty or whitespace"},
		{name: "whitespace-only name", yml: "tasks:\n  - {id: 1, name: \" \\t \", prompt: p}\n", wantErr: "tasks[0] (id 1): name: must not be empty or whitespace"},
		{name: "duplicate name", yml: valid + "  - {id: 2, name: a, prompt: p}\n", wantErr: "tasks[1] (id 2): name: duplicates the name of tasks[0]"},
		{name: "duplicate name differing only by case", yml: "tasks:\n  - {id: 1, name: Build, prompt: p}\n  - {id: 2, name: bUILD, prompt: p}\n", wantErr: "tasks[1] (id 2): name: duplicates the name of tasks[0]"},
		{name: "duplicate name differing only by surrounding whitespace", yml: "tasks:\n  - {id: 1, name: Build, prompt: p}\n  - {id: 2, name: \"  build\\t\", prompt: p}\n", wantErr: "tasks[1] (id 2): name: duplicates the name of tasks[0]"},

		// prompt
		{name: "missing prompt", yml: "tasks:\n  - {id: 1, name: a}\n", wantErr: "tasks[0] (id 1): prompt: is required"},
		{name: "null prompt", yml: "tasks:\n  - {id: 1, name: a, prompt: ~}\n", wantErr: "tasks[0] (id 1): prompt: must be a string"},
		{name: "mapping prompt", yml: "tasks:\n  - {id: 1, name: a, prompt: {x: y}}\n", wantErr: "tasks[0] (id 1): prompt: must be a string"},
		{name: "empty prompt", yml: "tasks:\n  - {id: 1, name: a, prompt: \"\"}\n", wantErr: "tasks[0] (id 1): prompt: must not be empty or whitespace"},
		{name: "whitespace-only prompt", yml: "tasks:\n  - {id: 1, name: a, prompt: \" \\t\\n \"}\n", wantErr: "tasks[0] (id 1): prompt: must not be empty or whitespace"},
		{name: "blank prompt on a later task", yml: valid + "  - {id: 2, name: b, prompt: \"  \"}\n", wantErr: "tasks[1] (id 2): prompt: must not be empty or whitespace"},

		// state
		{name: "null state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: ~}\n", wantErr: "tasks[0] (id 1): state: must be a string"},
		{name: "state key with no value", yml: "tasks:\n  - id: 1\n    name: a\n    prompt: p\n    state:\n", wantErr: "tasks[0] (id 1): state: must be a string"},
		{name: "whitespace-only state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: \" \"}\n", wantErr: `tasks[0] (id 1): state: must be pending, completed, or failed, got " "`},
		{name: "padded state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: \" pending \"}\n", wantErr: `state: must be pending, completed, or failed, got " pending "`},
		{name: "capitalized state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: Completed}\n", wantErr: `tasks[0] (id 1): state: must be pending, completed, or failed, got "Completed"`},
		{name: "upper-case state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: PENDING}\n", wantErr: `got "PENDING"`},
		{name: "abandoned state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: abandoned}\n", wantErr: `tasks[0] (id 1): state: must be pending, completed, or failed, got "abandoned"`},
		{name: "state typo", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: complete}\n", wantErr: `got "complete"`},
		{name: "boolean state", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: true}\n", wantErr: "tasks[0] (id 1): state: must be a string"},
		{name: "invalid state on a later task", yml: valid + "  - {id: 2, name: b, prompt: p, state: done}\n", wantErr: `tasks[1] (id 2): state: must be pending, completed, or failed, got "done"`},

		// error
		{name: "non-string error", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: failed, error: [x]}\n", wantErr: "tasks[0] (id 1): error: must be a string"},
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
		"tasks:\n  - id: 1\n   name: bad indent\n",
		"tasks:\n  - {id: 1, name: !foo a, prompt: p}\n",
		"tasks: nope\n",
		"tasks: []\n",
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
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n  - {id: 2, name: \"\", prompt: p}\n",
			wantErr: "tasks[0] (id 1): state:",
		},
		{
			name:    "id before name",
			yml:     "tasks:\n  - {id: 0, name: \"\", prompt: p}\n",
			wantErr: "tasks[0]: id:",
		},
		{
			name:    "name before prompt",
			yml:     "tasks:\n  - {id: 1, name: \"\", prompt: \"\"}\n",
			wantErr: "tasks[0] (id 1): name:",
		},
		{
			name:    "prompt before state",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: \"\", state: bogus}\n",
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
	got, err := ParseTasks([]byte("tasks:\n  - {id: !!int 1, name: !!str 42, prompt: p}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, "42", got.Tasks[0].Name)
}

func TestParseTasks_ReadsErrorField(t *testing.T) {
	got, err := ParseTasks([]byte("tasks:\n  - {id: 1, name: a, prompt: p, state: failed, error: boom}\n"))
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, "boom", got.Tasks[0].Error)
}

func TestSaveTasks_ErrorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	want := TaskList{Tasks: []Task{
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

	want := TaskList{Tasks: []Task{
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

	want := TaskList{Tasks: []Task{
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

	want := TaskList{Tasks: []Task{{ID: 1, Name: "Only", Prompt: "p", State: CompletedState}}}
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

	err := SaveTasks(target, TaskList{Tasks: []Task{{ID: 1, Name: "a", Prompt: "p", State: PendingState}}})
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
	err := SaveTasks(path, TaskList{Tasks: []Task{{ID: 1, Name: "a", Prompt: "p", State: PendingState}}})
	require.Error(t, err)

	_, statErr := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "no .tmp file should remain after a failed save")
}
