package runlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

const stampText = `"time":"2026-10-01T14:02:11-04:00"`

// fixedNow is a clock that never moves, in a zone that is not UTC, so the
// tests show the times written are the clock's own local time.
func fixedNow() time.Time {
	zone := time.FixedZone("EDT", -4*60*60)
	return time.Date(2026, time.October, 1, 14, 2, 11, 0, zone)
}

var testInfo = Info{
	Version:         "v0.10.0",
	TasksFile:       "run/tasks.yaml",
	Permissions:     "sandbox",
	SandboxSettings: "run/sandbox.json",
	Commit:          true,
}

// openTest opens a log in a fresh run folder with the fixed clock.
func openTest(t *testing.T) (*Log, string) {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "logs", "20261001T140211")
	l, err := open(runDir, testInfo, fixedNow)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return l, runDir
}

func record(t *testing.T, l *Log, events ...looper.Event) {
	t.Helper()
	for _, e := range events {
		require.NoError(t, l.Record(e))
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func ledgerLines(t *testing.T, runDir string) []string {
	t.Helper()
	return readLines(t, filepath.Join(runDir, "run.jsonl"))
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

func TestRunDir_IsNamedByLocalStartTime(t *testing.T) {
	start := fixedNow()
	assert.Equal(t, filepath.Join("logs", "20261001T140211"), RunDir("logs", start))
}

func TestOpen_CreatesPrivateFolderAndRunStarted(t *testing.T) {
	_, runDir := openTest(t)

	assert.Equal(t, os.FileMode(0o700), mode(t, filepath.Dir(runDir)), "the log directory")
	assert.Equal(t, os.FileMode(0o700), mode(t, runDir), "the run folder")
	assert.Equal(t, os.FileMode(0o600), mode(t, filepath.Join(runDir, "run.jsonl")))
	want := `{` + stampText + `,"event":"run_started","version":"v0.10.0","tasks_file":"run/tasks.yaml","permissions":"sandbox","sandbox_settings":"run/sandbox.json","commit":true}`
	lines := ledgerLines(t, runDir)
	assert.Equal(t, []string{want}, lines)
	assert.NotContains(t, lines[0], "prompt_file", "the prompt lives in the task file now")
}

func TestOpen_LeavesOutWhatWasNotPassed(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	info := Info{Version: "dev", TasksFile: "t.yaml", Permissions: "skip", GateTimeout: "90s"}
	l, err := open(runDir, info, fixedNow)
	require.NoError(t, err)
	require.NoError(t, l.Close())

	want := `{` + stampText + `,"event":"run_started","version":"dev","tasks_file":"t.yaml","permissions":"skip","gate_timeout":"90s","commit":false}`
	assert.Equal(t, []string{want}, ledgerLines(t, runDir))
}

// Two runs started in the same second get the same folder name; the second
// must not write into the first one's record.
func TestOpen_ExistingRunFolderIsAnError(t *testing.T) {
	_, runDir := openTest(t)

	_, err := open(runDir, testInfo, fixedNow)

	require.ErrorIs(t, err, os.ErrExist)
	assert.Len(t, ledgerLines(t, runDir), 1, "the first run's ledger is untouched")
}

func TestOpen_LogDirUnderAFileIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := open(filepath.Join(file, "logs", "run"), testInfo, fixedNow)

	require.Error(t, err)
}

func TestRecord_CompletedRun(t *testing.T) {
	l, runDir := openTest(t)
	task := tasks.Task{ID: 1, Name: "First", Prompt: "p1"}
	done := task
	done.State = tasks.CompletedState

	record(t, l,
		looper.Event{Kind: looper.TaskStarted, Task: task},
		looper.Event{Kind: looper.Activity, Task: task, Line: "working"},
		looper.Event{Kind: looper.SessionFinished, Task: done, Duration: 90 * time.Second},
		looper.Event{Kind: looper.Activity, Task: task, Line: "→ gate go test ./..."},
		looper.Event{Kind: looper.GateFinished, Task: task, Gate: tasks.Gate{Cmd: "go test ./..."}, Limit: "10m", Duration: 41200 * time.Millisecond},
		looper.Event{Kind: looper.Activity, Task: task, Line: "→ commit First"},
		looper.Event{Kind: looper.Committed, Task: task, Hash: "3f2a9c1"},
		looper.Event{Kind: looper.TaskFinished, Task: done, Duration: 132 * time.Second},
		looper.Event{Kind: looper.RunDone, Duration: 133 * time.Second},
	)

	want := []string{
		`{` + stampText + `,"event":"task_started","task":1,"name":"First"}`,
		`{` + stampText + `,"event":"session_finished","task":1,"state":"completed","duration":"1m30s"}`,
		`{` + stampText + `,"event":"gate_finished","task":1,"cmd":"go test ./...","result":"passed","timeout":"10m","duration":"41.2s"}`,
		`{` + stampText + `,"event":"committed","task":1,"hash":"3f2a9c1"}`,
		`{` + stampText + `,"event":"task_finished","task":1,"state":"completed","duration":"2m12s"}`,
		`{` + stampText + `,"event":"run_finished","result":"completed","duration":"2m13s"}`,
	}
	assert.Equal(t, want, ledgerLines(t, runDir)[1:])

	detailPath := filepath.Join(runDir, "task-1.log")
	assert.Equal(t, os.FileMode(0o600), mode(t, detailPath))
	wantDetail := []string{"14:02:11 working", "14:02:11 → gate go test ./...", "14:02:11 → commit First"}
	assert.Equal(t, wantDetail, readLines(t, detailPath))
}

func TestRecord_GateResults(t *testing.T) {
	timedOut := fmt.Errorf("wrapped: %w", context.DeadlineExceeded)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "passed", want: "passed"},
		{name: "failed", err: errors.New("exit status 3"), want: "failed"},
		{name: "timed out", err: timedOut, want: "timed_out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, runDir := openTest(t)
			task := tasks.Task{ID: 7, Name: "Seventh"}

			record(t, l, looper.Event{Kind: looper.GateFinished, Task: task, Gate: tasks.Gate{Cmd: "check"}, Limit: "90s", Err: tt.err})

			want := `{` + stampText + `,"event":"gate_finished","task":7,"cmd":"check","result":"` + tt.want + `","timeout":"90s","duration":"0s"}`
			assert.Equal(t, want, ledgerLines(t, runDir)[1])
		})
	}
}

// Shell syntax and quotes must survive as people wrote them, and only a
// multi-line command's first line is kept.
func TestRecord_KeepsShellSyntaxReadable(t *testing.T) {
	l, runDir := openTest(t)
	task := tasks.Task{ID: 1, Name: `Say "hi" & <go>`}
	gate := tasks.Gate{Cmd: "test -z \"$(gofmt -l .)\" && echo ok > out.txt\necho second line"}

	record(t, l,
		looper.Event{Kind: looper.TaskStarted, Task: task},
		looper.Event{Kind: looper.GateFinished, Task: task, Gate: gate, Limit: "10m"},
	)

	lines := ledgerLines(t, runDir)
	assert.Contains(t, lines[1], `"name":"Say \"hi\" & <go>"`)
	assert.Contains(t, lines[2], `"cmd":"test -z \"$(gofmt -l .)\" && echo ok > out.txt"`)
	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)), line)
	}
}

func TestRecord_FailedTaskEndsTheRunAsFailed(t *testing.T) {
	l, runDir := openTest(t)
	task := tasks.Task{ID: 2, Name: "Second"}
	failed := task
	failed.State, failed.Error = tasks.FailedState, `gate "exit 3" failed: exit status 3`
	runErr := errors.New(`task 2: Second failed: gate "exit 3" failed: exit status 3`)

	record(t, l,
		looper.Event{Kind: looper.TaskStarted, Task: task},
		looper.Event{Kind: looper.TaskFinished, Task: failed, Err: runErr, Duration: time.Second},
		looper.Event{Kind: looper.RunDone, Err: runErr, Duration: time.Second},
	)

	lines := ledgerLines(t, runDir)
	assert.Equal(t, `{`+stampText+`,"event":"task_finished","task":2,"state":"failed","error":"gate \"exit 3\" failed: exit status 3","duration":"1s"}`, lines[2])
	assert.Equal(t, `{`+stampText+`,"event":"run_finished","result":"failed","error":"task 2: Second failed: gate \"exit 3\" failed: exit status 3","duration":"1s"}`, lines[3])
}

// A stopped run has no outcome for the task that was running, which matches
// the task file: that task stays as it was.
func TestRecord_StoppedRunWritesNoTaskFinished(t *testing.T) {
	l, runDir := openTest(t)
	task := tasks.Task{ID: 1, Name: "First"}

	record(t, l,
		looper.Event{Kind: looper.TaskStarted, Task: task},
		looper.Event{Kind: looper.Activity, Task: task, Line: "working"},
		looper.Event{Kind: looper.RunDone, Err: errors.New("task 1: First failed: signal: killed"), Duration: 2 * time.Second},
	)

	lines := ledgerLines(t, runDir)
	require.Len(t, lines, 3)
	assert.Equal(t, `{`+stampText+`,"event":"run_finished","result":"stopped","error":"task 1: First failed: signal: killed","duration":"2s"}`, lines[2])
	assert.Equal(t, []string{"14:02:11 working"}, readLines(t, filepath.Join(runDir, "task-1.log")))
}

// A failed task earlier in the run must not make a later stop read as failed.
func TestRecord_StopAfterACompletedTaskIsStopped(t *testing.T) {
	l, runDir := openTest(t)
	first, second := tasks.Task{ID: 1, Name: "First"}, tasks.Task{ID: 2, Name: "Second"}
	done := first
	done.State = tasks.CompletedState

	record(t, l,
		looper.Event{Kind: looper.TaskStarted, Task: first},
		looper.Event{Kind: looper.TaskFinished, Task: done},
		looper.Event{Kind: looper.TaskStarted, Task: second},
		looper.Event{Kind: looper.RunDone, Err: errors.New("stopped")},
	)

	lines := ledgerLines(t, runDir)
	assert.Contains(t, lines[len(lines)-1], `"result":"stopped"`)
	assert.FileExists(t, filepath.Join(runDir, "task-2.log"))
}

func TestRecord_EachTaskGetsItsOwnDetailFile(t *testing.T) {
	l, runDir := openTest(t)
	first, second := tasks.Task{ID: 1, Name: "First"}, tasks.Task{ID: 2, Name: "Second"}

	record(t, l,
		looper.Event{Kind: looper.TaskStarted, Task: first},
		looper.Event{Kind: looper.Activity, Task: first, Line: "one"},
		looper.Event{Kind: looper.TaskFinished, Task: first},
		looper.Event{Kind: looper.TaskStarted, Task: second},
		looper.Event{Kind: looper.Activity, Task: second, Line: "two"},
		looper.Event{Kind: looper.TaskFinished, Task: second},
	)

	assert.Equal(t, []string{"14:02:11 one"}, readLines(t, filepath.Join(runDir, "task-1.log")))
	assert.Equal(t, []string{"14:02:11 two"}, readLines(t, filepath.Join(runDir, "task-2.log")))
}

// Claude's stdout and stderr are read by two goroutines, so Record is called
// from both at once; every line must still land whole.
func TestRecord_ConcurrentActivityKeepsLinesWhole(t *testing.T) {
	l, runDir := openTest(t)
	task := tasks.Task{ID: 1, Name: "First"}
	record(t, l, looper.Event{Kind: looper.TaskStarted, Task: task})

	const writers, perWriter = 8, 200
	line := strings.Repeat("x", 300)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() { recordMany(l, task, line, perWriter) })
	}
	wg.Wait()

	lines := readLines(t, filepath.Join(runDir, "task-1.log"))
	require.Len(t, lines, writers*perWriter)
	for _, got := range lines {
		require.Equal(t, "14:02:11 "+line, got)
	}
}

func recordMany(l *Log, task tasks.Task, line string, n int) {
	for range n {
		_ = l.Record(looper.Event{Kind: looper.Activity, Task: task, Line: line})
	}
}

func TestRecord_WriteErrorIsReturned(t *testing.T) {
	l, _ := openTest(t)
	require.NoError(t, l.Close())

	err := l.Record(looper.Event{Kind: looper.RunDone})

	require.Error(t, err)
}

func TestNilLog_RecordsNothing(t *testing.T) {
	var l *Log

	assert.NoError(t, l.Record(looper.Event{Kind: looper.RunDone}))
	assert.NoError(t, l.Close())
}
