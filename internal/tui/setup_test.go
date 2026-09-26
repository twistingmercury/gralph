package tui

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// enterPath types path into the focused field and presses Enter.
func enterPath(t *testing.T, s SetupModel, path string) (SetupModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, r := range path {
		next, _ := s.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		s = next.(SetupModel)
	}
	next, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return next.(SetupModel), cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

const validTasks = "tasks:\n  - id: 1\n    name: First\n    prompt: do it\n"

func TestSetup_RejectsInvalidPaths(t *testing.T) {
	cases := map[string]struct {
		tasks   bool
		path    string
		wantErr string
	}{
		"missing tasks file": {tasks: true, path: filepath.Join(t.TempDir(), "nope.yaml"), wantErr: "is not accessible"},
		"invalid yaml":       {tasks: true, path: writeFile(t, "tasks.yaml", "tasks: [\n"), wantErr: "failed to parse tasks yaml"},
		"failed task": {tasks: true, path: writeFile(t, "tasks.yaml",
			"tasks:\n  - id: 1\n    name: First\n    prompt: do it\n    state: failed\n"), wantErr: "fix the failed tasks"},
		"whitespace prompt": {path: writeFile(t, "prompt.md", " \n\t\n"), wantErr: "just whitespace"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := NewSetup("given.yaml", "")
			if tc.tasks {
				s = NewSetup("", "given.md")
			}
			s, cmd := enterPath(t, s, tc.path)

			assert.False(t, isQuit(cmd))
			assert.Equal(t, 0, s.focus)
			assert.Contains(t, s.View().Content, tc.wantErr)
		})
	}
}

func TestSetup_AcceptsValidPathsAndQuits(t *testing.T) {
	tasksPath := writeFile(t, "tasks.yaml", validTasks)
	promptPath := writeFile(t, "prompt.md", "  the prompt\n")
	s := NewSetup("", "")
	require.Len(t, s.fields, 2)
	assert.True(t, s.View().AltScreen)

	s, _ = enterPath(t, s, tasksPath)
	assert.Equal(t, 1, s.focus)

	s, cmd := enterPath(t, s, promptPath)
	assert.True(t, isQuit(cmd))
	assert.False(t, s.Cancelled())
	assert.Equal(t, tasksPath, s.TasksPath())
	assert.Equal(t, promptPath, s.PromptPath())
	assert.Equal(t, "the prompt", s.Prompt())
	require.NotNil(t, s.Tasks())
	assert.Equal(t, "First", s.Tasks().Tasks[0].Name)
}

func TestSetup_OnlyMissingPathsGetAField(t *testing.T) {
	s := NewSetup("", "prompt.md")
	require.Len(t, s.fields, 1)
	assert.True(t, s.fields[0].isTasks)
	assert.Equal(t, "prompt.md", s.PromptPath())

	s = NewSetup("tasks.yaml", "")
	require.Len(t, s.fields, 1)
	assert.False(t, s.fields[0].isTasks)
	assert.Equal(t, "tasks.yaml", s.TasksPath())
}

func TestSetup_EscCancels(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'c', Mod: tea.ModCtrl}} {
		next, cmd := NewSetup("", "").Update(k)
		assert.True(t, isQuit(cmd))
		assert.True(t, next.(SetupModel).Cancelled())
	}
}

// startSetup runs Setup headless for an empty tasks path and returns the input
// pipe's writer and a channel with Setup's result.
func startSetup(t *testing.T) (*io.PipeWriter, <-chan SetupModel) {
	t.Helper()
	in, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	done := make(chan SetupModel, 1)
	go func() {
		s, err := Setup("", "prompt.md", tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24))
		assert.NoError(t, err)
		done <- s
	}()
	return w, done
}

func waitSetup(t *testing.T, done <-chan SetupModel) SetupModel {
	t.Helper()
	select {
	case s := <-done:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("Setup did not return")
		return SetupModel{}
	}
}

func TestSetupProgram_ReturnsEnteredTasks(t *testing.T) {
	tasksPath := writeFile(t, "tasks.yaml", validTasks)
	w, done := startSetup(t)

	_, err := io.WriteString(w, tasksPath+"\r")
	require.NoError(t, err)

	s := waitSetup(t, done)
	assert.False(t, s.Cancelled())
	assert.Equal(t, tasksPath, s.TasksPath())
	assert.Equal(t, "prompt.md", s.PromptPath())
	require.NotNil(t, s.Tasks())
	assert.Equal(t, "First", s.Tasks().Tasks[0].Name)
}

func TestSetupProgram_EscCancels(t *testing.T) {
	w, done := startSetup(t)

	_, err := io.WriteString(w, "\x1b")
	require.NoError(t, err)

	assert.True(t, waitSetup(t, done).Cancelled())
}
