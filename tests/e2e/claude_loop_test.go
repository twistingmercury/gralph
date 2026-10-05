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

// TestLoop_RunsEveryTaskInOrder drives gralph through a tasks.yaml with
// three tasks in mixed states (completed, pending, and one with no explicit
// state, which normalizes to pending) using the fake claude fixture, and
// asserts the full happy path: the completed task is skipped entirely (no
// claude invocation, its prompt never echoed, just the skip message on
// stdout), the non-completed tasks run in file order with the exact
// documented stdin wire contract, and once every invocation exits 0 the
// tasks.yaml on disk is rewritten with every task marked completed. The ids
// are out of order, so sorting by id would change both the run order and the
// saved order.
func TestLoop_RunsEveryTaskInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	promptBody := "PROMPT BODY MARKER\nFollow the runbook.\n"
	sharedPrompt := strings.TrimSpace(promptBody)

	tasksYAML := `tasks:
  - id: 20
    name: First task
    prompt: Do the first thing.
    state: completed
  - id: 30
    name: Second task
    prompt: Do the second thing.
  - id: 10
    name: Third task
    prompt: Do the third thing.
    state: pending
`
	tasksPath := writeTasksYAML(t, dir, withShared(promptBody, "", tasksYAML))

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	assert.Contains(t, res.stdout, "task 20: First task already completed, skipping")
	assert.NotContains(t, res.stdout, "Do the first thing.", "expected the completed task's prompt never to be printed")

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 2, "expected claude invoked only for the non-completed tasks, in file order")

	wantArgv := []string{"--print", "--dangerously-skip-permissions"}
	wantTasks := []struct {
		id     int
		name   string
		prompt string
	}{
		{30, "Second task", "Do the second thing."},
		{10, "Third task", "Do the third thing."},
	}
	for i, want := range wantTasks {
		assert.Equal(t, wantArgv, records[i].Argv, "argv for invocation %d", i)
		assert.Equal(t, expectedStdin(sharedPrompt, want.id, want.name, want.prompt), records[i].Stdin, "stdin for invocation %d must match the wire contract exactly", i)
	}

	after := readTasksYAML(t, tasksPath)
	require.Len(t, after.Tasks, 3)
	for i, wantID := range []int{20, 30, 10} {
		assert.Equal(t, wantID, after.Tasks[i].ID, "expected the saved file to keep file order at index %d", i)
		assert.Equal(t, "completed", after.Tasks[i].State, "expected task %d to be completed", after.Tasks[i].ID)
	}

	assertNoTmpFile(t, tasksPath)
}

// TestLoop_FailedTaskRefusesToRun verifies that a task file containing a
// failed task is refused before any work starts: gralph prints the task
// summary table on stdout, exits 1, never invokes claude, and leaves the task file
// byte-for-byte unchanged with no temporary file behind.
func TestLoop_FailedTaskRefusesToRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksYAML := withShared("Body.", "", `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
    state: failed
    error: boom
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, "Some tasks failed previous runs:\n")
	assert.Contains(t, res.stdout, "   1  PENDING    First task\n")
	assert.Contains(t, res.stdout, "❌  2  \033[1;91mFAILED   \033[0m  Second task  \033[1;91m← Needs review!\033[0m\n")
	assert.Contains(t, res.stderr, "fix the failed tasks and set their state to pending before running")
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")

	raw, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(raw), "expected the task file to be unchanged")

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

	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`
	tasksPath := writeTasksYAML(t, dir, withShared("Body.", "", tasksYAML))

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_EXIT_CODE":        "3",
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
		"FAKECLAUDE_STDERR_MSG":       "claude-stderr-marker",
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: exit status 3")
	assert.Contains(t, res.stderr, "claude-stderr-marker", "expected claude's stderr to pass through")

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
	sharedPrompt := strings.TrimSpace(promptBody)

	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`
	tasksPath := writeTasksYAML(t, dir, withShared(promptBody, "", tasksYAML))

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, gralphEnv(fakeClaudeDir, nil))
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

	tasksPath := writeTasksYAML(t, dir, withShared("Body.", "", `tasks:
  - id: 1
    name: Only task
    prompt: Do the thing.
`))

	env := gralphEnvWithPath(emptyPathDir, nil)
	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: Only task failed")
	assert.Contains(t, res.stderr, "executable file not found")
}

// TestIterationsFlagRejected verifies that --iterations no longer exists as
// a flag: gralph must reject it as unknown rather than silently accepting or
// ignoring it. If the flag came back, this run would still exit non-zero for
// the missing --tasks and usage would list the flag, so the unknown-flag
// line and pflag's exit code 2 are what pin it.
func TestIterationsFlagRejected(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--iterations=3")
	require.Equal(t, 2, result.exitCode, "stdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
	assert.Contains(t, result.stderr, "unknown flag: --iterations\n")
	assert.NotContains(t, result.stderr, "required flag")
}

// TestStart_EmptyPrompt verifies that an empty shared prompt fails startup
// before claude is ever invoked.
func TestStart_EmptyPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksPath := writeTasksYAML(t, dir, "shared:\n  prompt: \"\"\n"+validTasks)

	assertStartupFailure(t, dir, tasksPath, "shared.prompt: must not be empty or whitespace")
}

// TestStart_WhitespacePrompt verifies that a whitespace-only shared prompt
// fails startup before claude is ever invoked.
func TestStart_WhitespacePrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksPath := writeTasksYAML(t, dir, "shared:\n  prompt: \"   \\t\\n  \"\n"+validTasks)

	assertStartupFailure(t, dir, tasksPath, "shared.prompt: must not be empty or whitespace")
}

// TestStart_MissingSharedBlock verifies that a task file without a shared
// block fails startup before claude is ever invoked.
func TestStart_MissingSharedBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksPath := writeTasksYAML(t, dir, validTasks)

	assertStartupFailure(t, dir, tasksPath, "shared: is required")
}

// TestStart_AbandonedStateRejected verifies that "abandoned" -- a state
// value that is no longer valid -- fails startup via the same invalid-state
// error as any other unrecognized value, naming the offending task id and
// state, and that claude is never invoked.
func TestStart_AbandonedStateRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tasksPath := writeTasksYAML(t, dir, withShared("Body.", "", `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
    state: abandoned
`))

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

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

	tasksPath := writeTasksYAML(t, dir, withShared("Body.", "", "tasks: []\n"))

	assertStartupFailure(t, dir, tasksPath, "tasks: must contain at least one task")
}

// assertStartupFailure runs gralph with the given task file and
// asserts the shared startup-failure contract: stderr mentions the loop
// runner failed to start and the given reason, gralph exits non-zero, and
// claude is never invoked.
func assertStartupFailure(t *testing.T, dir, tasksPath, want string) {
	t.Helper()

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "failed to start loop runner")
	assert.Contains(t, res.stderr, want)
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
}

// validTasks is the single task every fixture shares; validTasksYAML puts a
// shared prompt above it.
const validTasks = `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`

// validTasksYAML returns a minimal, well-formed single-task tasks.yaml for
// tests where the task file itself is not under test.
func validTasksYAML() string {
	return withShared("Follow the runbook.", "", validTasks)
}
