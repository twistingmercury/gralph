package tui

import "errors"

// ErrCancelled is returned when the user quits the wizard; nothing has been
// written.
var ErrCancelled = errors.New("setup cancelled")

// Given marks the optional steps a flag already answered. They cannot be read
// from Settings, because "no" and "not asked" look the same there.
type Given struct {
	Commit, LogDir, GateTimeout bool
}

// NeedsWizard reports whether any gap is open: the files, the permission
// choice, or an undecided gate list. The optional steps never open the wizard
// by themselves, or every run without --commit would stop to ask.
func NeedsWizard(s Settings) bool {
	return folderOpen(s) || permissionsOpen(s) || gatesOpen(s)
}

func folderOpen(s Settings) bool {
	return s.TasksPath == "" || s.PromptPath == ""
}

// permissionsOpen has no default to fall back on: a run needs one of the two
// flags or the wizard's answer (ADR-014).
func permissionsOpen(s Settings) bool {
	return s.SandboxSettings == "" && !s.SkipPermissions
}

// gatesOpen treats gates: [] as a decision and only an absent key as a gap, so
// a file that chose no gates is never asked again.
func gatesOpen(s Settings) bool {
	return s.Tasks == nil || s.Tasks.Gates == nil
}

// wizard holds the answers as the steps fill them in. The hide funcs are
// methods so a form can take them as method values and see each answer as
// soon as it is given.
type wizard struct {
	s             Settings
	g             Given
	perm          string
	timeoutChoice string
	commit        bool
}

func newWizard(s Settings, g Given) *wizard {
	return &wizard{s: s, g: g}
}

func (w *wizard) hideFolder() bool {
	return !folderOpen(w.s)
}

func (w *wizard) hidePermissions() bool {
	return !permissionsOpen(w.s)
}

func (w *wizard) hideSandbox() bool {
	return w.perm != permSandbox
}

func (w *wizard) hideCommit() bool {
	return w.g.Commit
}

// hideLogging also hides the step without a folder, because the only log
// directory it offers is <folder>/logs.
func (w *wizard) hideLogging() bool {
	return w.g.LogDir || w.s.Dir == ""
}

func (w *wizard) hideTimeout() bool {
	return w.g.GateTimeout
}

func (w *wizard) hideCustomTimeout() bool {
	return w.timeoutChoice != timeoutCustom
}
