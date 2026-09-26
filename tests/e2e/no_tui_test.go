package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoTUI_MatchesDefaultWithoutTerminal runs gralph with and without
// --no-tui on identical fresh copies of a two-task file. With no terminal
// attached both runs are plain, so their exit code, stdout, stderr, and
// saved tasks file must match.
func TestNoTUI_MatchesDefaultWithoutTerminal(t *testing.T) {
	t.Parallel()
	promptPath := writePrompt(t, t.TempDir(), "Follow the runbook.\n")

	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`
	env := gralphEnv(fakeClaudeDir, nil)

	run := func(extra ...string) (gralphResult, string) {
		tasksPath := writeTasksYAML(t, t.TempDir(), tasksYAML)
		args := append([]string{"-p", promptPath, "-t", tasksPath}, extra...)
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
