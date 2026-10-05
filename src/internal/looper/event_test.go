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
	err := Run(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), "", bypass, nil, func(e Event) {
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
	tl := &tasks.TaskList{Shared: tasks.Shared{Prompt: "shared prompt"}, Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	events, err := runWithRecorder(t, tl)
	require.NoError(t, err)

	require.Equal(t, []EventKind{
		TaskStarted, Activity, SessionFinished, TaskFinished,
		TaskStarted, Activity, SessionFinished, TaskFinished,
		RunDone,
	}, eventKinds(events))
	assert.Equal(t, int16(1), events[0].Task.ID)
	assert.Equal(t, int16(1), events[3].Task.ID)
	assert.Equal(t, tasks.CompletedState, events[3].Task.State)
	assert.NoError(t, events[3].Err)
	assert.Equal(t, int16(2), events[4].Task.ID)
	assert.Equal(t, int16(2), events[7].Task.ID)
	assert.Equal(t, tasks.CompletedState, events[7].Task.State)
	assert.NoError(t, events[8].Err)
}

func TestRun_ReportsFailedTask(t *testing.T) {
	useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_EXIT", "1")
	tl := &tasks.TaskList{Shared: tasks.Shared{Prompt: "shared prompt"}, Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	events, err := runWithRecorder(t, tl)
	require.Error(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, SessionFinished, TaskFinished, RunDone}, eventKinds(events))
	assert.Equal(t, tasks.FailedState, events[3].Task.State)
	assert.Error(t, events[3].Err)
	assert.Equal(t, err, events[4].Err)
}

func TestRun_SkippedCompletedTaskSendsNoEvent(t *testing.T) {
	useFakeClaude(t)
	tl := &tasks.TaskList{Shared: tasks.Shared{Prompt: "shared prompt"}, Tasks: []tasks.Task{
		{ID: 1, Name: "Done", Prompt: "p1", State: tasks.CompletedState},
	}}

	events, err := runWithRecorder(t, tl)
	require.NoError(t, err)

	assert.Equal(t, []EventKind{RunDone}, eventKinds(events))
}

func eventsOfKind(events []Event, kind EventKind) []Event {
	var out []Event
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestRun_ReportsSessionFinished(t *testing.T) {
	events, err := runStream(t)
	require.NoError(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, SessionFinished, TaskFinished, RunDone}, eventKinds(events))
	session, task, run := events[2], events[3], events[4]
	assert.Equal(t, int16(1), session.Task.ID)
	assert.Equal(t, tasks.CompletedState, session.Task.State)
	assert.Positive(t, session.Duration)
	assert.GreaterOrEqual(t, task.Duration, session.Duration)
	assert.GreaterOrEqual(t, run.Duration, task.Duration)
}

func TestRun_SessionFinishedCarriesAFailedOutcome(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"boom"}`)
	events, err := runStream(t)
	require.Error(t, err)

	sessions := eventsOfKind(events, SessionFinished)
	require.Len(t, sessions, 1)
	assert.Equal(t, tasks.FailedState, sessions[0].Task.State)
	assert.Equal(t, "boom", sessions[0].Task.Error)
}
