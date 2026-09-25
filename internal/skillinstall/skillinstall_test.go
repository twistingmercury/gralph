package skillinstall

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/skills"
)

func TestInstall_WritesEmbeddedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dest, err := Install()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude", "skills", "gralph-docs-writer"), dest)

	var files []string
	err = fs.WalkDir(skills.FS, skillName, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		want, err := skills.FS.ReadFile(path)
		require.NoError(t, err)
		rel, err := filepath.Rel(skillName, path)
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(dest, rel))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), rel)
		files = append(files, rel)
		return nil
	})
	require.NoError(t, err)
	assert.Contains(t, files, "SKILL.md")
	assert.Contains(t, files, filepath.Join("templates", "tasks_template.yaml"))
	assert.Contains(t, files, filepath.Join("templates", "prompt_template.md"))
}

func TestInstall_ReplacesExistingFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dest, err := Install()
	require.NoError(t, err)
	stray := filepath.Join(dest, "stray.txt")
	require.NoError(t, os.WriteFile(stray, []byte("stale"), 0o600))

	_, err = Install()
	require.NoError(t, err)

	assert.NoFileExists(t, stray)
	assert.FileExists(t, filepath.Join(dest, "SKILL.md"))
}
