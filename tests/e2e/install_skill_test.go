package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallSkill verifies --install-skill writes the embedded skill folder
// under $HOME/.claude/skills without --prompt or --tasks, and that a second
// run replaces the folder.
func TestInstallSkill(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := gralphEnv(fakeClaudeDir, map[string]string{"HOME": home})
	dest := filepath.Join(home, ".claude", "skills", "gralph-docs-writer")

	res := runGralph(t, defaultTimeout, []string{"--install-skill"}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, dest, strings.TrimSpace(res.stdout))

	skill, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(skill), "name: gralph-docs-writer")
	assert.FileExists(t, filepath.Join(dest, "templates", "tasks_template.yaml"))
	assert.FileExists(t, filepath.Join(dest, "templates", "prompt_template.md"))

	stray := filepath.Join(dest, "stray.txt")
	require.NoError(t, os.WriteFile(stray, []byte("stale"), 0o600))

	res = runGralph(t, defaultTimeout, []string{"--install-skill"}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.NoFileExists(t, stray)
	assert.FileExists(t, filepath.Join(dest, "SKILL.md"))
}
