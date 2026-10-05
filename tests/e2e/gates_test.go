package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers gates (ADR-013, ADR-019): commands in the task file's
// shared.gates list that gralph runs itself, through sh, after each
// session reports completed. A task is completed only when every gate exits
// zero, and gates are never sent to claude.

func TestGates_PassingGatesCompleteTheTask(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "gate-ran")

	tasksYAML := withShared("Body.", fmt.Sprintf(`    - cmd: echo gate-output
    - cmd: touch '%s'
`, marker), `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, "gate: echo gate-output\ngate-output\n")
	assert.FileExists(t, marker)

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 1)
	assert.Equal(t, expectedStdin("Body.", 1, "First task", "Do the first thing."), records[0].Stdin,
		"gates must never be sent to claude")

	after := readTasksYAML(t, tasksPath)
	require.Len(t, after.Tasks, 1)
	assert.Equal(t, "completed", after.Tasks[0].State)
	assert.Equal(t, []gateFixture{{Cmd: "echo gate-output"}, {Cmd: "touch '" + marker + "'"}}, after.Shared.Gates,
		"gates must survive the save")
	assertNoTmpFile(t, tasksPath)
}

func TestGates_FailingGateFailsTheTaskAndBlocksTheNextRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	never := filepath.Join(dir, "never")

	tasksYAML := withShared("Body.", fmt.Sprintf(`    - cmd: test "a" = "b"
    - cmd: touch '%s'
`, never), `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})
	args := []string{"--skip-permissions", "--tasks=" + tasksPath}

	res := runGralph(t, 15*time.Second, args, env)

	const gateErr = `gate "test \"a\" = \"b\"" failed: exit status 1`
	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: "+gateErr)
	assert.NoFileExists(t, never, "gates after a failed gate must not run")
	require.Len(t, readFakeClaudeRecords(t, recordFile), 1, "expected the loop to stop before the second task")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, gateErr, taskErrors(after)[1])
	assert.Equal(t, "pending", taskStates(after)[2])
	assertNoTmpFile(t, tasksPath)

	// A gate failure is an ordinary failed task: the saved file must load
	// again, and the next run must refuse to start.
	again := runGralph(t, 15*time.Second, args, env)
	require.NotEqual(t, 0, again.exitCode, "stdout:\n%s\nstderr:\n%s", again.stdout, again.stderr)
	assert.Contains(t, again.stdout, "Some tasks failed previous runs:")
	assert.Len(t, readFakeClaudeRecords(t, recordFile), 1, "a blocked run must not invoke claude")
}

func TestGates_SkippedWhenTheSessionFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "gate-ran")

	tasksYAML := withShared("Body.", fmt.Sprintf(`    - cmd: touch '%s'
`, marker), `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKE_CLAUDE_OUTPUT": `{"state":"failed","error":"something broke"}` + "\n",
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: something broke")
	assert.NotContains(t, res.stdout, "gate:")
	assert.NoFileExists(t, marker, "a failed session must not run gates")
	assert.Equal(t, "something broke", taskErrors(readTasksYAML(t, tasksPath))[1])
}

func TestGates_TimedOutGateFailsTheTask(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	never := filepath.Join(dir, "never")

	tasksYAML := withShared("Body.", fmt.Sprintf(`    - cmd: sleep 30
      timeout: 200ms
    - cmd: touch '%s'
`, never), `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)
	env := gralphEnv(fakeClaudeDir, nil)

	start := time.Now()
	res := runGralph(t, 20*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	const gateErr = `gate "sleep 30" timed out after 200ms`
	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Less(t, time.Since(start), 15*time.Second, "the timeout must kill the gate instead of waiting out its sleep")
	assert.Contains(t, res.stderr, "task 1: First task failed: "+gateErr)
	assert.NoFileExists(t, never, "gates after a timed-out gate must not run")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, gateErr, taskErrors(after)[1])
	require.NotEmpty(t, after.Shared.Gates)
	assert.Equal(t, "200ms", after.Shared.Gates[0].Timeout, "timeout must survive the save as written")
	assertNoTmpFile(t, tasksPath)
}

func TestGates_GateTimeoutFlagOverridesTheGateTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksPath := writeTasksYAML(t, dir, withShared("Body.", `    - cmd: sleep 30
      timeout: 30s
    - cmd: echo no-timeout
`, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`))
	env := gralphEnv(fakeClaudeDir, nil)

	start := time.Now()
	res := runGralph(t, 20*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath, "--gate-timeout", "200ms"}, env)

	const gateErr = `gate "sleep 30" timed out after 200ms`
	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Less(t, time.Since(start), 15*time.Second, "the flag must cut the gate short")
	assert.Contains(t, res.stderr, "task 1: First task failed: "+gateErr)

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, gateErr, taskErrors(after)[1])
	assert.Equal(t, []gateFixture{{Cmd: "sleep 30", Timeout: "30s"}, {Cmd: "echo no-timeout"}}, after.Shared.Gates,
		"the flag must never be written to the tasks file")
}

func TestGates_DryRunListsEffectiveTimeouts(t *testing.T) {
	t.Parallel()
	tasksYAML := withShared("Body.", `    - cmd: go test ./...
      timeout: 30s
    - cmd: make lint
`, `tasks:
  - id: 1
    name: First task
    prompt: p
`)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "gate and default", want: "gate: go test ./...: 30s (gate)\ngate: make lint: 10m (default)\n"},
		{name: "flag", args: []string{"--gate-timeout", "2m"}, want: "gate: go test ./...: 2m (flag)\ngate: make lint: 2m (flag)\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, tasksPath := runDryRun(t, tasksYAML, func(p string) []string {
				return append([]string{"-t", p, "--dry-run"}, tt.args...)
			})

			require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
			assert.Contains(t, res.stdout, "First task\n"+tt.want+tasksPath+" is valid\n")
		})
	}
}

func TestGates_BadGateTimeoutFlagRunsNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		dryRun  bool
		wantErr string
	}{
		{name: "unitless", value: "30", wantErr: "--gate-timeout: must be a duration string such as 90s or 10m"},
		{name: "unitless dry run", value: "30", dryRun: true, wantErr: "--gate-timeout: must be a duration string such as 90s or 10m"},
		{name: "zero", value: "0s", wantErr: "--gate-timeout: must be greater than zero"},
		{name: "negative dry run", value: "-1m", dryRun: true, wantErr: "--gate-timeout: must be greater than zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tasksYAML := withShared("Body.", "", "tasks:\n  - id: 1\n    name: First task\n    prompt: p\n")
			tasksPath := writeTasksYAML(t, dir, tasksYAML)
			recordFile := filepath.Join(dir, "record.ndjson")
			env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})

			args := []string{"--skip-permissions", "--tasks=" + tasksPath, "--gate-timeout=" + tt.value}
			if tt.dryRun {
				args = append(args, "--dry-run")
			}

			res := runGralph(t, 15*time.Second, args, env)

			require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
			assert.Contains(t, res.stderr, tt.wantErr)
			assert.Empty(t, res.stdout, "nothing may load or run before the flag is checked")
			assert.NoFileExists(t, recordFile, "claude must not run")

			raw, err := os.ReadFile(tasksPath)
			require.NoError(t, err)
			assert.Equal(t, tasksYAML, string(raw), "the tasks file must be untouched")
		})
	}
}

// The flag is passed alone: with --tasks also missing, only a
// check that runs before the required-flag check reports the bad value.
func TestGates_BadGateTimeoutFlagIsReportedBeforeAnythingElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		arg  string
	}{
		{name: "unitless", arg: "--gate-timeout=30"},
		{name: "explicitly empty", arg: "--gate-timeout="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := runCLI(t, tt.arg)

			require.Equal(t, 1, result.exitCode, "stdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
			assert.Contains(t, result.stderr, "error: --gate-timeout: must be a duration string such as 90s or 10m\n")
			assert.NotContains(t, result.stderr, "required flag")
		})
	}
}

func TestGates_MultiLineGateErrorNamesOnlyTheFirstLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksPath := writeTasksYAML(t, dir, withShared("Body.", `    - cmd: |
        echo first-line
        exit 3
`, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`))
	env := gralphEnv(fakeClaudeDir, nil)

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	const gateErr = `gate "echo first-line" failed: exit status 3`
	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, gateErr, taskErrors(readTasksYAML(t, tasksPath))[1])
}

func TestGates_TaskLevelGatesAreRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tasksYAML := withShared("Body.", "", "tasks:\n  - id: 1\n    name: First task\n    prompt: do it\n    gates:\n      - cmd: \"true\"\n")
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	env := gralphEnv(fakeClaudeDir, nil)

	res := runGralph(t, 20*time.Second, []string{"--dry-run", "--tasks=" + tasksPath}, env)

	assert.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "tasks[0] (id 1): gates: gates are set once for the whole file, as shared.gates; see the HOWTO")
	raw, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(raw), "a rejected file must not be rewritten")
}

func TestGates_SharedGatesRunForEveryTask(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "gate-runs")

	tasksYAML := withShared("Body.", fmt.Sprintf(`    - cmd: echo ran >> '%s'
`, marker), `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	runs, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Equal(t, "ran\nran\n", string(runs), "the shared gate must run once per task")

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 2)
	assert.Equal(t, expectedStdin("Body.", 1, "First task", "Do the first thing."), records[0].Stdin,
		"gates must never be sent to claude")
	assert.Equal(t, expectedStdin("Body.", 2, "Second task", "Do the second thing."), records[1].Stdin,
		"gates must never be sent to claude")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, map[int]string{1: "completed", 2: "completed"}, taskStates(after))
	assert.Equal(t, []gateFixture{{Cmd: "echo ran >> '" + marker + "'"}}, after.Shared.Gates, "gates must survive the save")
}
