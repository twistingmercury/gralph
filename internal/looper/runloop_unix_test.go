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
		errCh <- runLoop(ctx, "prompt", tl, tasksPath, nil)
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
