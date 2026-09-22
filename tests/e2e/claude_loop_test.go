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

// TestLoop_RunsEveryTaskInOrder drives gralph through a tasks.yaml with three
// tasks in mixed states (completed, pending, abandoned) using the fake
// claude fixture, and asserts the full happy path: gralph runs every task in
// file order regardless of state (there is no state filtering today), one
// invocation per task, and the exact stdin gralph sends to claude for each
// invocation matches the documented wire contract. gralph never writes
// tasks.yaml today, so the file must be byte-identical afterward.
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
    state: abandoned
`
	tasksPath := writeTasksYAML(t, dir, tasksYAML)
	original, err := os.ReadFile(tasksPath)
	require.NoError(t, err)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 3, "expected claude invoked once per task, in file order")

	wantArgv := []string{"--print", "--dangerously-skip-permissions"}
	wantTasks := []struct {
		id     int
		name   string
		prompt string
	}{
		{1, "First task", "Do the first thing."},
		{2, "Second task", "Do the second thing."},
		{3, "Third task", "Do the third thing."},
	}
	for i, want := range wantTasks {
		assert.Equal(t, wantArgv, records[i].Argv, "argv for invocation %d", i)
		assert.Equal(t, expectedStdin(sharedPrompt, want.id, want.name, want.prompt), records[i].Stdin, "stdin for invocation %d must match the wire contract exactly", i)
	}

	after, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after), "gralph must never write tasks.yaml")
}

// TestLoop_StopsOnFirstFailure covers the fail-fast branch: when claude exits
// non-zero on the first task, gralph exits non-zero, reports the failing
// task by id and name, invokes claude exactly once, and never starts (or
// prints the prompt for) the second task.
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
