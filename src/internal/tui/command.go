package tui

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// Settings is what a run will use, however each value was given: a flag, the
// run folder, or the wizard. cmd/main runs its startup checks on it after the
// wizard closes, so nothing here is trusted as already checked.
type Settings struct {
	Dir             string
	TasksPath       string
	Tasks           *tasks.TaskList
	SandboxSettings string
	SkipPermissions bool
	Commit          bool
	LogDir          string
	GateTimeout     string
	// GatesEdited marks a gate list changed in the wizard, which cmd/main
	// saves to the task file on Start and only then.
	GatesEdited bool
}

// safeWord matches what zsh and bash read as one plain word, so only paths
// that need quoting get it. ~ and = are safe only past the first character;
// see shellQuote.
var safeWord = regexp.MustCompile(`^[A-Za-z0-9_./~:=@%+-]+$`)

// CommandLine is the command that starts the same run with no wizard, shown on
// the review screen so the user can copy it. Gates are not on it: they are in
// the task file.
func CommandLine(s Settings) string {
	parts := []string{"gralph"}
	parts = appendPaths(parts, s)
	switch {
	case s.SandboxSettings != "":
		quoted := shellQuote(s.SandboxSettings)
		parts = append(parts, "--sandbox-settings", quoted)
	case s.SkipPermissions:
		parts = append(parts, "--skip-permissions")
	}

	if s.Commit {
		parts = append(parts, "--commit")
	}

	if s.LogDir != "" {
		quoted := shellQuote(s.LogDir)
		parts = append(parts, "--log-dir", quoted)
	}

	if s.GateTimeout != "" {
		parts = append(parts, "--gate-timeout", s.GateTimeout)
	}

	return strings.Join(parts, " ")
}

// appendPaths writes -d when there is a folder, plus -t only when the task file
// is not the one the folder would supply by its fixed name.
func appendPaths(parts []string, s Settings) []string {
	if s.Dir == "" {
		tasksQuoted := shellQuote(s.TasksPath)
		return append(parts, "-t", tasksQuoted)
	}

	dirQuoted := shellQuote(s.Dir)
	parts = append(parts, "-d", dirQuoted)
	defaultTasks := filepath.Join(s.Dir, "tasks.yaml")
	if s.TasksPath != defaultTasks {
		quoted := shellQuote(s.TasksPath)
		parts = append(parts, "-t", quoted)
	}

	return parts
}

// A leading ~ is a home or named-directory lookup and a leading = is zsh's
// EQUALS expansion, so a word starting with either is quoted.
func shellQuote(s string) string {
	leadsExpansion := strings.HasPrefix(s, "~") || strings.HasPrefix(s, "=")
	if safeWord.MatchString(s) && !leadsExpansion {
		return s
	}

	escaped := strings.ReplaceAll(s, "'", `'\''`)
	return "'" + escaped + "'"
}
