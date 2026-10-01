// Package skillinstall installs the embedded gralph-docs-writer skill for Claude Code.
package skillinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
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

	err = fs.WalkDir(skills.FS, skillName, copyTo(dest))
	if err != nil {
		return "", err
	}

	hash, err := Hash()
	if err != nil {
		return "", err
	}

	stamp := version.Version() + "\n" + hash + "\n"
	versionPath := filepath.Join(dest, "VERSION")
	if err := os.WriteFile(versionPath, []byte(stamp), 0o600); err != nil {
		return "", err
	}

	return dest, nil
}

// copyTo returns a WalkDirFunc that copies each embedded skill entry under
// dest; it is a closure because fs.WalkDirFunc has no parameter for dest.
func copyTo(dest string) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
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
	}
}

// Hash returns a SHA-256 content hash of the embedded skill: each file's
// slash-separated relative path, then its bytes, in fs.WalkDir order.
func Hash() (string, error) {
	h := sha256.New()
	if err := fs.WalkDir(skills.FS, skillName, hashInto(h)); err != nil {
		return "", err
	}

	sum := h.Sum(nil)
	return "sha256:" + hex.EncodeToString(sum), nil
}

// hashInto returns a WalkDirFunc that writes each embedded skill file's
// relative path and bytes to h; it is a closure because fs.WalkDirFunc has no
// parameter for h.
func hashInto(h hash.Hash) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}

		rel := strings.TrimPrefix(path, skillName+"/")
		h.Write([]byte(rel))
		h.Write(data)
		return nil
	}
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
	installed := os.DirFS(dest)
	if data, err := fs.ReadFile(installed, "VERSION"); err == nil {
		lines := strings.Split(string(data), "\n")
		if lines[0] != "" {
			installedBy = lines[0]
		}

		if len(lines) > 1 && lines[1] == hash {
			return nil
		}
	}

	current := version.Version()
	return fmt.Errorf("the gralph-docs-writer skill is outdated (installed by %s, this is gralph %s).\nRun: gralph --install-skill", installedBy, current)
}
