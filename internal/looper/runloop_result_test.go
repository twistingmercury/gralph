package looper

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// TestRunLoop_ResultLineOutcomes drives a single-task run through fake
// claude for every row of the outcome table (see result.go's outcome),
// asserting both the error runLoop returns and what gets persisted.
func TestRunLoop_ResultLineOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		exit        string // FAKE_CLAUDE_EXIT; "" leaves it unset (exit 0)
		outputSet   bool   // whether to set FAKE_CLAUDE_OUTPUT at all
		output      string // FAKE_CLAUDE_OUTPUT value when outputSet
		wantState   string
		wantErr     string // task.Error after the run
		wantErrText string // substring runLoop's returned error must contain; "" means runLoop must return nil
	}{
		{
			name:      "zero exit, completed result",
			wantState: tasks.CompletedState,
			wantErr:   "",
		},
		{
			name:        "zero exit, failed result with error",
			outputSet:   true,
			output:      "{\"state\":\"failed\",\"error\":\"go vet failed\"}\n",
			wantState:   tasks.FailedState,
			wantErr:     "go vet failed",
			wantErrText: "go vet failed",
		},
		{
			name:        "zero exit, failed result with blank error",
			outputSet:   true,
			output:      "{\"state\":\"failed\",\"error\":\"\"}\n",
			wantState:   tasks.FailedState,
			wantErr:     "session reported failed with no error",
			wantErrText: "session reported failed with no error",
		},
		{
			name:        "zero exit, missing result line",
			outputSet:   true,
			output:      "",
			wantState:   tasks.FailedState,
			wantErr:     "no valid result line in session output",
			wantErrText: "no valid result line in session output",
		},
		{
			name:        "zero exit, invalid json result line",
			outputSet:   true,
			output:      "not json at all\n",
			wantState:   tasks.FailedState,
			wantErr:     "no valid result line in session output",
			wantErrText: "no valid result line in session output",
		},
		{
			name:        "non-zero exit, valid result with error",
			exit:        "1",
			outputSet:   true,
			output:      "{\"state\":\"failed\",\"error\":\"the build broke\"}\n",
			wantState:   tasks.FailedState,
			wantErr:     "the build broke",
			wantErrText: "the build broke",
		},
		{
			name:        "non-zero exit, no result line falls back to the exec error",
			exit:        "1",
			wantState:   tasks.FailedState,
			wantErr:     "exit status 1",
			wantErrText: "exit status 1",
		},
		{
			name:      "fenced result line is still found",
			outputSet: true,
			output:    "```json\n{\"state\":\"completed\",\"error\":\"\"}\n```\n",
			wantState: tasks.CompletedState,
			wantErr:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			dir := t.TempDir()
			tasksPath := filepath.Join(dir, "tasks.yaml")
			t.Setenv("FAKE_CLAUDE_RECORD", filepath.Join(dir, "record.log"))
			if tt.exit != "" {
				t.Setenv("FAKE_CLAUDE_EXIT", tt.exit)
			}
			if tt.outputSet {
				t.Setenv("FAKE_CLAUDE_OUTPUT", tt.output)
			}

			tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "Only", Prompt: "p"}}}

			err := runLoop(context.Background(), "prompt", tl, tasksPath)

			if tt.wantErrText == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErrText)
			}

			saved := readSavedTasks(t, tasksPath)
			require.Len(t, saved.Tasks, 1)
			assert.Equal(t, tt.wantState, saved.Tasks[0].State)
			assert.Equal(t, tt.wantErr, saved.Tasks[0].Error)
		})
	}
}

// TestRunLoop_NonZeroExitUsesJSONErrorNotExitStatus proves that when a
// session exits non-zero but still emits a valid result line with a
// non-blank error, the message runLoop returns (and task.Error) is the JSON
// error text, not the generic "exit status N" text — while the exec error
// itself stays reachable via errors.As for callers that care about it.
func TestRunLoop_NonZeroExitUsesJSONErrorNotExitStatus(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", filepath.Join(dir, "record.log"))
	t.Setenv("FAKE_CLAUDE_EXIT", "5")
	t.Setenv("FAKE_CLAUDE_OUTPUT", "{\"state\":\"failed\",\"error\":\"boom\"}\n")

	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First task", Prompt: "p"}}}

	err := runLoop(context.Background(), "prompt", tl, tasksPath)
	require.Error(t, err)
	assert.Equal(t, "task 1: First task failed: boom", err.Error())
	assert.NotContains(t, err.Error(), "exit status", "the JSON error must replace the generic exit status text")

	var exitErr *exec.ExitError
	assert.ErrorAs(t, err, &exitErr, "the underlying exec error must stay reachable via errors.As")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 1)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, "boom", saved.Tasks[0].Error)
}

// TestRunLoop_ClearsErrorWhenPreviouslyFailedTaskNowCompletes proves a task
// retried after an earlier failure has its stored error cleared, and that
// the cleared error is omitted from the saved yaml rather than written as an
// empty string.
func TestRunLoop_ClearsErrorWhenPreviouslyFailedTaskNowCompletes(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", filepath.Join(dir, "record.log"))

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "Only", Prompt: "p", State: tasks.FailedState, Error: "exit status 1"},
	}}

	err := runLoop(context.Background(), "prompt", tl, tasksPath)
	require.NoError(t, err)

	data, readErr := os.ReadFile(tasksPath)
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "error:", "a cleared error must be omitted from the saved yaml, not written empty")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 1)
	assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
	assert.Equal(t, "", saved.Tasks[0].Error)
}

// TestRunLoop_ResultLineFailureStopsLaterTasks proves a failure signalled
// purely through the result line (a zero exit that reports "failed") stops
// the loop before any later task runs, same as a non-zero exit does.
func TestRunLoop_ResultLineFailureStopsLaterTasks(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	recordPath := filepath.Join(dir, "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	t.Setenv("FAKE_CLAUDE_OUTPUT", "{\"state\":\"failed\",\"error\":\"tests failed\"}\n")

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	err := runLoop(context.Background(), "prompt", tl, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "tests failed")

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 1, "the second task must never run after the first fails via its result line")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 2)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, "tests failed", saved.Tasks[0].Error)
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State)
}
