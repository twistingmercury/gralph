package looper

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// runWithRecorder runs tl through Run with a report hook that records every
// event it receives.
func runWithRecorder(t *testing.T, tl *tasks.TaskList) ([]Event, error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_CLAUDE_RECORD", filepath.Join(dir, "record.log"))

	var events []Event
	err := Run(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), func(e Event) {
		events = append(events, e)
	})
	return events, err
}

func eventKinds(events []Event) []EventKind {
	kinds := make([]EventKind, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}
	return kinds
}

func TestRun_ReportsTwoTaskSuccess(t *testing.T) {
	useFakeClaude(t)
	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	events, err := runWithRecorder(t, tl)
	require.NoError(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, TaskFinished, TaskStarted, Activity, TaskFinished, RunDone}, eventKinds(events))
	assert.Equal(t, int16(1), events[0].Task.ID)
	assert.Equal(t, int16(1), events[2].Task.ID)
	assert.Equal(t, tasks.CompletedState, events[2].Task.State)
	assert.NoError(t, events[2].Err)
	assert.Equal(t, int16(2), events[3].Task.ID)
	assert.Equal(t, int16(2), events[5].Task.ID)
	assert.Equal(t, tasks.CompletedState, events[5].Task.State)
	assert.NoError(t, events[6].Err)
}

func TestRun_ReportsFailedTask(t *testing.T) {
	useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_EXIT", "1")
	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	events, err := runWithRecorder(t, tl)
	require.Error(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, TaskFinished, RunDone}, eventKinds(events))
	assert.Equal(t, tasks.FailedState, events[2].Task.State)
	assert.Error(t, events[2].Err)
	assert.Equal(t, err, events[3].Err)
}

func TestRun_SkippedCompletedTaskSendsNoEvent(t *testing.T) {
	useFakeClaude(t)
	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "Done", Prompt: "p1", State: tasks.CompletedState},
	}}

	events, err := runWithRecorder(t, tl)
	require.NoError(t, err)

	assert.Equal(t, []EventKind{RunDone}, eventKinds(events))
}
