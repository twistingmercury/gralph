package looper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// TestRunLoop_CancelMidTaskStopsQuickly proves that canceling the context
// while claude is running kills it via the process-group termination wired
// up in configureProcessTree (process_tree_unix.go), so runLoop returns
// promptly instead of hanging, and the next task is never started.
func TestRunLoop_CancelMidTaskStopsQuickly(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	t.Setenv("FAKE_CLAUDE_BLOCK", "1")
	t.Setenv("FAKE_CLAUDE_READY", readyPath)

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n  - {id: 2, name: Second, prompt: p2, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runLoop(ctx, "prompt", tl, tasksPath, "", bypass, nil, nil)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "fake claude never became ready")

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.ErrorContains(t, err, "task 1")
	case <-time.After(5 * time.Second):
		t.Fatal("runLoop did not return after cancellation")
	}

	records := readFakeClaudeRecords(t, recordPath)
	assert.Empty(t, records, "expected claude never to record an invocation (killed while blocked)")

	gotTasksYAML, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(gotTasksYAML), "the tasks file must be byte-for-byte unchanged after a mid-task cancel")
}

// TestRunLoop_SavesAfterEachCompletedTask proves the tasks file is saved as
// soon as a task completes, not only at the end of the run: task 1 completes,
// the run is cancelled while task 2 is running, and the file on disk already
// records task 1 as completed.
func TestRunLoop_SavesAfterEachCompletedTask(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_BLOCK_ON", "block-second")
	t.Setenv("FAKE_CLAUDE_READY", readyPath)

	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n  - {id: 2, name: Second, prompt: block-second, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))
	tl, err := tasks.ParseTasks([]byte(tasksYAML))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- runLoop(ctx, "prompt", &tl, tasksPath, "", bypass, nil, nil)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "fake claude never blocked on task 2")

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.ErrorContains(t, err, "task 2")
	case <-time.After(5 * time.Second):
		t.Fatal("runLoop did not return after cancellation")
	}

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	saved, err := tasks.ParseTasks(data)
	require.NoError(t, err)
	require.Len(t, saved.Tasks, 2)
	assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State)
}

// unwritableTasksFile writes a two-task file into a directory of its own and
// then takes away the directory's write permission, which is how a user would
// hit a failed save: SaveTasks cannot create its temporary file there. The
// mode is restored on cleanup so the temp dir can be removed.
func unwritableTasksFile(t *testing.T) (string, *tasks.TaskList) {
	t.Helper()

	dir := t.TempDir()
	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n  - {id: 2, name: Second, prompt: p2, state: pending}\n"
	tasksPath := writeTasksFile(t, dir, tasksYAML)
	tl, err := tasks.ParseTasks([]byte(tasksYAML))
	require.NoError(t, err)

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	return tasksPath, &tl
}

// TestRunLoop_FailedSaveAfterCompletedTaskStopsRun proves a completed task
// whose state cannot be written stops the run: carrying on would run later
// tasks while the file still calls this one pending, so the next run would
// repeat it.
func TestRunLoop_FailedSaveAfterCompletedTaskStopsRun(t *testing.T) {
	useFakeClaude(t)
	recordPath := filepath.Join(t.TempDir(), "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	tasksPath, tl := unwritableTasksFile(t)

	err := runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil)

	require.Error(t, err)
	assert.ErrorContains(t, err, "task 1: First: failed to save task state")

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 1, "task 2 must never start once the save has failed")
}

// TestRunLoop_FailedSaveAfterFailedTaskReportsBoth proves that when the
// session fails and the save fails too, the user is told both: the task
// failure alone would hide that the file still says pending, and the save
// failure alone would hide why the run stopped.
func TestRunLoop_FailedSaveAfterFailedTaskReportsBoth(t *testing.T) {
	useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"boom"}`)
	tasksPath, tl := unwritableTasksFile(t)

	err := runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil)

	require.Error(t, err)
	assert.ErrorContains(t, err, "task 1: First failed: boom")
	assert.ErrorContains(t, err, "failed to save tasks to")
}
