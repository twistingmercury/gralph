package skillinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/version"
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

func TestInstall_WritesVersionFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dest, err := Install()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dest, "VERSION"))
	require.NoError(t, err)
	hash, err := Hash()
	require.NoError(t, err)
	assert.Equal(t, version.Version()+"\n"+hash+"\n", string(data))
}

func TestHash_Format(t *testing.T) {
	got, err := Hash()
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^sha256:[0-9a-f]{64}$`), got)
}

func TestHash_Stable(t *testing.T) {
	first, err := Hash()
	require.NoError(t, err)
	second, err := Hash()
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestHash_MatchesIndependentWalk(t *testing.T) {
	h := sha256.New()
	err := fs.WalkDir(skills.FS, skillName, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		data, err := skills.FS.ReadFile(path)
		require.NoError(t, err)
		h.Write([]byte(strings.TrimPrefix(path, skillName+"/")))
		h.Write(data)
		return nil
	})
	require.NoError(t, err)

	got, err := Hash()
	require.NoError(t, err)
	assert.Equal(t, "sha256:"+hex.EncodeToString(h.Sum(nil)), got)
}

func TestCheck_NotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := Check()
	require.Error(t, err)
	assert.Equal(t, "the gralph-docs-writer skill is not installed.\nRun: gralph --install-skill", err.Error())
}

func TestCheck_VersionMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dest, err := Install()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dest, "VERSION")))

	err = Check()
	require.Error(t, err)
	assert.Equal(t, "the gralph-docs-writer skill is outdated (installed by unknown, this is gralph "+version.Version()+").\nRun: gralph --install-skill", err.Error())
}

func TestCheck_HashDiffers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dest, err := Install()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dest, "VERSION"), []byte("v0.1.0\nsha256:old\n"), 0o600))

	err = Check()
	require.Error(t, err)
	assert.Equal(t, "the gralph-docs-writer skill is outdated (installed by v0.1.0, this is gralph "+version.Version()+").\nRun: gralph --install-skill", err.Error())
}

func TestCheck_FreshInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := Install()
	require.NoError(t, err)

	assert.NoError(t, Check())
}
