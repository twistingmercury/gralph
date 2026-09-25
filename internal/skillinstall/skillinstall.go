// Package skillinstall installs the embedded gralph-docs-writer skill for Claude Code.
package skillinstall

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/twistingmercury/gralph/skills"
)

const skillName = "gralph-docs-writer"

// Install replaces ~/.claude/skills/gralph-docs-writer with the skill
// embedded in the binary and returns the installed path.
func Install() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(home, ".claude", "skills", skillName)

	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}

	err = fs.WalkDir(skills.FS, skillName, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skillName, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		return "", err
	}

	return dest, nil
}
