package e2e

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// folderRun is one gralph run over a run folder holding a task file and,
// when sandboxJSON is not empty, a sandbox.json.
type folderRun struct {
	res        gralphResult
	dir        string
	recordFile string
	attemptLog string
}

func runFolder(t *testing.T, sandboxJSON string, flags ...string) folderRun {
	t.Helper()
	dir := t.TempDir()
	writeTasksYAML(t, dir, validTasksYAML())
	if sandboxJSON != "" {
		writeSandboxSettings(t, dir, sandboxJSON)
	}

	scratch := t.TempDir()
	run := folderRun{
		dir:        dir,
		recordFile: filepath.Join(scratch, "record.ndjson"),
		attemptLog: filepath.Join(scratch, "attempts.log"),
	}
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE":      run.recordFile,
		"FAKECLAUDE_ATTEMPT_LOG_FILE": run.attemptLog,
	})

	args := append([]string{"-d", dir}, flags...)
	run.res = runGralph(t, 15*time.Second, args, env)
	return run
}

// TestDirSandbox_FolderFileSandboxesTheRun pins that a sandbox.json in the -d
// folder stands in for --sandbox-settings.
func TestDirSandbox_FolderFileSandboxesTheRun(t *testing.T) {
	t.Parallel()
	run := runFolder(t, `{"sandbox":{"filesystem":{"denyRead":["~/"]}}}`)
	require.Equal(t, 0, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)

	records := readFakeClaudeRecords(t, run.recordFile)
	require.Len(t, records, 1)
	want := []string{
		"--print", "--permission-mode", "acceptEdits", "--settings",
		`{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/"]}}}`,
	}
	assert.Equal(t, want, records[0].Argv)
}

// TestDirSandbox_SkipPermissionsWinsOverTheFolderFile pins that opting out
// with --skip-permissions is not an error and ignores the file.
func TestDirSandbox_SkipPermissionsWinsOverTheFolderFile(t *testing.T) {
	t.Parallel()
	run := runFolder(t, `{}`, "--skip-permissions")
	require.Equal(t, 0, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)

	records := readFakeClaudeRecords(t, run.recordFile)
	require.Len(t, records, 1)
	assert.Equal(t, []string{"--print", "--dangerously-skip-permissions"}, records[0].Argv)
}

// TestDirSandbox_BadFolderFileFailsLoud pins that an invalid sandbox.json is
// an error and never a fallback to an unsandboxed run (ADR-014).
func TestDirSandbox_BadFolderFileFailsLoud(t *testing.T) {
	t.Parallel()
	run := runFolder(t, `[1]`)

	require.Equal(t, 1, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
	assert.Contains(t, run.res.stderr, "error: --sandbox-settings:")
	assert.Equal(t, 0, countAttempts(t, run.attemptLog), "expected claude never invoked")
}

// TestDirSandbox_NoFolderFileStillNeedsAFlag pins that a folder without a
// sandbox.json changes nothing: a run still needs a permission flag.
func TestDirSandbox_NoFolderFileStillNeedsAFlag(t *testing.T) {
	t.Parallel()
	run := runFolder(t, "")

	require.Equal(t, 1, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
	assert.Contains(t, run.res.stderr, neitherFlagError)
	assert.Equal(t, 0, countAttempts(t, run.attemptLog), "expected claude never invoked")
}

func TestDirSandbox_DryRunNamesTheFolderFile(t *testing.T) {
	t.Parallel()
	run := runFolder(t, `{}`, "--dry-run")

	require.Equal(t, 0, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
	want := "sandbox settings: " + filepath.Join(run.dir, "sandbox.json") + "\n"
	assert.Contains(t, run.res.stdout, want)
}
