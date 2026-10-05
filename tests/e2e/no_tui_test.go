package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNonTerminal_DefaultsToPlain proves the non-terminal fallback: with no
// terminal attached, a run without --no-tui must be plain, so its exit code,
// stdout, stderr, and saved tasks file match a --no-tui run on an identical
// fresh copy of a two-task file. It cannot detect --no-tui being ignored;
// cmd/main's isPlain unit test covers that.
func TestNonTerminal_DefaultsToPlain(t *testing.T) {
	t.Parallel()

	tasksYAML := withShared("Follow the runbook.", "", `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`)
	env := gralphEnv(fakeClaudeDir, nil)

	run := func(extra ...string) (gralphResult, string) {
		tasksPath := writeTasksYAML(t, t.TempDir(), tasksYAML)
		args := append([]string{"--skip-permissions", "-t", tasksPath}, extra...)
		res := runGralph(t, 15*time.Second, args, env)
		saved, err := os.ReadFile(tasksPath)
		require.NoError(t, err)
		return res, string(saved)
	}

	plain, plainSaved := run("--no-tui")
	def, defSaved := run()

	require.Equal(t, 0, plain.exitCode, "stdout:\n%s\nstderr:\n%s", plain.stdout, plain.stderr)
	assert.Equal(t, plain.exitCode, def.exitCode)
	assert.Equal(t, plain.stdout, def.stdout)
	assert.Equal(t, plain.stderr, def.stderr)
	assert.Equal(t, plainSaved, defSaved)
}
