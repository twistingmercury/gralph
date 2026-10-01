package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers --log-dir (ADR-016). The suite has no terminal, so gralph
// always runs plain mode here and only the flag's plain-mode rules can be
// pinned: a real run refuses it, and a dry run ignores it. The record itself
// is covered by the unit tests of internal/runlog, internal/looper, and
// internal/tui.

const logDirPlainError = "error: --log-dir only works with the full-screen view\n"

// runWithLogDir runs gralph on a one-task file with --log-dir plus extra
// flags and returns the result, the task file's path, the log directory, and
// the claude attempt log.
func runWithLogDir(t *testing.T, logDirArg func(logDir string) string, extra ...string) (res gralphResult, tasksPath, logDir, attemptLog string) {
	t.Helper()
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	tasksYAML := validTasksYAML()
	tasksPath = writeTasksYAML(t, dir, tasksYAML)
	logDir = filepath.Join(dir, "logs")
	attemptLog = filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog})
	logArg := logDirArg(logDir)
	args := append([]string{"--skip-permissions", "-p", promptPath, "-t", tasksPath, logArg}, extra...)

	res = runGralph(t, 15*time.Second, args, env)
	return res, tasksPath, logDir, attemptLog
}

func withLogDir(logDir string) string { return "--log-dir=" + logDir }

func withEmptyLogDir(string) string { return "--log-dir=" }

func assertLogDirRefused(t *testing.T, extra ...string) {
	t.Helper()
	res, tasksPath, logDir, attemptLog := runWithLogDir(t, withLogDir, extra...)

	require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, logDirPlainError, res.stderr)
	assert.Empty(t, res.stdout)
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
	assert.NoDirExists(t, logDir)
	raw, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	want := validTasksYAML()
	assert.Equal(t, want, string(raw), "expected the task file to be unchanged")
}

func TestLogDir_NoTUIIsRefused(t *testing.T) {
	t.Parallel()
	assertLogDirRefused(t, "--no-tui")
}

// With no terminal the run is plain without --no-tui, so the same refusal
// applies.
func TestLogDir_NoTerminalIsRefused(t *testing.T) {
	t.Parallel()
	assertLogDirRefused(t)
}

func TestLogDir_EmptyValueCountsAsNotPassed(t *testing.T) {
	t.Parallel()
	res, tasksPath, logDir, attemptLog := runWithLogDir(t, withEmptyLogDir, "--no-tui")

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, 1, countAttempts(t, attemptLog), "expected the run to go ahead")
	assert.Equal(t, "completed", taskStates(readTasksYAML(t, tasksPath))[1])
	assert.NoDirExists(t, logDir)
}

func TestLogDir_DryRunIgnoresIt(t *testing.T) {
	t.Parallel()
	logDir := filepath.Join(t.TempDir(), "logs")
	res, tasksPath := runDryRun(t, validTasksYAML(), func(p string) []string {
		return []string{"-t", p, "--dry-run", "--log-dir", logDir}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	want := "   ID  STATE      NAME\n" +
		"   --  ---------  ----\n" +
		"    1  PENDING    First task\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, res.stdout, "the output is the same as without --log-dir")
	assert.Empty(t, res.stderr)
	assert.NoDirExists(t, logDir)
}
