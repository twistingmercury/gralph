package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

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
  - {id: 20, name: b, prompt: p, state: abandoned}
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
	assert.Equal(t, "abandoned", AbandonedState)
}

func TestParseTasks_AcceptsEveryValidState(t *testing.T) {
	for _, state := range []string{PendingState, CompletedState, AbandonedState} {
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
		{name: "state null", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: ~}\n"},
		{name: "state key with no value", yml: "tasks:\n  - id: 1\n    name: a\n    prompt: p\n    state:\n"},
		{name: "state whitespace only", yml: "tasks:\n  - {id: 1, name: a, prompt: p, state: \" \\t \"}\n"},
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

func TestParseTasks_NormalizesStateCaseAndWhitespace(t *testing.T) {
	tests := []struct {
		written string
		want    string
	}{
		{written: "Pending", want: PendingState},
		{written: "PENDING", want: PendingState},
		{written: "\" pending \"", want: PendingState},
		{written: "Completed", want: CompletedState},
		{written: "\"\\tCOMPLETED\\n\"", want: CompletedState},
		{written: "Abandoned", want: AbandonedState},
		{written: "\"  aBaNdOnEd  \"", want: AbandonedState},
	}

	for _, tt := range tests {
		t.Run(tt.written, func(t *testing.T) {
			got, err := ParseTasks([]byte("tasks:\n  - {id: 1, name: a, prompt: p, state: " + tt.written + "}\n"))
			require.NoError(t, err)
			require.Len(t, got.Tasks, 1)
			assert.Equal(t, tt.want, got.Tasks[0].State, "the stored state is the normalized constant")
		})
	}
}

func TestParseTasks_DefaultsOnlyTheEmptyStates(t *testing.T) {
	yml := []byte(`tasks:
  - {id: 1, name: a, prompt: p, state: completed}
  - {id: 2, name: b, prompt: p}
  - {id: 3, name: c, prompt: p, state: abandoned}
  - {id: 4, name: d, prompt: p}
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)

	states := make([]string, 0, len(got.Tasks))
	for _, task := range got.Tasks {
		states = append(states, task.State)
	}
	assert.Equal(t, []string{CompletedState, PendingState, AbandonedState, PendingState}, states)
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
	tests := []struct {
		name    string
		yml     string
		wantErr string
	}{
		{
			name:    "empty input",
			yml:     "",
			wantErr: "the list of tasks is empty",
		},
		{
			name:    "empty tasks sequence",
			yml:     "tasks: []\n",
			wantErr: "the list of tasks is empty",
		},
		{
			name:    "missing tasks key",
			yml:     "items:\n  - {id: 1, name: a, prompt: p}\n",
			wantErr: "the list of tasks is empty",
		},
		{
			name:    "malformed yaml",
			yml:     "tasks:\n  - id: 1\n   name: bad indent\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "tasks is not a sequence",
			yml:     "tasks: nope\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "non-numeric id",
			yml:     "tasks:\n  - {id: one, name: a, prompt: p}\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "id overflows int16",
			yml:     "tasks:\n  - {id: 32768, name: a, prompt: p}\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "quoted string id",
			yml:     "tasks:\n  - {id: \"3\", name: a, prompt: p}\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "task entry is a scalar",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\n  - just a string\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "duplicate id key within a task",
			yml:     "tasks:\n  - {id: 1, id: 2, name: a, prompt: p}\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "duplicate tasks key",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\ntasks:\n  - {id: 2, name: b, prompt: p}\n",
			wantErr: "failed to parse yaml tasks",
		},
		{
			name:    "zero id",
			yml:     "tasks:\n  - {id: 0, name: a, prompt: p}\n",
			wantErr: "task named 'a' has an invalid ID of 0",
		},
		{
			name:    "negative id",
			yml:     "tasks:\n  - {id: -3, name: a, prompt: p}\n",
			wantErr: "task named 'a' has an invalid ID of -3",
		},
		{
			name:    "missing id defaults to zero",
			yml:     "tasks:\n  - {name: a, prompt: p}\n",
			wantErr: "task named 'a' has an invalid ID of 0",
		},
		{
			name:    "duplicate id",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 1, name: b, prompt: p}\n",
			wantErr: "task named 'b' duplicates id 1",
		},
		{
			name:    "aliased task repeats its id",
			yml:     "tasks:\n  - &t {id: 1, name: a, prompt: p}\n  - *t\n",
			wantErr: "task named 'a' duplicates id 1",
		},
		{
			name:    "duplicate name",
			yml:     "tasks:\n  - {id: 1, name: build, prompt: p}\n  - {id: 2, name: build, prompt: p}\n",
			wantErr: "task id 2 duplicates the task name 'build'",
		},
		{
			name:    "duplicate name differing only by case",
			yml:     "tasks:\n  - {id: 1, name: Build, prompt: p}\n  - {id: 2, name: bUILD, prompt: p}\n",
			wantErr: "task id 2 duplicates the task name 'bUILD'",
		},
		{
			name:    "duplicate name differing only by surrounding whitespace",
			yml:     "tasks:\n  - {id: 1, name: Build, prompt: p}\n  - {id: 2, name: \"  build\\t\", prompt: p}\n",
			wantErr: "task id 2 duplicates the task name",
		},
		{
			name:    "missing name",
			yml:     "tasks:\n  - {id: 1, prompt: p}\n",
			wantErr: "task id 1 name is empty or whitespace",
		},
		{
			name:    "empty name",
			yml:     "tasks:\n  - {id: 1, name: \"\", prompt: p}\n",
			wantErr: "task id 1 name is empty or whitespace",
		},
		{
			name:    "whitespace-only name",
			yml:     "tasks:\n  - {id: 1, name: \" \\t \", prompt: p}\n",
			wantErr: "task id 1 name is empty or whitespace",
		},
		{
			name:    "blank name on a later task",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 2, name: \"  \", prompt: p}\n",
			wantErr: "task id 2 name is empty or whitespace",
		},
		{
			name:    "missing prompt",
			yml:     "tasks:\n  - {id: 1, name: a}\n",
			wantErr: "task id 1 prompt is empty or whitespace",
		},
		{
			name:    "empty prompt",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: \"\"}\n",
			wantErr: "task id 1 prompt is empty or whitespace",
		},
		{
			name:    "whitespace-only prompt",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: \" \\t\\n \"}\n",
			wantErr: "task id 1 prompt is empty or whitespace",
		},
		{
			name:    "blank prompt on a later task",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 2, name: b, prompt: \"  \"}\n",
			wantErr: "task id 2 prompt is empty or whitespace",
		},
		{
			name:    "interior whitespace is not stripped from a state",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: \"pen ding\"}\n",
			wantErr: "task id 1 has an invalid state: pen ding",
		},
		{
			name:    "invalid state is reported in its normalized form",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: \"  BoGuS \"}\n",
			wantErr: "task id 1 has an invalid state: bogus",
		},
		{
			name:    "state typo",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: complete}\n",
			wantErr: "task id 1 has an invalid state: complete",
		},
		{
			name:    "withdrawn in_progress state",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: in_progress}\n",
			wantErr: "task id 1 has an invalid state: in_progress",
		},
		{
			name:    "checkbox marker is not a state",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: \"[x]\"}\n",
			wantErr: "task id 1 has an invalid state: [x]",
		},
		{
			name:    "invalid state on a later task",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: pending}\n  - {id: 2, name: b, prompt: p, state: done}\n",
			wantErr: "task id 2 has an invalid state: done",
		},
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

// TestParseTasks_ValidationPrecedence pins which rule reports first when one
// file breaks several: invalid ID, duplicate ID, blank name, duplicate name,
// blank prompt, then invalid state.
func TestParseTasks_ValidationPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		yml     string
		wantErr string
	}{
		{
			name:    "invalid id reported before duplicate id",
			yml:     "tasks:\n  - {name: a, prompt: p}\n  - {name: b, prompt: p}\n",
			wantErr: "task named 'a' has an invalid ID of 0",
		},
		{
			name:    "invalid id reported before an earlier duplicate name",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 2, name: a, prompt: p}\n  - {id: 0, name: c, prompt: p}\n",
			wantErr: "task named 'c' has an invalid ID of 0",
		},
		{
			name:    "duplicate id reported before duplicate name",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 1, name: a, prompt: p}\n",
			wantErr: "task named 'a' duplicates id 1",
		},
		{
			name:    "duplicate id reported before an earlier blank name",
			yml:     "tasks:\n  - {id: 1, name: \"\", prompt: p}\n  - {id: 1, name: b, prompt: p}\n",
			wantErr: "task named 'b' duplicates id 1",
		},
		{
			name:    "blank name reported before duplicate name",
			yml:     "tasks:\n  - {id: 1, name: \" \", prompt: p}\n  - {id: 2, name: \"\", prompt: p}\n",
			wantErr: "task id 1 name is empty or whitespace",
		},
		{
			name:    "duplicate name reported before an earlier blank prompt",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: \"\"}\n  - {id: 2, name: a, prompt: p}\n",
			wantErr: "task id 2 duplicates the task name 'a'",
		},
		{
			name:    "blank prompt reported before an earlier invalid state",
			yml:     "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n  - {id: 2, name: b, prompt: \"\", state: pending}\n",
			wantErr: "task id 2 prompt is empty or whitespace",
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

func TestParseTasks_WrapsYAMLError(t *testing.T) {
	_, err := ParseTasks([]byte("tasks: nope\n"))
	require.Error(t, err)

	var typeErr *yaml.TypeError
	assert.ErrorAs(t, err, &typeErr, "the underlying yaml error must stay reachable")
}

func TestIDsAreUnique(t *testing.T) {
	ok, task := idsAreUnique(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}})
	assert.True(t, ok)
	assert.Equal(t, Task{}, task)

	ok, task = idsAreUnique(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 1, Name: "c"}}})
	assert.False(t, ok)
	assert.Equal(t, Task{ID: 1, Name: "c"}, task, "the later holder of the id is the offender")

	ok, task = idsAreUnique(TaskList{})
	assert.True(t, ok, "an empty list has no duplicate ids")
	assert.Equal(t, Task{}, task)
}

func TestNamesAreUnique(t *testing.T) {
	ok, task := namesAreUnique(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}})
	assert.True(t, ok)
	assert.Equal(t, Task{}, task)

	ok, task = namesAreUnique(TaskList{Tasks: []Task{{ID: 1, Name: "Deploy"}, {ID: 2, Name: "DEPLOY"}}})
	assert.False(t, ok, "names compare case-insensitively")
	assert.Equal(t, Task{ID: 2, Name: "DEPLOY"}, task, "the later holder of the name is the offender")

	ok, task = namesAreUnique(TaskList{Tasks: []Task{{ID: 1, Name: "Deploy"}, {ID: 2, Name: "\tdeploy  "}}})
	assert.False(t, ok, "names compare ignoring surrounding whitespace")
	assert.Equal(t, int16(2), task.ID)

	ok, _ = namesAreUnique(TaskList{Tasks: []Task{{ID: 1, Name: "re deploy"}, {ID: 2, Name: "redeploy"}}})
	assert.True(t, ok, "interior whitespace is significant")

	ok, _ = namesAreUnique(TaskList{})
	assert.True(t, ok, "an empty list has no duplicate names")
}

func TestNamesAreNotWhitespace(t *testing.T) {
	ok, task := namesAreNotWhitespace(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: 2, Name: "  padded  "}}})
	assert.True(t, ok)
	assert.Equal(t, Task{}, task)

	for _, name := range []string{"", " ", "\t", "\n", " \t\r\n "} {
		ok, task = namesAreNotWhitespace(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: 2, Name: name}}})
		assert.False(t, ok, "name %q must be rejected", name)
		assert.Equal(t, int16(2), task.ID, "name %q: the offending task is returned", name)
	}

	ok, _ = namesAreNotWhitespace(TaskList{})
	assert.True(t, ok, "an empty list has no blank names")
}

func TestPromptsAreNotWhitespace(t *testing.T) {
	ok, task := promptsAreNotWhitespace(TaskList{Tasks: []Task{{ID: 1, Prompt: "p"}, {ID: 2, Prompt: "  padded  "}}})
	assert.True(t, ok)
	assert.Equal(t, Task{}, task)

	for _, prompt := range []string{"", " ", "\t", "\n", " \t\r\n "} {
		ok, task = promptsAreNotWhitespace(TaskList{Tasks: []Task{{ID: 1, Prompt: "p"}, {ID: 2, Prompt: prompt}}})
		assert.False(t, ok, "prompt %q must be rejected", prompt)
		assert.Equal(t, int16(2), task.ID, "prompt %q: the offending task is returned", prompt)
	}

	ok, _ = promptsAreNotWhitespace(TaskList{})
	assert.True(t, ok, "an empty list has no blank prompts")
}

func TestIDsArePositive(t *testing.T) {
	ok, task := idsArePositive(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: 32767, Name: "b"}}})
	assert.True(t, ok)
	assert.Equal(t, Task{}, task)

	for _, id := range []int16{0, -1, -32768} {
		ok, task = idsArePositive(TaskList{Tasks: []Task{{ID: 1, Name: "a"}, {ID: id, Name: "bad"}}})
		assert.False(t, ok, "id %d must be rejected", id)
		assert.Equal(t, Task{ID: id, Name: "bad"}, task, "id %d: the offending task is returned", id)
	}

	ok, _ = idsArePositive(TaskList{})
	assert.True(t, ok, "an empty list has no invalid ids")
}

func TestStatesAreValid(t *testing.T) {
	ok, task := statesAreValid(TaskList{Tasks: []Task{
		{ID: 1, State: PendingState},
		{ID: 2, State: CompletedState},
		{ID: 3, State: AbandonedState},
	}})
	assert.True(t, ok)
	assert.Equal(t, Task{}, task)

	for _, state := range []string{"", " ", "Pending", "PENDING", " pending", "pending ", "complete", "done", "in_progress", "blocked", "[ ]", "[x]", "[~]"} {
		ok, task = statesAreValid(TaskList{Tasks: []Task{{ID: 1, State: PendingState}, {ID: 2, State: state}}})
		assert.False(t, ok, "state %q must be rejected", state)
		assert.Equal(t, Task{ID: 2, State: state}, task, "state %q: the offending task is returned unmodified", state)
	}

	ok, _ = statesAreValid(TaskList{})
	assert.True(t, ok, "an empty list has no invalid states")
}

func TestNormalizeState(t *testing.T) {
	list := TaskList{Tasks: []Task{
		{ID: 1, State: ""},
		{ID: 2, State: "  "},
		{ID: 3, State: "Completed"},
		{ID: 4, State: " ABANDONED\t"},
		{ID: 5, State: PendingState},
		{ID: 6, State: " Bogus "},
		{ID: 7, State: "Com Pleted"},
	}}

	normalizeState(&list)

	want := []Task{
		{ID: 1, State: PendingState},
		{ID: 2, State: PendingState},
		{ID: 3, State: CompletedState},
		{ID: 4, State: AbandonedState},
		{ID: 5, State: PendingState},
		{ID: 6, State: "bogus"},
		{ID: 7, State: "com pleted"},
	}
	assert.Equal(t, want, list.Tasks, "every task is normalized in place; unknown values are cleaned but left for validation to reject")

	assert.NotPanics(t, func() { normalizeState(&TaskList{}) })
}
