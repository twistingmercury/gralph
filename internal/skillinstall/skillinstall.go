// Package skillinstall installs the embedded gralph-docs-writer skill for Claude Code.
package skillinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/twistingmercury/gralph/internal/version"
	"github.com/twistingmercury/gralph/skills"
)

const skillName = "gralph-docs-writer"

// Install replaces ~/.claude/skills/gralph-docs-writer with the skill
// embedded in the binary, writes a VERSION file holding the gralph version
// and Hash(), and returns the installed path.
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

	hash, err := Hash()
	if err != nil {
		return "", err
	}
	stamp := version.Version() + "\n" + hash + "\n"
	if err := os.WriteFile(filepath.Join(dest, "VERSION"), []byte(stamp), 0o600); err != nil {
		return "", err
	}

	return dest, nil
}

// Hash returns a SHA-256 content hash of the embedded skill: each file's
// slash-separated relative path, then its bytes, in fs.WalkDir order.
func Hash() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(skills.FS, skillName, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(strings.TrimPrefix(path, skillName+"/")))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// Check returns nil when ~/.claude/skills/gralph-docs-writer/VERSION exists
// and its second line equals Hash(); otherwise it returns an error telling
// the user to run gralph --install-skill.
func Check() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dest := filepath.Join(home, ".claude", "skills", skillName)

	if _, err := os.Stat(dest); errors.Is(err, fs.ErrNotExist) {
		return errors.New("the gralph-docs-writer skill is not installed.\nRun: gralph --install-skill")
	} else if err != nil {
		return err
	}

	hash, err := Hash()
	if err != nil {
		return err
	}

	installedBy := "unknown"
	if data, err := fs.ReadFile(os.DirFS(dest), "VERSION"); err == nil {
		lines := strings.Split(string(data), "\n")
		if lines[0] != "" {
			installedBy = lines[0]
		}
		if len(lines) > 1 && lines[1] == hash {
			return nil
		}
	}
	return fmt.Errorf("the gralph-docs-writer skill is outdated (installed by %s, this is gralph %s).\nRun: gralph --install-skill", installedBy, version.Version())
}
