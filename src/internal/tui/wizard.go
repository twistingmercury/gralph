package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/twistingmercury/gralph/internal/tasks"
)

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
	customTimeout string
	logging       bool
	folder        string
	sandbox       string
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

const (
	reviewStart     = "Start"
	reviewEditGates = "Edit gates"
	reviewCancel    = "Cancel"
)

// Wizard asks for what s leaves open, then shows the review screen. It writes
// nothing: cmd/main saves an edited gate list after Start, so a cancel at any
// point leaves the task file as it was.
func Wizard(s Settings, g Given, opts ...tea.ProgramOption) (Settings, error) {
	w := newWizard(s, g)
	if w.stepsOpen() {
		form := w.stepsForm()
		if err := runWizardForm(form, opts); err != nil {
			return Settings{}, err
		}
	}

	w.fold()
	if gatesOpen(w.s) {
		if err := w.editGates(opts); err != nil {
			return Settings{}, err
		}
	}

	return w.review(opts)
}

// stepsOpen leaves out the sandbox file and the custom limit, which open only
// from an answer given on another step.
func (w *wizard) stepsOpen() bool {
	hides := []func() bool{w.hideFolder, w.hidePermissions, w.hideCommit, w.hideLogging, w.hideTimeout}
	return slices.ContainsFunc(hides, isShown)
}

func isShown(hide func() bool) bool {
	return !hide()
}

func (w *wizard) stepsForm() *huh.Form {
	folderField, permissionField, sandboxField := w.folderField(), w.permissionField(), w.sandboxField()
	commitField, loggingField := w.commitField(), w.loggingField()
	timeoutField, customField := w.timeoutField(), w.customTimeoutField()
	return huh.NewForm(
		huh.NewGroup(folderField).WithHideFunc(w.hideFolder),
		huh.NewGroup(permissionField).WithHideFunc(w.hidePermissions),
		huh.NewGroup(sandboxField).WithHideFunc(w.hideSandbox),
		huh.NewGroup(commitField).WithHideFunc(w.hideCommit),
		huh.NewGroup(loggingField).WithHideFunc(w.hideLogging),
		huh.NewGroup(timeoutField).WithHideFunc(w.hideTimeout),
		huh.NewGroup(customField).WithHideFunc(w.hideCustomTimeout),
	)
}

// folderField writes to its own variable because the picker stores a choice
// before checkFolder sees it; only checkFolder moves a folder into w.s.
func (w *wizard) folderField() huh.Field {
	return huh.NewFilePicker().
		Title("Run folder").
		Description("enter picks the highlighted folder · → opens it · ← goes up").
		DirAllowed(true).
		FileAllowed(false).
		ShowHidden(true).
		CurrentDirectory(".").
		Picking(true).
		Validate(w.checkFolder).
		Value(&w.folder)
}

// permissionField starts on a placeholder that checkPermission refuses, so
// Enter on a step the user never moved through chooses nothing (ADR-014).
func (w *wizard) permissionField() huh.Field {
	return huh.NewSelect[string]().
		Title("How should sessions run?").
		Options(
			huh.NewOption("Choose one", ""),
			huh.NewOption("In Claude's sandbox, with a settings file", permSandbox),
			huh.NewOption("Skip permissions: no sandbox, no checks", permSkip),
		).
		Validate(checkPermission).
		Value(&w.perm)
}

// sandboxField writes to its own variable so a picked file does not count as
// the permission flag, which would hide the permissions step mid-form.
func (w *wizard) sandboxField() huh.Field {
	return huh.NewFilePicker().
		Title("Sandbox settings file").
		AllowedTypes([]string{".json"}).
		FileAllowed(true).
		DirAllowed(false).
		ShowHidden(true).
		CurrentDirectory(".").
		Picking(true).
		Validate(checkSandboxFile).
		Value(&w.sandbox)
}

func (w *wizard) commitField() huh.Field {
	return huh.NewConfirm().
		Title("Commit each completed task with git?").
		Affirmative("Yes").
		Negative("No").
		Validate(w.checkCommit).
		Value(&w.commit)
}

func (w *wizard) loggingField() huh.Field {
	return huh.NewConfirm().
		TitleFunc(w.loggingTitle, &w.s.Dir).
		Affirmative("Yes").
		Negative("No").
		Validate(w.checkLogging).
		Value(&w.logging)
}

func (w *wizard) loggingTitle() string {
	logDir := filepath.Join(w.s.Dir, "logs")
	return fmt.Sprintf("Keep a record of the run in %s?", logDir)
}

func (w *wizard) timeoutField() huh.Field {
	return huh.NewSelect[string]().
		Title("Gate time limit").
		Options(
			huh.NewOption("Default (each gate's own timeout, else 10m)", ""),
			huh.NewOption("One limit for every gate", timeoutCustom),
		).
		Value(&w.timeoutChoice)
}

func (w *wizard) customTimeoutField() huh.Field {
	return huh.NewInput().
		Title("Limit, such as 90s or 10m").
		Validate(checkCustomTimeout).
		Value(&w.customTimeout)
}

// fold moves the answers kept outside w.s into it. The folder and the log
// folder are already there, put in by their checks.
func (w *wizard) fold() {
	switch w.perm {
	case permSkip:
		w.s.SkipPermissions, w.s.SandboxSettings = true, ""
	case permSandbox:
		w.s.SkipPermissions, w.s.SandboxSettings = false, w.sandbox
	}

	if !w.g.Commit {
		w.s.Commit = w.commit
	}

	if w.timeoutChoice == timeoutCustom {
		w.s.GateTimeout = w.customTimeout
	}
}

// editGates replaces the task list with an edited copy, leaving the caller's
// untouched. An absent gates key counts as a change even when no gate was
// added, so Done saves gates: [] and the file is not asked about again.
func (w *wizard) editGates(opts []tea.ProgramOption) error {
	before := w.s.Tasks.GateList()
	after, err := editGates(before, opts...)
	if err != nil {
		return err
	}

	if !gatesOpen(w.s) && slices.Equal(before, after) {
		return nil
	}

	tl := *w.s.Tasks
	tl.Gates = &after
	w.s.Tasks = &tl
	w.s.GatesEdited = true
	return nil
}

func (w *wizard) review(opts []tea.ProgramOption) (Settings, error) {
	for {
		choice, err := w.askReview(opts)
		if err != nil {
			return Settings{}, err
		}

		if choice == reviewStart {
			return w.s, nil
		}

		if choice != reviewEditGates {
			return Settings{}, ErrCancelled
		}

		if err := w.editGates(opts); err != nil {
			return Settings{}, err
		}
	}
}

func (w *wizard) askReview(opts []tea.ProgramOption) (string, error) {
	text := reviewText(w.s)
	escaped := noteEscape(text)
	note := huh.NewNote().Title("Ready to run").Description(escaped)
	choice := reviewStart
	actions := huh.NewOptions(reviewStart, reviewEditGates, reviewCancel)
	sel := huh.NewSelect[string]().Options(actions...).Value(&choice)
	err := runGateForm(opts, note, sel)
	return choice, err
}

func reviewText(s Settings) string {
	lines := reviewFiles(s)
	permissions := permissionLabel(s)
	commit := yesNo(s.Commit)
	logging := orElse(s.LogDir, "no")
	limit := orElse(s.GateTimeout, "default")
	lines = append(lines,
		"Permissions: "+permissions,
		"Commit: "+commit,
		"Logging: "+logging,
		"Gate limit: "+limit,
	)
	gates := s.Tasks.GateList()
	gateList := gateLines(gates)
	lines = append(lines, gateList...)
	cmd := CommandLine(s)
	lines = append(lines, "", "Same run, no wizard:", cmd)
	return strings.Join(lines, "\n")
}

func reviewFiles(s Settings) []string {
	if s.Dir != "" {
		return []string{"Folder: " + s.Dir}
	}

	return []string{"Tasks: " + s.TasksPath, "Prompt: " + s.PromptPath}
}

func permissionLabel(s Settings) string {
	if s.SkipPermissions {
		return "skip permissions, no sandbox"
	}

	return "sandbox, settings in " + s.SandboxSettings
}

func gateLines(gates []tasks.Gate) []string {
	if len(gates) == 0 {
		return []string{"Gates: no gates"}
	}

	lines := []string{"Gates:"}
	for _, g := range gates {
		label := gateLabel(g)
		lines = append(lines, "  "+label)
	}

	return lines
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}

	return s
}

// noteMarkup escapes what a huh note reads as markup, so a path with an
// underscore and the quoted command line show as typed and paste back into a
// shell unchanged.
var noteMarkup = strings.NewReplacer(`\`, `\\`, "_", `\_`, "*", `\*`, "`", "\\`")

func noteEscape(s string) string {
	return noteMarkup.Replace(s)
}

// runWizardForm makes Esc quit like ctrl+c, as in the gate editor, and turns
// huh's abort into the wizard's cancel.
func runWizardForm(form *huh.Form, opts []tea.ProgramOption) error {
	km := huh.NewDefaultKeyMap()
	km.Quit.SetKeys("ctrl+c", "esc")
	err := form.WithKeyMap(km).WithProgramOptions(opts...).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}

	return err
}
