package looper

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// recorder is a report hook that records every event it receives; report
// may be called from more than one goroutine.
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) report(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

// runStream runs one pending task through Run with a recording report and
// returns the events and Run's error.
func runStream(t *testing.T) ([]Event, error) {
	t.Helper()
	useFakeClaude(t)
	dir := t.TempDir()
	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "p1"}}}

	var rec recorder
	err := Run(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), rec.report)
	return rec.snapshot(), err
}

func activityLines(events []Event) []string {
	var lines []string
	for _, e := range events {
		if e.Kind == Activity {
			lines = append(lines, e.Line)
		}
	}
	return lines
}

func TestRunTaskStream_ArgvAndStdin(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	recordPath := filepath.Join(dir, "record.log")
	t.Setenv("FAKE_CLAUDE_ARGS", argsPath)
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	task := tasks.Task{ID: 1, Name: "First", Prompt: "Do the first thing."}
	tl := &tasks.TaskList{Tasks: []tasks.Task{task}}
	var rec recorder
	require.NoError(t, Run(context.Background(), "Follow the runbook.", tl, filepath.Join(dir, "tasks.yaml"), rec.report))

	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, "--print --output-format stream-json --verbose --dangerously-skip-permissions\n", string(args))

	records := readFakeClaudeRecords(t, recordPath)
	require.Len(t, records, 1)
	assert.Equal(t, "Follow the runbook.\n\n"+task.String()+"\n", records[0])
}

func TestRunTaskStream_Success(t *testing.T) {
	events, err := runStream(t)
	require.NoError(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, TaskFinished, RunDone}, eventKinds(events))
	assert.Equal(t, "working", events[1].Line)
	assert.Equal(t, int16(1), events[1].Task.ID)
	assert.Equal(t, tasks.CompletedState, events[2].Task.State)
	assert.NoError(t, events[2].Err)
	assert.NoError(t, events[3].Err)
}

func TestRunTaskStream_ResultReportsFailed(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"boom"}`)
	events, err := runStream(t)
	require.Error(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, TaskFinished, RunDone}, eventKinds(events))
	assert.Equal(t, tasks.FailedState, events[2].Task.State)
	assert.Equal(t, "boom", events[2].Task.Error)
}

func TestRunTaskStream_NonZeroExitFails(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_EXIT", "1")
	events, err := runStream(t)
	require.Error(t, err)

	require.Equal(t, []EventKind{TaskStarted, Activity, TaskFinished, RunDone}, eventKinds(events))
	assert.Equal(t, tasks.FailedState, events[2].Task.State)
}

func TestRunTaskStream_BigEventStillCompletes(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_BIG_EVENT", "200000")
	events, err := runStream(t)
	require.NoError(t, err)

	require.Equal(t, TaskFinished, events[len(events)-2].Kind)
	assert.Equal(t, tasks.CompletedState, events[len(events)-2].Task.State)
}

func TestRunTaskStream_StderrArrivesAsActivity(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_STDERR", "warn one\nwarn two\n")
	events, err := runStream(t)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"working", "warn one", "warn two"}, activityLines(events))
}

func TestRunTaskStream_CancelLeavesTasksFileUntouched(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_BLOCK", "1")
	t.Setenv("FAKE_CLAUDE_READY", readyPath)

	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))
	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "p1"}}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var rec recorder
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, "prompt", tl, tasksPath, rec.report)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "fake claude never became ready")

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	assert.NotContains(t, eventKinds(rec.snapshot()), TaskFinished)
	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got))
}

func TestRunTaskStream_WritesNothingToStdout(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	outCh := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		outCh <- string(out)
	}()

	_, runErr := runStream(t)
	os.Stdout = orig
	require.NoError(t, w.Close())

	require.NoError(t, runErr)
	assert.Empty(t, <-outCh)
}
