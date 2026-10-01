package looper

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// touchGate is a gate that leaves proof it ran.
func touchGate(path string) tasks.Gate {
	return tasks.Gate{Cmd: "touch '" + path + "'"}
}

func gatedTask(gates ...tasks.Gate) *tasks.TaskList {
	return &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "p1", Gates: gates}}}
}

func TestRunLoop_GatesPassCompletesTask(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	first, second := filepath.Join(dir, "gate1"), filepath.Join(dir, "gate2")

	// The second gate passes only if the first already ran: file order.
	gates := []tasks.Gate{touchGate(first), {Cmd: "test -e '" + first + "' && touch '" + second + "'"}}
	tl := gatedTask(gates...)

	require.NoError(t, runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil))

	assert.FileExists(t, first)
	assert.FileExists(t, second)

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 1)
	assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
	assert.Empty(t, saved.Tasks[0].Error)
	assert.Equal(t, gates, saved.Tasks[0].Gates, "gates must survive the save")
}

func TestRunLoop_GateFailureFailsTaskAndStopsRun(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	never := filepath.Join(dir, "never")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1", Gates: []tasks.Gate{{Cmd: `test "a" = "b"`}, touchGate(never)}},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	const wantErr = `gate "test \"a\" = \"b\"" failed: exit status 1`
	err := runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil)
	require.EqualError(t, err, "task 1: First failed: "+wantErr)

	assert.NoFileExists(t, never, "gates after a failed gate must not run")
	assert.Len(t, readFakeClaudeRecords(t, recordPath), 1, "the second task must never run")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 2)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, wantErr, saved.Tasks[0].Error, "the quoted command must survive the yaml save and reload")
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State)
}

func TestRunLoop_GatesSkippedWhenSessionFails(t *testing.T) {
	tests := []struct {
		name    string
		env     [2]string
		wantErr string
	}{
		{name: "non-zero exit", env: [2]string{"FAKE_CLAUDE_EXIT", "1"}, wantErr: "exit status 1"},
		{name: "session reports failed", env: [2]string{"FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"nope"}`}, wantErr: "nope"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			t.Setenv(tt.env[0], tt.env[1])
			dir := t.TempDir()
			marker := filepath.Join(dir, "gate-ran")

			err := runLoop(context.Background(), "prompt", gatedTask(touchGate(marker)), filepath.Join(dir, "tasks.yaml"), "", bypass, nil, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.NotContains(t, err.Error(), "gate")
			assert.NoFileExists(t, marker, "a failed session must not run gates")
		})
	}
}

func TestRunLoop_GatesNeverReachClaude(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	tl := gatedTask(tasks.Gate{Cmd: "true # gate-only-text"})
	require.NoError(t, runLoop(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), "", bypass, nil, nil))

	records := readFakeClaudeRecords(t, recordPath)
	require.Len(t, records, 1)
	assert.Equal(t, "prompt\n\n1: First\n\np1\n", records[0], "the stdin contract must not change when a task has gates")
}

func TestRunLoop_GateShellBehavior(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantErr string
	}{
		{name: "pipes and substitution", cmd: `test "$(printf ab | tr a-z A-Z)" = AB`},
		{name: "multi-line command", cmd: "x=1\ntest \"$x\" = 1"},
		{name: "stdin is empty, not inherited", cmd: `test -z "$(cat)"`},
		{name: "exit code is reported", cmd: "exit 3", wantErr: `gate "exit 3" failed: exit status 3`},
		{name: "command not found", cmd: "gralph-no-such-command", wantErr: `gate "gralph-no-such-command" failed: exit status 127`},
		{name: "shell syntax error", cmd: `echo "unterminated`, wantErr: `gate "echo \"unterminated" failed: exit status`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

			err := runLoop(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: tt.cmd}), tasksPath, "", bypass, nil, nil)
			saved := readSavedTasks(t, tasksPath)
			require.Len(t, saved.Tasks, 1)

			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
				return
			}

			require.Error(t, err)
			assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
			assert.Contains(t, saved.Tasks[0].Error, tt.wantErr)
		})
	}
}

func TestRunLoop_PlainPrintsGateLineAndOutput(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runLoop(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: "echo gate-says-hi"}), tasksPath, "", bypass, nil, nil)

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	require.NoError(t, runErr)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "gate: echo gate-says-hi\ngate-says-hi\n")
}

func TestRun_GateOutputArrivesAsActivity(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

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

	var rec recorder
	tl := gatedTask(tasks.Gate{Cmd: "echo from-stdout\necho from-stderr >&2"})
	runErr := Run(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, rec.report)
	os.Stdout = orig
	require.NoError(t, w.Close())
	require.NoError(t, runErr)
	assert.Empty(t, <-outCh, "the stream path must write nothing to gralph's stdout, gates included")

	events := rec.snapshot()
	lines := activityLines(events)
	assert.Contains(t, lines, "→ gate echo from-stdout", "only the command's first line names the gate")
	assert.Contains(t, lines, "from-stdout")
	assert.Contains(t, lines, "from-stderr")

	kinds := eventKinds(events)
	require.GreaterOrEqual(t, len(kinds), 2)
	assert.Equal(t, []EventKind{TaskFinished, RunDone}, kinds[len(kinds)-2:], "TaskFinished must follow the gate's activity")
}

func TestRun_GateFailureReportsFailedTask(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

	var rec recorder
	err := Run(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: "exit 3"}), tasksPath, "", bypass, nil, rec.report)
	require.Error(t, err)

	var finished []Event
	for _, e := range rec.snapshot() {
		if e.Kind == TaskFinished {
			finished = append(finished, e)
		}
	}

	require.Len(t, finished, 1)
	assert.Equal(t, tasks.FailedState, finished[0].Task.State)
	assert.Equal(t, `gate "exit 3" failed: exit status 3`, finished[0].Task.Error)
	assert.Error(t, finished[0].Err)
}

func TestRun_CancelDuringGateLeavesFileUntouched(t *testing.T) {
	tests := []struct {
		name    string
		stream  bool
		timeout string
	}{
		{name: "plain"},
		{name: "stream", stream: true},
		{name: "plain with timeout", timeout: "30s"},
		{name: "stream with timeout", stream: true, timeout: "30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { cancelDuringGate(t, tt.stream, tt.timeout) })
	}
}

func cancelDuringGate(t *testing.T, stream bool, timeout string) {
	useFakeClaude(t)
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")

	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))
	tl := gatedTask(tasks.Gate{Cmd: "touch '" + readyPath + "'; sleep 60", Timeout: timeout})

	var rec recorder
	var report func(Event)
	if stream {
		report = rec.report
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, "prompt", tl, tasksPath, "", bypass, nil, report) }()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the gate never started")

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.ErrorContains(t, err, "task 1: First failed: gate")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a cancel during a gate; the gate's process group was not killed")
	}

	assert.NotContains(t, eventKinds(rec.snapshot()), TaskFinished)
	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got), "the tasks file must be byte-for-byte unchanged after a cancel during a gate")
}

func TestRun_GateTimeoutFailsTaskAndSkipsLaterGates(t *testing.T) {
	tests := []struct {
		name   string
		stream bool
	}{
		{name: "plain"},
		{name: "stream", stream: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			dir := t.TempDir()
			tasksPath := filepath.Join(dir, "tasks.yaml")
			never := filepath.Join(dir, "never")

			tl := gatedTask(tasks.Gate{Cmd: "sleep 30", Timeout: "100ms"}, touchGate(never))

			var rec recorder
			var report func(Event)
			if tt.stream {
				report = rec.report
			}

			const wantErr = `gate "sleep 30" timed out after 100ms`
			start := time.Now()
			err := Run(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, report)

			require.EqualError(t, err, "task 1: First failed: "+wantErr)
			assert.Less(t, time.Since(start), 10*time.Second, "the deadline must kill the gate, not wait out its sleep")
			assert.NoFileExists(t, never, "gates after a timed-out gate must not run")

			saved := readSavedTasks(t, tasksPath)
			require.Len(t, saved.Tasks, 1)
			assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
			assert.Equal(t, wantErr, saved.Tasks[0].Error)
		})
	}
}

func TestRun_GateWithinTimeoutPasses(t *testing.T) {
	tests := []struct {
		name   string
		stream bool
	}{
		{name: "plain"},
		{name: "stream", stream: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			dir := t.TempDir()
			tasksPath := filepath.Join(dir, "tasks.yaml")
			marker := filepath.Join(dir, "second")

			tl := gatedTask(tasks.Gate{Cmd: "true", Timeout: "30s"}, touchGate(marker))

			var rec recorder
			var report func(Event)
			if tt.stream {
				report = rec.report
			}

			require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, report))
			assert.FileExists(t, marker)

			saved := readSavedTasks(t, tasksPath)
			require.Len(t, saved.Tasks, 1)
			assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
			assert.Equal(t, tl.Tasks[0].Gates, saved.Tasks[0].Gates, "timeout must survive the save as written")
		})
	}
}

func TestRun_GateTimeoutThatCannotBeParsedFailsTheGate(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

	err := Run(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: "true", Timeout: "soon"}), tasksPath, "", bypass, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, `gate "true" failed:`)
	assert.ErrorContains(t, err, "must be a duration string such as 90s or 10m")
}

func TestGateLimit_FlagBeatsGateBeatsDefault(t *testing.T) {
	tests := []struct {
		name       string
		gate       tasks.Gate
		override   string
		wantLimit  string
		wantSource string
	}{
		{name: "flag beats a gate's own timeout", gate: tasks.Gate{Cmd: "x", Timeout: "30s"}, override: "2m", wantLimit: "2m", wantSource: "flag"},
		{name: "flag beats the default", gate: tasks.Gate{Cmd: "x"}, override: "90s", wantLimit: "90s", wantSource: "flag"},
		{name: "gate beats the default", gate: tasks.Gate{Cmd: "x", Timeout: "30s"}, wantLimit: "30s", wantSource: "gate"},
		{name: "default when neither is set", gate: tasks.Gate{Cmd: "x"}, wantLimit: defaultGateTimeout, wantSource: "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, source := gateLimit(tt.gate, tt.override)
			assert.Equal(t, tt.wantLimit, limit)
			assert.Equal(t, tt.wantSource, source)
		})
	}
}

func TestGateLimit_DefaultIsTenMinutes(t *testing.T) {
	assert.Equal(t, "10m", defaultGateTimeout)
}

func TestGateFailure_TimedOutNamesTheLimitThatApplied(t *testing.T) {
	gate := tasks.Gate{Cmd: "sleep 30", Timeout: "30s"}

	assert.Equal(t, `gate "sleep 30" timed out after 10m`, gateFailure(gate, "10m", context.DeadlineExceeded))
	assert.Equal(t, `gate "sleep 30" timed out after 2m`, gateFailure(gate, "2m", context.DeadlineExceeded))
}

func TestRun_GateTimeoutFlagOverridesTheGateTimeout(t *testing.T) {
	tests := []struct {
		name       string
		stream     bool
		gateLimit  string
		override   string
		wantSuffix string
	}{
		{name: "plain, flag cuts a longer gate timeout", gateLimit: "30s", override: "100ms", wantSuffix: "100ms"},
		{name: "stream, flag cuts a longer gate timeout", stream: true, gateLimit: "30s", override: "100ms", wantSuffix: "100ms"},
		{name: "plain, flag applies to a gate with no timeout", override: "100ms", wantSuffix: "100ms"},
		{name: "stream, flag applies to a gate with no timeout", stream: true, override: "100ms", wantSuffix: "100ms"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
			tl := gatedTask(tasks.Gate{Cmd: "sleep 30", Timeout: tt.gateLimit})

			var rec recorder
			var report func(Event)
			if tt.stream {
				report = rec.report
			}

			start := time.Now()
			err := Run(context.Background(), "prompt", tl, tasksPath, tt.override, bypass, nil, report)

			require.EqualError(t, err, `task 1: First failed: gate "sleep 30" timed out after `+tt.wantSuffix)
			assert.Less(t, time.Since(start), 10*time.Second, "the flag must cut the gate short")
		})
	}
}

func TestRun_GateTimeoutFlagIsNeverSaved(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	tl := gatedTask(tasks.Gate{Cmd: "true", Timeout: "30s"}, tasks.Gate{Cmd: "true"})

	require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "45s", bypass, nil, nil))

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), "timeout:"), "only the gate that had a timeout may carry the key")
	assert.Contains(t, string(data), "timeout: 30s")
	assert.NotContains(t, string(data), "45s", "the flag must not reach the tasks file")
	assert.NotContains(t, string(data), "10m", "the default must not reach the tasks file")
	assert.Equal(t, []tasks.Gate{{Cmd: "true", Timeout: "30s"}, {Cmd: "true"}}, readSavedTasks(t, tasksPath).Tasks[0].Gates)
}

func TestRun_MultiLineGateErrorsNameOnlyTheFirstLine(t *testing.T) {
	tests := []struct {
		name    string
		gate    tasks.Gate
		wantErr string
	}{
		{name: "non-zero exit", gate: tasks.Gate{Cmd: "echo first\nexit 3"}, wantErr: `gate "echo first" failed: exit status 3`},
		{name: "timeout", gate: tasks.Gate{Cmd: "echo first\nsleep 30", Timeout: "100ms"}, wantErr: `gate "echo first" timed out after 100ms`},
	}

	for _, tt := range tests {
		for _, stream := range []bool{false, true} {
			name := tt.name + " plain"
			var rec recorder
			var report func(Event)
			if stream {
				name = tt.name + " stream"
				report = rec.report
			}

			t.Run(name, func(t *testing.T) {
				useFakeClaude(t)
				tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

				err := Run(context.Background(), "prompt", gatedTask(tt.gate), tasksPath, "", bypass, nil, report)

				require.EqualError(t, err, "task 1: First failed: "+tt.wantErr)
				assert.Equal(t, tt.wantErr, readSavedTasks(t, tasksPath).Tasks[0].Error)
				assert.Equal(t, tt.gate.Cmd, readSavedTasks(t, tasksPath).Tasks[0].Gates[0].Cmd, "the stored command stays whole")
			})
		}
	}
}

func TestGateFirstLine(t *testing.T) {
	assert.Equal(t, "one", firstLine("one"))
	assert.Equal(t, "  one ", firstLine("  one \ntwo"))
	assert.Equal(t, "", firstLine("\ntwo"))
}
