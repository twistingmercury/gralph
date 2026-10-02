package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

func TestNeedsWizard(t *testing.T) {
	gates := []tasks.Gate{}
	decided := &tasks.TaskList{Gates: &gates}
	undecided := &tasks.TaskList{}
	full := Settings{TasksPath: "t.yaml", PromptPath: "p.md", Tasks: decided, SkipPermissions: true}

	cases := map[string]struct {
		s    Settings
		want bool
	}{
		"everything answered":      {full, false},
		"no folder or files":       {Settings{SkipPermissions: true}, true},
		"one file missing":         {Settings{TasksPath: "t.yaml", Tasks: decided, SkipPermissions: true}, true},
		"no permission choice":     {Settings{TasksPath: "t.yaml", PromptPath: "p.md", Tasks: decided}, true},
		"gates key absent":         {Settings{TasksPath: "t.yaml", PromptPath: "p.md", Tasks: undecided, SkipPermissions: true}, true},
		"sandbox counts as chosen": {Settings{TasksPath: "t.yaml", PromptPath: "p.md", Tasks: decided, SandboxSettings: "sb.json"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NeedsWizard(tc.s)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStepVisibility(t *testing.T) {
	s := Settings{Dir: "f", TasksPath: "f/tasks.yaml", PromptPath: "f/prompt.md", SkipPermissions: true}
	w := newWizard(s, Given{Commit: true})
	assert.True(t, w.hideFolder(), "both files are known")
	assert.True(t, w.hidePermissions(), "--skip-permissions was passed")
	assert.True(t, w.hideCommit(), "--commit was passed")
	assert.False(t, w.hideLogging(), "a folder and no --log-dir")
	assert.False(t, w.hideTimeout())

	w = newWizard(Settings{TasksPath: "t.yaml", PromptPath: "p.md"}, Given{})
	assert.True(t, w.hideLogging(), "no folder, so no <folder>/logs to offer")
	assert.False(t, w.hidePermissions(), "no permission flag")
	assert.False(t, w.hideCommit())

	w = newWizard(Settings{Dir: "f", PromptPath: "f/prompt.md"}, Given{LogDir: true, GateTimeout: true})
	assert.False(t, w.hideFolder(), "the task file is still missing")
	assert.True(t, w.hideLogging(), "--log-dir was passed")
	assert.True(t, w.hideTimeout(), "--gate-timeout was passed")
}

func TestStepVisibility_FollowsAnswers(t *testing.T) {
	w := newWizard(Settings{}, Given{})
	assert.True(t, w.hideSandbox(), "no permission chosen yet")
	assert.True(t, w.hideCustomTimeout(), "no timeout chosen yet")

	w.perm = permSkip
	assert.True(t, w.hideSandbox())
	w.perm = permSandbox
	assert.False(t, w.hideSandbox())

	w.timeoutChoice = "default"
	assert.True(t, w.hideCustomTimeout())
	w.timeoutChoice = timeoutCustom
	assert.False(t, w.hideCustomTimeout())
}

func TestStepOpen(t *testing.T) {
	gates := []tasks.Gate{}
	decided := &tasks.TaskList{Gates: &gates}

	assert.True(t, folderOpen(Settings{TasksPath: "t.yaml"}))
	assert.False(t, folderOpen(Settings{TasksPath: "t.yaml", PromptPath: "p.md"}))
	assert.True(t, permissionsOpen(Settings{}))
	assert.False(t, permissionsOpen(Settings{SandboxSettings: "sb.json"}))
	assert.True(t, gatesOpen(Settings{}), "no task file loaded yet")
	assert.True(t, gatesOpen(Settings{Tasks: &tasks.TaskList{}}), "gates key absent")
	assert.False(t, gatesOpen(Settings{Tasks: decided}), "gates: [] is a decision")
}

type wizardResult struct {
	s   Settings
	err error
}

// permissionsOnly is a run whose files and optional steps are all given, so
// the wizard opens on the permissions step alone.
func permissionsOnly(t *testing.T) Settings {
	t.Helper()
	tasksPath := writeFile(t, "tasks.yaml", "gates: []\n"+validTasks)
	promptPath := writeFile(t, "prompt.md", "do the work")
	gates := []tasks.Gate{}
	tl := &tasks.TaskList{Gates: &gates, Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "do it"}}}
	return Settings{TasksPath: tasksPath, PromptPath: promptPath, Prompt: "do the work", Tasks: tl}
}

var allGiven = Given{Commit: true, LogDir: true, GateTimeout: true}

// startWizard runs one wizard screen headless and returns the input pipe's
// writer and a channel with its result. Only one form per test reads the
// pipe: a finished program's input reader lingers and swallows the next keys.
func startWizard(t *testing.T, run func(opts ...tea.ProgramOption) (Settings, error)) (*io.PipeWriter, <-chan wizardResult) {
	t.Helper()
	in, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	done := make(chan wizardResult, 1)
	go func() {
		got, err := run(tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24))
		done <- wizardResult{got, err}
	}()
	return w, done
}

// press sends keys to a form the way a program would, running the commands a
// key returns so the form moves between fields and groups.
func press(t *testing.T, form *huh.Form, keys ...tea.KeyPressMsg) {
	t.Helper()
	for _, k := range keys {
		_, cmd := form.Update(k)
		if cmd == nil {
			continue
		}

		_, cmd = form.Update(cmd())
		if cmd != nil {
			_, _ = form.Update(cmd())
		}
	}
}

var (
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
)

func waitWizard(t *testing.T, done <-chan wizardResult) wizardResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Wizard did not return")
		return wizardResult{}
	}
}

func TestWizard_CancelReturnsErrCancelled(t *testing.T) {
	s := permissionsOnly(t)
	before, err := os.ReadFile(s.TasksPath)
	require.NoError(t, err)
	wizard := func(opts ...tea.ProgramOption) (Settings, error) {
		return Wizard(context.Background(), s, allGiven, opts...)
	}
	w, done := startWizard(t, wizard)

	_, err = io.WriteString(w, "\x1b")
	require.NoError(t, err)

	r := waitWizard(t, done)
	assert.True(t, errors.Is(r.err, ErrCancelled), "got %v", r.err)
	after, err := os.ReadFile(s.TasksPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a cancel changes nothing on disk")
}

func TestWizard_CancelledContextReturnsErrCancelled(t *testing.T) {
	s := permissionsOnly(t)
	before, err := os.ReadFile(s.TasksPath)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wizard := func(opts ...tea.ProgramOption) (Settings, error) { return Wizard(ctx, s, allGiven, opts...) }
	_, done := startWizard(t, wizard)

	r := waitWizard(t, done)
	assert.True(t, errors.Is(r.err, ErrCancelled), "got %v", r.err)
	after, err := os.ReadFile(s.TasksPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a signal changes nothing on disk")
}

func TestRelativeTo(t *testing.T) {
	cases := map[string]struct{ base, path, want string }{
		"inside":          {"/work", "/work/runs/a", "runs/a"},
		"the base itself": {"/work", "/work", "."},
		"a sibling":       {"/work/a", "/work/b", "../b"},
		"no base":         {"", "/work/a", "/work/a"},
		"relative base":   {"work", "/work/a", "/work/a"},
		"relative path":   {"/work", "runs/a", "runs/a"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := relativeTo(tc.base, tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWizardKeyMap_HelpMatchesKeys(t *testing.T) {
	km := wizardKeyMap()
	assert.Equal(t, "esc", km.FilePicker.Close.Help().Key)
	assert.Equal(t, "quit setup", km.FilePicker.Close.Help().Desc)
	assert.Equal(t, "→", km.FilePicker.Open.Help().Key)
	assert.Equal(t, "open", km.FilePicker.Open.Help().Desc)
	assert.Equal(t, []string{"ctrl+c", "esc"}, km.Quit.Keys(), "Esc and ctrl+c still quit")
}

func TestWizard_PermissionsPlaceholderChoosesNothing(t *testing.T) {
	w := newWizard(permissionsOnly(t), allGiven)
	form := w.stepsForm()
	form.Init()
	// Init queues this move past the hidden folder group; a program would run
	// it, this driver does not.
	form.NextGroup()

	press(t, form, keyEnter)
	assert.Equal(t, huh.StateNormal, form.State, "Enter on the placeholder must not move on")
	assert.Empty(t, w.perm)

	press(t, form, keyDown, keyDown, keyEnter)
	require.Equal(t, huh.StateCompleted, form.State)
	w.fold()
	assert.True(t, w.s.SkipPermissions)
	assert.Empty(t, w.s.SandboxSettings)
}

func TestWizard_StepsOpen(t *testing.T) {
	assert.True(t, newWizard(permissionsOnly(t), allGiven).stepsOpen())

	s := permissionsOnly(t)
	s.SkipPermissions = true
	assert.False(t, newWizard(s, allGiven).stepsOpen(), "every step answered by a flag")
}

func TestWizard_Fold(t *testing.T) {
	w := newWizard(Settings{SandboxSettings: "old.json"}, Given{})
	w.perm, w.sandbox = permSandbox, "sb.json"
	w.commit = true
	w.timeoutChoice, w.customTimeout = timeoutCustom, "90s"
	w.fold()
	assert.Equal(t, "sb.json", w.s.SandboxSettings)
	assert.False(t, w.s.SkipPermissions)
	assert.True(t, w.s.Commit)
	assert.Equal(t, "90s", w.s.GateTimeout)

	w = newWizard(Settings{Commit: true, GateTimeout: "5m"}, Given{Commit: true, GateTimeout: true})
	w.fold()
	assert.True(t, w.s.Commit, "a flag's answer stays when its step was hidden")
	assert.Equal(t, "5m", w.s.GateTimeout)
}

func reviewOf(s Settings) func(opts ...tea.ProgramOption) (Settings, error) {
	return func(opts ...tea.ProgramOption) (Settings, error) {
		w := newWizard(s, allGiven)
		return w.review(context.Background(), opts)
	}
}

func TestWizard_ReviewStartReturnsSettings(t *testing.T) {
	s := permissionsOnly(t)
	s.SkipPermissions = true
	w, done := startWizard(t, reviewOf(s))

	// Start is the first option.
	_, err := io.WriteString(w, "\r")
	require.NoError(t, err)

	r := waitWizard(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, s, r.s)
}

func TestWizard_ReviewCancel(t *testing.T) {
	cases := map[string]string{
		"the Cancel option": "jj\r",
		"esc":               "\x1b",
	}
	for name, keys := range cases {
		t.Run(name, func(t *testing.T) {
			s := permissionsOnly(t)
			s.SkipPermissions = true
			w, done := startWizard(t, reviewOf(s))

			_, err := io.WriteString(w, keys)
			require.NoError(t, err)

			r := waitWizard(t, done)
			assert.True(t, errors.Is(r.err, ErrCancelled), "got %v", r.err)
		})
	}
}

func TestReviewText(t *testing.T) {
	gates := []tasks.Gate{{Cmd: "make test", Timeout: "5m"}}
	s := Settings{
		Dir: "my_run", TasksPath: "my_run/tasks.yaml", PromptPath: "my_run/prompt.md",
		Tasks: &tasks.TaskList{Gates: &gates}, SkipPermissions: true, GateTimeout: "90s",
	}
	text := reviewText(s)
	assert.Contains(t, text, "Folder: my_run")
	assert.Contains(t, text, "Permissions: skip permissions, no sandbox")
	assert.Contains(t, text, "Commit: no")
	assert.Contains(t, text, "Logging: no")
	assert.Contains(t, text, "Gate limit: 90s")
	assert.Contains(t, text, "  make test (5m)")
	assert.Contains(t, text, "gralph -d my_run --skip-permissions --gate-timeout 90s")

	s.Tasks = &tasks.TaskList{}
	text = reviewText(s)
	assert.Contains(t, text, "Gates: no gates")
}

func TestReviewFiles(t *testing.T) {
	s := Settings{Dir: "run", TasksPath: "run/tasks.yaml", PromptPath: "run/prompt.md"}
	assert.Equal(t, []string{"Folder: run"}, reviewFiles(s))

	s.TasksPath, s.PromptPath = "other/t.yaml", "other/p.md"
	want := []string{"Folder: run", "Tasks: other/t.yaml", "Prompt: other/p.md"}
	assert.Equal(t, want, reviewFiles(s), "an override must not hide behind the folder")

	s.TasksPath = "run/tasks.yaml"
	assert.Equal(t, []string{"Folder: run", "Prompt: other/p.md"}, reviewFiles(s))

	s = Settings{TasksPath: "t.yaml", PromptPath: "p.md"}
	assert.Equal(t, []string{"Tasks: t.yaml", "Prompt: p.md"}, reviewFiles(s))
}

func TestNoteEscape(t *testing.T) {
	escaped := noteEscape(`gralph -d 'it'\''s_a*run' x` + "`")
	assert.Equal(t, `gralph -d 'it'\\''s\_a\*run' x\`+"`", escaped)
}
