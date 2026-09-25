package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkillCheck verifies runs and dry-runs refuse to start while the
// installed skill is missing or outdated, and proceed once it matches.
func TestSkillCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	tasksPath := writeTasksYAML(t, dir, validTasksYAML())
	promptPath := writePrompt(t, dir, "Shared prompt.")
	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"HOME":                        home,
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})
	dryRun := []string{"-t", tasksPath, "--dry-run"}

	res := runGralph(t, defaultTimeout, dryRun, env)
	require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "is not installed")
	assert.Contains(t, res.stderr, "Run: gralph --install-skill")

	res = runGralph(t, defaultTimeout, []string{"--install-skill"}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	res = runGralph(t, defaultTimeout, dryRun, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	versionPath := filepath.Join(home, ".claude", "skills", "gralph-docs-writer", "VERSION")
	stamp, err := os.ReadFile(versionPath)
	require.NoError(t, err)
	lines := strings.Split(string(stamp), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "VERSION:\n%s", stamp)
	lines[1] = "sha256:0000"
	require.NoError(t, os.WriteFile(versionPath, []byte(strings.Join(lines, "\n")), 0o600))

	for _, args := range [][]string{dryRun, {"-t", tasksPath, "-p", promptPath}} {
		res = runGralph(t, defaultTimeout, args, env)
		require.Equal(t, 1, res.exitCode, "args=%v\nstdout:\n%s\nstderr:\n%s", args, res.stdout, res.stderr)
		assert.Contains(t, res.stderr, "is outdated")
		assert.Contains(t, res.stderr, "Run: gralph --install-skill")
	}
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
}
