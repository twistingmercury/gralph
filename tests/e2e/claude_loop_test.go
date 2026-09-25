package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoop_RunsEveryTaskInOrder drives gralph through a tasks.yaml with
// three tasks in mixed states (completed, pending, and one with no explicit
// state, which normalizes to pending) using the fake claude fixture, and
// asserts the full happy path: the completed task is skipped entirely (no
// claude invocation, its prompt never echoed, just the skip message on
// stdout), the non-completed tasks run in file order with the exact
// documented stdin wire contract, and once every invocation exits 0 the
// tasks.yaml on disk is rewritten with every task marked completed.
func TestLoop_RunsEveryTaskInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptBody := "PROMPT BODY MARKER\nFollow the runbook.\n"
	promptPath := writePrompt(t, dir, promptBody)
	sharedPrompt := strings.TrimSpace(promptBody)

	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    state: completed
  - id: 2
    name: Second task
    prompt: Do the second thing.
  - id: 3
    name: Third task
    prompt: Do the third thing.
    state: pending
`
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	assert.Contains(t, res.stdout, "task 1: First task already completed, skipping")
	assert.NotContains(t, res.stdout, "Do the first thing.", "expected the completed task's prompt never to be printed")

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 2, "expected claude invoked only for the non-completed tasks, in file order")

	wantArgv := []string{"--print", "--dangerously-skip-permissions"}
	wantTasks := []struct {
		id     int
		name   string
		prompt string
	}{
		{2, "Second task", "Do the second thing."},
		{3, "Third task", "Do the third thing."},
	}
	for i, want := range wantTasks {
		assert.Equal(t, wantArgv, records[i].Argv, "argv for invocation %d", i)
		assert.Equal(t, expectedStdin(sharedPrompt, want.id, want.name, want.prompt), records[i].Stdin, "stdin for invocation %d must match the wire contract exactly", i)
	}

	after := readTasksYAML(t, tasksPath)
	require.Len(t, after.Tasks, 3)
	states := taskStates(after)
	assert.Equal(t, "completed", states[1], "expected the already-completed task to remain completed")
	assert.Equal(t, "completed", states[2], "expected the second task to be written back as completed")
	assert.Equal(t, "completed", states[3], "expected the third task to be written back as completed")

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_FailedTaskReRunsAndCompletes verifies that a task left in the
// failed state by a prior run is re-run (not skipped) on a later gralph
// invocation, and is written back as completed once claude exits 0 for it.
func TestLoop_FailedTaskReRunsAndCompletes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    state: failed
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

	after := readTasksYAML(t, tasksPath)
	states := taskStates(after)
	assert.Equal(t, "completed", states[1], "expected the re-run task to be written back as completed")

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_StopsOnFirstFailure covers the fail-fast branch: when claude exits
// non-zero on the first task, gralph exits non-zero, reports the failing
// task by id and name, invokes claude exactly once, and never starts (or
// prints the prompt for) the second task. It also asserts the write-back
// contract: the failing task is saved as failed and the unstarted task is
// left untouched as pending.
func TestLoop_StopsOnFirstFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_EXIT_CODE":        "3",
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: exit status 3")

	assert.Equal(t, 1, countAttempts(t, attemptLog), "expected claude invoked exactly once")
	assert.NotContains(t, res.stdout, "Do the second thing.", "expected the second task's prompt never to be printed")

	after := readTasksYAML(t, tasksPath)
	states := taskStates(after)
	assert.Equal(t, "failed", states[1], "expected the failing task to be written back as failed")
	assert.Equal(t, "pending", states[2], "expected the unstarted task to remain pending")

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_PromptEchoedToStdout verifies gralph prints the exact combined
// prompt (shared prompt file plus task) to stdout before invoking claude for
// each task.
func TestLoop_PromptEchoedToStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptBody := "Follow the runbook.\n"
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

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, gralphEnv(fakeClaudeDir, nil))
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	assert.Contains(t, res.stdout, expectedStdin(sharedPrompt, 1, "First task", "Do the first thing."))
	assert.Contains(t, res.stdout, expectedStdin(sharedPrompt, 2, "Second task", "Do the second thing."))
}

// TestLoop_ClaudeMissingFromPath verifies that when claude cannot be
// resolved on PATH at all, gralph fails fast with an error naming the task
// and the underlying PATH lookup failure.
func TestLoop_ClaudeMissingFromPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	emptyPathDir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: Only task
    prompt: Do the thing.
`)

	env := gralphEnvWithPath(emptyPathDir, nil)
	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: Only task failed")
	assert.Contains(t, res.stderr, "executable file not found")
}

// TestIterationsFlagRejected verifies that --iterations no longer exists as
// a flag: gralph must reject it as unknown rather than silently accepting or
// ignoring it.
func TestIterationsFlagRejected(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--iterations=3")
	require.NotEqual(t, 0, result.exitCode, "stdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
	assert.Contains(t, strings.ToLower(result.stderr), "iterations")
}

// TestStart_EmptyPrompt verifies that an empty prompt file fails startup
// before claude is ever invoked.
func TestStart_EmptyPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "")
	tasksPath := writeTasksYAML(t, dir, validTasksYAML())

	assertStartupFailure(t, dir, promptPath, tasksPath)
}

// TestStart_WhitespacePrompt verifies that a whitespace-only prompt file
// fails startup before claude is ever invoked.
func TestStart_WhitespacePrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "   \t\n  ")
	tasksPath := writeTasksYAML(t, dir, validTasksYAML())

	assertStartupFailure(t, dir, promptPath, tasksPath)
}

// TestStart_InvalidTaskState verifies that a tasks.yaml with an invalid
// state value fails startup before claude is ever invoked.
func TestStart_InvalidTaskState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
    state: bogus
`)

	assertStartupFailure(t, dir, promptPath, tasksPath)
}

// TestStart_AbandonedStateRejected verifies that "abandoned" -- a state
// value that is no longer valid -- fails startup via the same invalid-state
// error as any other unrecognized value, naming the offending task id and
// state, and that claude is never invoked.
func TestStart_AbandonedStateRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
    state: abandoned
`)

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "failed to start loop runner")
	assert.Contains(t, res.stderr, `tasks[0] (id 1): state: must be pending, completed, or failed, got "abandoned"`)
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
}

// TestStart_EmptyTaskList verifies that a tasks.yaml with no tasks fails
// startup before claude is ever invoked.
func TestStart_EmptyTaskList(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, "tasks: []\n")

	assertStartupFailure(t, dir, promptPath, tasksPath)
}

// assertStartupFailure runs gralph with the given prompt/tasks files and
// asserts the shared startup-failure contract: stderr mentions the loop
// runner failed to start, gralph exits non-zero, and claude is never
// invoked.
func assertStartupFailure(t *testing.T, dir, promptPath, tasksPath string) {
	t.Helper()

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "failed to start loop runner")
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
}

// validTasksYAML returns a minimal, well-formed single-task tasks.yaml body
// for startup-failure tests where the tasks file itself is not under test.
func validTasksYAML() string {
	return `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`
}
