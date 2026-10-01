package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const neitherFlagError = "error: pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox"

const bothFlagsError = "error: --sandbox-settings and --skip-permissions cannot be used together"

// writeSandboxSettings writes a settings fixture and returns its path.
func writeSandboxSettings(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "sandbox.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// sandboxRun is one gralph run over a single valid task, with the fake
// claude recording its argv.
type sandboxRun struct {
	res        gralphResult
	recordFile string
	attemptLog string
	tasksPath  string
}

func runWithFlags(t *testing.T, flags ...string) sandboxRun {
	t.Helper()
	return runTasksWithFlags(t, validTasksYAML(), flags...)
}

func runTasksWithFlags(t *testing.T, tasksYAML string, flags ...string) sandboxRun {
	t.Helper()
	dir := t.TempDir()
	run := sandboxRun{
		recordFile: filepath.Join(dir, "record.ndjson"),
		attemptLog: filepath.Join(dir, "attempts.log"),
		tasksPath:  writeTasksYAML(t, dir, tasksYAML),
	}
	promptPath := writePrompt(t, dir, "Follow the runbook.\n")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE":      run.recordFile,
		"FAKECLAUDE_ATTEMPT_LOG_FILE": run.attemptLog,
	})

	args := append([]string{"--prompt=" + promptPath, "--tasks=" + run.tasksPath}, flags...)
	run.res = runGralph(t, 15*time.Second, args, env)
	return run
}

func TestSandbox_ArgvCarriesForcedSettings(t *testing.T) {
	t.Parallel()
	content := `{"sandbox":{"enabled":false,"filesystem":{"denyRead":["~/"]}}}`
	settings := writeSandboxSettings(t, t.TempDir(), content)

	run := runWithFlags(t, "--sandbox-settings="+settings)
	require.Equal(t, 0, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)

	records := readFakeClaudeRecords(t, run.recordFile)
	require.Len(t, records, 1)
	want := []string{
		"--print", "--permission-mode", "acceptEdits", "--settings",
		`{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/"]}}}`,
	}
	assert.Equal(t, want, records[0].Argv)
	assert.Equal(t, expectedStdin("Follow the runbook.", 1, "First task", "Do the thing."), records[0].Stdin, "the stdin contract must not change")

	after, err := os.ReadFile(settings)
	require.NoError(t, err)
	assert.Equal(t, content, string(after), "the settings file must not be written")
	assert.Equal(t, "completed", taskStates(readTasksYAML(t, run.tasksPath))[1])
}

// TestSandbox_EveryTaskGetsTheSameSettings pins that the settings are read
// once and reused: a second task must not run with weaker flags than the
// first.
func TestSandbox_EveryTaskGetsTheSameSettings(t *testing.T) {
	t.Parallel()
	settings := writeSandboxSettings(t, t.TempDir(), `{"sandbox":{"filesystem":{"denyRead":["~/"]}}}`)
	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`

	run := runTasksWithFlags(t, tasksYAML, "--sandbox-settings="+settings)
	require.Equal(t, 0, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)

	records := readFakeClaudeRecords(t, run.recordFile)
	require.Len(t, records, 2)
	want := []string{
		"--print", "--permission-mode", "acceptEdits", "--settings",
		`{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/"]}}}`,
	}
	assert.Equal(t, want, records[0].Argv)
	assert.Equal(t, want, records[1].Argv)
	assert.Equal(t, expectedStdin("Follow the runbook.", 1, "First task", "Do the first thing."), records[0].Stdin)
	assert.Equal(t, expectedStdin("Follow the runbook.", 2, "Second task", "Do the second thing."), records[1].Stdin)

	states := taskStates(readTasksYAML(t, run.tasksPath))
	assert.Equal(t, "completed", states[1])
	assert.Equal(t, "completed", states[2])
}

func TestRun_NeedsOnePermissionFlag(t *testing.T) {
	t.Parallel()
	settings := writeSandboxSettings(t, t.TempDir(), `{}`)

	tests := []struct {
		name    string
		flags   []string
		wantErr string
	}{
		{name: "neither", wantErr: neitherFlagError},
		{name: "empty sandbox path", flags: []string{"--sandbox-settings="}, wantErr: neitherFlagError},
		{name: "both", flags: []string{"--sandbox-settings=" + settings, "--skip-permissions"}, wantErr: bothFlagsError},
		{name: "both on a dry run", flags: []string{"--sandbox-settings=" + settings, "--skip-permissions", "--dry-run"}, wantErr: bothFlagsError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := runWithFlags(t, tt.flags...)

			require.Equal(t, 1, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
			assert.Contains(t, run.res.stderr, tt.wantErr)
			assert.Equal(t, 0, countAttempts(t, run.attemptLog), "expected claude never invoked")
			// The fixture has no state key and a refused run never writes the
			// file, so an untouched task reads back as empty.
			assert.Empty(t, taskStates(readTasksYAML(t, run.tasksPath))[1], "expected the task file untouched")
		})
	}
}

func TestSandbox_BadSettingsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "missing", path: filepath.Join(dir, "missing.json"), wantErr: "is not accessible"},
		{name: "directory", path: dir, wantErr: "is not accessible"},
		{name: "not an object", path: writeSandboxSettings(t, t.TempDir(), `null`), wantErr: "must be a JSON object"},
		{name: "sandbox not an object", path: writeSandboxSettings(t, t.TempDir(), `{"sandbox":true}`), wantErr: "sandbox: must be a JSON object"},
	}
	modes := []struct {
		name  string
		flags []string
	}{
		{name: "run"},
		{name: "dry run", flags: []string{"--dry-run"}},
	}
	for _, tt := range tests {
		for _, mode := range modes {
			t.Run(tt.name+", "+mode.name, func(t *testing.T) {
				t.Parallel()
				run := runWithFlags(t, append([]string{"--sandbox-settings=" + tt.path}, mode.flags...)...)

				require.Equal(t, 1, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
				assert.Contains(t, run.res.stderr, "error: --sandbox-settings: ")
				assert.Contains(t, run.res.stderr, tt.wantErr)
				assert.Equal(t, 0, countAttempts(t, run.attemptLog), "expected claude never invoked")
			})
		}
	}
}

func TestDryRun_NamesSandboxSettings(t *testing.T) {
	t.Parallel()
	settings := writeSandboxSettings(t, t.TempDir(), `{}`)

	res, tasksPath := runDryRun(t, validTasksYAML(), func(p string) []string {
		return []string{"-t", p, "--dry-run", "--sandbox-settings=" + settings}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	want := "   ID  STATE      NAME\n" +
		"   --  ---------  ----\n" +
		"    1  PENDING    First task\n" +
		"sandbox settings: " + settings + "\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, res.stdout)
}
