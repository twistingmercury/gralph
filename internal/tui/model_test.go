package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

func testTasks() *tasks.TaskList {
	return &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "do the first thing", State: tasks.PendingState},
		{ID: 2, Name: "Second", Prompt: "do the second thing", State: tasks.PendingState},
	}}
}

func update(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func render(m Model) string {
	return m.View().Content
}

func TestNew_CopiesTasks(t *testing.T) {
	tl := testTasks()
	m := New(tl)
	m = update(t, m, looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]})

	assert.Equal(t, tasks.PendingState, tl.Tasks[0].State)
	assert.Equal(t, inProgressState, m.tasks[0].State)
}

func TestUpdate_StatusesFollowEvents(t *testing.T) {
	tl := testTasks()
	m := update(t, New(tl), tea.WindowSizeMsg{Width: 120, Height: 30})
	assert.Contains(t, render(m), "   First: pending")
	assert.Contains(t, render(m), "   Second: pending")

	m = update(t, m, looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]})
	assert.Contains(t, render(m), "▶  First: in progress")

	done := tl.Tasks[0]
	done.State = tasks.CompletedState
	m = update(t, m, looper.Event{Kind: looper.TaskFinished, Task: done})
	assert.Contains(t, render(m), "✅ First: completed")

	m = update(t, m, looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[1]})
	failed := tl.Tasks[1]
	failed.State = tasks.FailedState
	m = update(t, m, looper.Event{Kind: looper.TaskFinished, Task: failed})
	assert.Contains(t, render(m), "❌ Second: failed")
	assert.Contains(t, render(m), "✅ First: completed")
}

func TestUpdate_TaskStartedReplacesPromptAndClearsOutput(t *testing.T) {
	tl := testTasks()
	m := update(t, New(tl), tea.WindowSizeMsg{Width: 120, Height: 30},
		looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]},
		looper.Event{Kind: looper.Activity, Line: "first activity"},
	)
	out := render(m)
	assert.Contains(t, out, "1: First")
	assert.Contains(t, out, "do the first thing")
	assert.Contains(t, out, "task 1: First")
	assert.Contains(t, out, "first activity")

	m = update(t, m, looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[1]})
	out = render(m)
	assert.Contains(t, out, "2: Second")
	assert.Contains(t, out, "do the second thing")
	assert.Contains(t, out, "task 2: Second")
	assert.NotContains(t, out, "do the first thing")
	assert.NotContains(t, out, "task 1: First")
	assert.NotContains(t, out, "first activity")
}

func TestUpdate_ActivityLinesAppear(t *testing.T) {
	tl := testTasks()
	m := update(t, New(tl), tea.WindowSizeMsg{Width: 120, Height: 30},
		looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]},
		looper.Event{Kind: looper.Activity, Line: "reading files"},
		looper.Event{Kind: looper.Activity, Line: "writing code"},
	)
	out := render(m)
	assert.Contains(t, out, "reading files")
	assert.Contains(t, out, "writing code")
}

func TestView_FillsWindow(t *testing.T) {
	tl := testTasks()
	long := strings.Repeat("a very long activity line ", 20)
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 30}, {Width: 60, Height: 15}} {
		m := update(t, New(tl), size,
			looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]},
			looper.Event{Kind: looper.Activity, Line: long},
		)
		v := m.View()
		assert.True(t, v.AltScreen)

		lines := strings.Split(v.Content, "\n")
		require.Len(t, lines, size.Height, "size %dx%d", size.Width, size.Height)
		for i, line := range lines {
			assert.Equal(t, size.Width, lipgloss.Width(line), "size %dx%d line %d", size.Width, size.Height, i)
		}
	}
}

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func activity(n int) []tea.Msg {
	msgs := make([]tea.Msg, n)
	for i := range msgs {
		msgs[i] = looper.Event{Kind: looper.Activity, Line: fmt.Sprintf("line %d", i)}
	}
	return msgs
}

func TestUpdate_TabCyclesFocus(t *testing.T) {
	m := New(testTasks())
	assert.Equal(t, 2, m.focus, "output")
	for _, want := range []int{0, 1, 2, 0} {
		m = update(t, m, key(tea.KeyTab))
		assert.Equal(t, want, m.focus)
	}
}

func TestView_FocusedPaneHighlighted(t *testing.T) {
	m := update(t, New(testTasks()), tea.WindowSizeMsg{Width: 60, Height: 15})
	blue := "\x1b[94m"
	views := map[string]bool{}
	for range 3 {
		out := render(m)
		assert.Equal(t, 1, strings.Count(out, blue+"┌"), "one highlighted pane")
		views[out] = true
		m = update(t, m, key(tea.KeyTab))
	}
	assert.Len(t, views, 3, "each focus highlights a different pane")
}

func TestUpdate_ScrollMovesOnlyFocusedPane(t *testing.T) {
	tl := &tasks.TaskList{}
	for i := range int16(20) {
		tl.Tasks = append(tl.Tasks, tasks.Task{ID: i + 1, Name: fmt.Sprintf("T%d", i+1), Prompt: strings.Repeat("step\n", 20)})
	}
	m := update(t, New(tl), tea.WindowSizeMsg{Width: 60, Height: 15},
		looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]})
	m = update(t, m, activity(30)...)
	outY := m.outPane.YOffset()
	require.Positive(t, outY, "output follows to the bottom")

	m = update(t, m, key(tea.KeyTab), key(tea.KeyDown))
	assert.Equal(t, 1, m.prompt.YOffset())
	m = update(t, m, key(tea.KeyPgDown))
	assert.Greater(t, m.prompt.YOffset(), 1)
	assert.Zero(t, m.taskPane.YOffset())
	assert.Equal(t, outY, m.outPane.YOffset())

	promptY := m.prompt.YOffset()
	m = update(t, m, key(tea.KeyTab), key(tea.KeyDown), key(tea.KeyPgDown))
	assert.Greater(t, m.taskPane.YOffset(), 1)
	assert.Equal(t, promptY, m.prompt.YOffset())
	assert.Equal(t, outY, m.outPane.YOffset())

	taskY := m.taskPane.YOffset()
	m = update(t, m, key(tea.KeyTab), key(tea.KeyPgUp), key(tea.KeyUp))
	assert.Less(t, m.outPane.YOffset(), outY-1)
	scrolled := m.outPane.YOffset()
	m = update(t, m, key(tea.KeyDown))
	assert.Equal(t, scrolled+1, m.outPane.YOffset())
	assert.Equal(t, promptY, m.prompt.YOffset())
	assert.Equal(t, taskY, m.taskPane.YOffset())
}

func TestUpdate_OutputFollowsUntilScrolledUp(t *testing.T) {
	tl := testTasks()
	m := update(t, New(tl), tea.WindowSizeMsg{Width: 60, Height: 15},
		looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[0]})
	m = update(t, m, activity(30)...)
	assert.True(t, m.outPane.AtBottom())
	assert.Contains(t, render(m), "line 29")

	m = update(t, m, key(tea.KeyUp))
	y := m.outPane.YOffset()
	m = update(t, m, activity(5)...)
	assert.Equal(t, y, m.outPane.YOffset(), "scrolled up: new lines do not move it")
	assert.False(t, m.outPane.AtBottom())

	m = update(t, m, key(tea.KeyPgDown), key(tea.KeyPgDown))
	require.True(t, m.outPane.AtBottom())
	m = update(t, m, looper.Event{Kind: looper.Activity, Line: "back at bottom"})
	assert.True(t, m.outPane.AtBottom())
	assert.Contains(t, render(m), "back at bottom")

	m = update(t, m, key(tea.KeyUp), looper.Event{Kind: looper.TaskStarted, Task: tl.Tasks[1]})
	m = update(t, m, activity(30)...)
	assert.True(t, m.outPane.AtBottom(), "TaskStarted resets to following")
	assert.Contains(t, render(m), "line 29")
}
