package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin that gralph no longer looks at the installed skill: users
// may edit it, so a run or dry run must not care whether it is missing,
// edited, or current.

// skillRun runs one real plain-mode session in home and returns the result
// and the attempt log path.
func skillRun(t *testing.T, home string) (gralphResult, string) {
	t.Helper()

	dir := t.TempDir()
	tasksPath := writeTasksYAML(t, dir, validTasksYAML())
	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"HOME":                        home,
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, defaultTimeout, []string{"--skip-permissions", "-t", tasksPath}, env)
	return res, attemptLog
}

func TestRun_NoSkillInstalled(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	res, attemptLog := skillRun(t, home)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, 1, countAttempts(t, attemptLog))
	assert.NoDirExists(t, filepath.Join(home, ".claude", "skills", "gralph-docs-writer"))
}

// TestRun_EditedSkillIsLeftAlone pins that a run never rewrites an edited
// skill. It passed against the old check too, which read only the VERSION
// stamp, so TestRun_SkillFromAnOlderGralphStillRuns is the one that proves
// the removal.
func TestRun_EditedSkillIsLeftAlone(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := gralphEnv(fakeClaudeDir, map[string]string{"HOME": home})
	res := runGralph(t, defaultTimeout, []string{"--install-skill"}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	skillPath := filepath.Join(home, ".claude", "skills", "gralph-docs-writer", "SKILL.md")
	original, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	edited := append(original, []byte("\nMy own tweak.\n")...)
	require.NoError(t, os.WriteFile(skillPath, edited, 0o600))

	res, attemptLog := skillRun(t, home)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, 1, countAttempts(t, attemptLog))
	after, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	assert.Equal(t, string(edited), string(after))
}

func TestDryRun_NoSkillInstalled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tasksPath := writeTasksYAML(t, dir, validTasksYAML())
	env := gralphEnv(fakeClaudeDir, map[string]string{"HOME": t.TempDir()})

	res := runGralph(t, defaultTimeout, []string{"-t", tasksPath, "--dry-run"}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
}

// oldGralphSkill writes a skill folder the way an older gralph left it: a
// SKILL.md and a VERSION stamp whose hash this binary would not match.
func oldGralphSkill(t *testing.T, home string) (skillPath, versionPath string) {
	t.Helper()

	dest := filepath.Join(home, ".claude", "skills", "gralph-docs-writer")
	require.NoError(t, os.MkdirAll(dest, 0o750))
	skillPath = filepath.Join(dest, "SKILL.md")
	versionPath = filepath.Join(dest, "VERSION")
	require.NoError(t, os.WriteFile(skillPath, []byte("old skill\n"), 0o600))
	require.NoError(t, os.WriteFile(versionPath, []byte("v0.0.1\nsha256:00\n"), 0o600))
	return skillPath, versionPath
}

func TestRun_SkillFromAnOlderGralphStillRuns(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	skillPath, versionPath := oldGralphSkill(t, home)

	res, attemptLog := skillRun(t, home)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, 1, countAttempts(t, attemptLog))
	skill, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	assert.Equal(t, "old skill\n", string(skill))
	stamp, err := os.ReadFile(versionPath)
	require.NoError(t, err)
	assert.Equal(t, "v0.0.1\nsha256:00\n", string(stamp))
}

func TestDryRun_SkillFromAnOlderGralphStillRuns(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	oldGralphSkill(t, home)
	tasksPath := writeTasksYAML(t, t.TempDir(), validTasksYAML())
	env := gralphEnv(fakeClaudeDir, map[string]string{"HOME": home})

	res := runGralph(t, defaultTimeout, []string{"-t", tasksPath, "--dry-run"}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
}
