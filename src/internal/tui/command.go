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
	PromptPath      string
	Prompt          string
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
// that need quoting get it.
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

// appendPaths writes -d when there is a folder, plus -t or -p only for a file
// the folder would not supply by its fixed name.
func appendPaths(parts []string, s Settings) []string {
	if s.Dir == "" {
		tasksQuoted, promptQuoted := shellQuote(s.TasksPath), shellQuote(s.PromptPath)
		return append(parts, "-t", tasksQuoted, "-p", promptQuoted)
	}

	dirQuoted := shellQuote(s.Dir)
	parts = append(parts, "-d", dirQuoted)
	defaultTasks := filepath.Join(s.Dir, "tasks.yaml")
	if s.TasksPath != defaultTasks {
		quoted := shellQuote(s.TasksPath)
		parts = append(parts, "-t", quoted)
	}

	defaultPrompt := filepath.Join(s.Dir, "prompt.md")
	if s.PromptPath != defaultPrompt {
		quoted := shellQuote(s.PromptPath)
		parts = append(parts, "-p", quoted)
	}

	return parts
}

func shellQuote(s string) string {
	if safeWord.MatchString(s) {
		return s
	}

	escaped := strings.ReplaceAll(s, "'", `'\''`)
	return "'" + escaped + "'"
}
