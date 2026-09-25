package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the session-result contract: claude is expected to end
// its stdout with one JSON object on the last non-blank line,
// {"state": "completed"|"failed", "error": "..."}, which gralph parses to
// decide the outcome independently of claude's exit code (see CLAUDE.md's
// "New behavior (approved design)" table). The fake claude fixture is driven
// via the FAKE_CLAUDE_OUTPUT env var (see testdata/fakeclaude/main.go): when
// set, its value is written verbatim to stdout in place of the fixture's
// default result line.

// TestLoop_JSONFailedResult_WritesErrorAndStopsLoop verifies that an exit-0
// session whose result line reports state "failed" is still treated as a
// failure: the error from the JSON is written back to tasks.yaml, reported
// on stderr, the loop stops before the next task, and claude's own stdout is
// still streamed through to gralph's stdout even though gralph also parses
// it.
func TestLoop_JSONFailedResult_WritesErrorAndStopsLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptBody := "Body.\n"
	promptPath := writePrompt(t, dir, promptBody)
	sharedPrompt := strings.TrimSpace(promptBody)

	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	resultLine := `{"state":"failed","error":"something broke"}` + "\n"
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
		"FAKE_CLAUDE_OUTPUT":     resultLine,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: something broke")

	// Claude's own stdout (the result line) must still be streamed through to
	// gralph's stdout, even though gralph also parses it internally.
	assert.Contains(t, res.stdout, resultLine, "expected claude's session output to still be streamed to gralph's stdout")

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 1, "expected the loop to stop before the second task")
	assert.Equal(t, expectedStdin(sharedPrompt, 1, "First task", "Do the first thing."), records[0].Stdin,
		"expected the wire contract to be unchanged: the error text must never be sent to claude")
	assert.NotContains(t, records[0].Stdin, "something broke", "the error text must never appear in what claude receives")

	after := readTasksYAML(t, tasksPath)
	states := taskStates(after)
	errs := taskErrors(after)
	assert.Equal(t, "failed", states[1], "expected the failing task to be written back as failed")
	assert.Equal(t, "something broke", errs[1], "expected the JSON error to be written back verbatim")
	assert.Equal(t, "pending", states[2], "expected the unstarted task to remain pending")
	assert.Empty(t, errs[2])

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_MissingResultLine_TreatedAsFailed verifies that a zero-exit
// session whose stdout has no valid JSON result line at all is treated as a
// failure with a fixed, documented error message.
func TestLoop_MissingResultLine_TreatedAsFailed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKE_CLAUDE_OUTPUT": "some text\n",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: no valid result line in session output")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, "no valid result line in session output", taskErrors(after)[1])

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_InvalidJSONResult_TreatedAsFailed is the invalid-JSON counterpart
// of TestLoop_MissingResultLine_TreatedAsFailed: the last non-blank line
// exists but does not parse as the expected JSON object, which must be
// treated identically to a missing result line.
func TestLoop_InvalidJSONResult_TreatedAsFailed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKE_CLAUDE_OUTPUT": "{not valid json}\n",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: no valid result line in session output")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, "no valid result line in session output", taskErrors(after)[1])

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_NonZeroExitWithFailedJSON_UsesJSONError verifies that when claude
// exits non-zero but still produced a valid, non-blank failed result line,
// the JSON error (not the generic exit error) is what gets reported and
// persisted.
func TestLoop_NonZeroExitWithFailedJSON_UsesJSONError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_EXIT_CODE": "5",
		"FAKE_CLAUDE_OUTPUT":   `{"state":"failed","error":"boom"}` + "\n",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: boom")
	assert.NotContains(t, res.stderr, "exit status", "expected the JSON error to take precedence over the generic exit error")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, "boom", taskErrors(after)[1])

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_NonZeroExitWithNoResultLine_UsesExitError verifies that when
// claude exits non-zero and produced no result line at all, the generic exit
// error is what gets reported and persisted (the pre-existing fail-fast
// behavior).
func TestLoop_NonZeroExitWithNoResultLine_UsesExitError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_EXIT_CODE": "1",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: exit status 1")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, "exit status 1", taskErrors(after)[1])

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_FencedResultLine_Completes verifies that a result line wrapped in
// a Markdown code fence (``` or ```json) is still recognized: fence-only
// lines are skipped when gralph looks for the last non-blank line.
func TestLoop_FencedResultLine_Completes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
`)

	fencedOutput := "```json\n{\"state\":\"completed\",\"error\":\"\"}\n```\n"
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKE_CLAUDE_OUTPUT": fencedOutput,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, fencedOutput, "expected claude's fenced output to still be streamed to gralph's stdout")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "completed", taskStates(after)[1])
	assert.Empty(t, taskErrors(after)[1])

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_PreviouslyFailedTaskWithError_RerunsAndClearsError verifies that
// a task left failed with a persisted error by a prior run is re-run, and
// once it completes, its stored error is cleared entirely (the `error` YAML
// key is absent, not merely blank) rather than left stale alongside the new
// completed state.
func TestLoop_PreviouslyFailedTaskWithError_RerunsAndClearsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    state: failed
    error: boom old
`
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 1, "expected the previously failed task to be re-run rather than skipped")
	assert.NotContains(t, records[0].Stdin, "boom old", "the stale error must never be sent to claude")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "completed", taskStates(after)[1])
	assert.Empty(t, taskErrors(after)[1])

	raw, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "error:", "expected the error key to be fully removed once the task completes")

	assertNoTmpFile(t, tasksPath)
}
