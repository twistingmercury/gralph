package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers gates (ADR-013): commands in a task's gates list that
// gralph runs itself, through sh, after the session reports completed. A
// task is completed only when every gate exits zero, and gates are never
// sent to claude.

func TestGates_PassingGatesCompleteTheTask(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	marker := filepath.Join(dir, "gate-ran")

	tasksYAML := fmt.Sprintf(`tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: echo gate-output
      - cmd: touch '%s'
`, marker)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

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
	assert.Equal(t, []gateFixture{{Cmd: "echo gate-output"}, {Cmd: "touch '" + marker + "'"}}, after.Tasks[0].Gates,
		"gates must survive the save")
	assertNoTmpFile(t, tasksPath)
}

func TestGates_FailingGateFailsTheTaskAndBlocksTheNextRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	never := filepath.Join(dir, "never")

	tasksYAML := fmt.Sprintf(`tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: test "a" = "b"
      - cmd: touch '%s'
  - id: 2
    name: Second task
    prompt: Do the second thing.
`, never)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})
	args := []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}

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
	promptPath := writePrompt(t, dir, "Body.\n")
	marker := filepath.Join(dir, "gate-ran")

	tasksYAML := fmt.Sprintf(`tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: touch '%s'
`, marker)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKE_CLAUDE_OUTPUT": `{"state":"failed","error":"something broke"}` + "\n",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: something broke")
	assert.NotContains(t, res.stdout, "gate:")
	assert.NoFileExists(t, marker, "a failed session must not run gates")
	assert.Equal(t, "something broke", taskErrors(readTasksYAML(t, tasksPath))[1])
}
