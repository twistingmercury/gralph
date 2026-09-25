package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDryRun writes tasksYAML to a temp dir, runs gralph with the args built
// from its path, and asserts claude is never invoked and the task file is
// left byte-for-byte unchanged with no temp file behind.
func runDryRun(t *testing.T, tasksYAML string, args func(tasksPath string) []string) (gralphResult, string) {
	t.Helper()
	dir := t.TempDir()
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, args(tasksPath), env)

	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
	raw, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(raw), "expected the task file to be unchanged")
	assertNoTmpFile(t, tasksPath)

	return res, tasksPath
}

func TestDryRun_ValidFile(t *testing.T) {
	t.Parallel()
	res, tasksPath := runDryRun(t, validTasksYAML(), func(p string) []string {
		return []string{"-t", p, "--dry-run"}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, tasksPath+" is valid\n", res.stdout)
}

func TestDryRun_InvalidFile(t *testing.T) {
	t.Parallel()
	tasksYAML := "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n"
	res, tasksPath := runDryRun(t, tasksYAML, func(p string) []string {
		return []string{"--tasks=" + p, "--dry-run"}
	})

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "failed to parse tasks yaml")
	assert.NotContains(t, res.stdout, tasksPath+" is valid")
}

func TestDryRun_FlagsFailedTaskAndExitsZero(t *testing.T) {
	t.Parallel()
	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
    state: failed
    error: boom
`
	res, tasksPath := runDryRun(t, tasksYAML, func(p string) []string {
		return []string{"-t", p, "--dry-run"}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	want := "❌ \033[1;31mtask 2: Second task is failed and needs manual intervention before execution\033[0m\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, res.stdout)
}

func TestDryRun_IgnoresNonexistentPrompt(t *testing.T) {
	t.Parallel()
	res, tasksPath := runDryRun(t, validTasksYAML(), func(p string) []string {
		return []string{"-t", p, "-p", "/nonexistent/prompt.md", "--dry-run"}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, tasksPath+" is valid\n", res.stdout)
}

func TestDryRun_MissingTasksFlag(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--dry-run")
	require.NotEqual(t, 0, result.exitCode, "expected non-zero exit when --tasks is missing")
	assert.Contains(t, result.stderr, "--tasks")
	assert.NotContains(t, result.stderr, "--prompt not set")
}
