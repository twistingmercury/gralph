package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
