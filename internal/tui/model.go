// Package tui is the terminal UI for a gralph run.
package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

// inProgressState is display only; it is never written to tasks.yaml.
const inProgressState = "in progress"

const legend = "tab switch pane · ↑/↓/PgUp/PgDn scroll · q quit"

var (
	border  = lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	focused = border.BorderForeground(lipgloss.Color("12"))
)

var icons = map[string]string{
	tasks.CompletedState: "✅",
	tasks.FailedState:    "❌",
	inProgressState:      "▶ ",
}

// Model is the run view: prompt, tasks, and output panes over a key legend.
type Model struct {
	tasks  []tasks.Task
	output []string

	prompt, taskPane, outPane, legend viewport.Model
	// focus indexes panes(): prompt, tasks, output.
	focus int
}

// New returns a run view over a copy of tl's tasks.
func New(tl *tasks.TaskList) Model {
	m := Model{tasks: append([]tasks.Task(nil), tl.Tasks...), focus: 2}
	for _, vp := range []*viewport.Model{&m.prompt, &m.taskPane, &m.outPane, &m.legend} {
		*vp = viewport.New()
		vp.Style = border
		vp.SoftWrap = true
	}
	m.legend.SoftWrap = false
	m.legend.SetContent(legend)
	m.renderTasks()
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		vp := m.panes()[m.focus]
		switch msg.String() {
		case "tab":
			m.focus = (m.focus + 1) % len(m.panes())
		case "up":
			vp.ScrollUp(1)
		case "down":
			vp.ScrollDown(1)
		case "pgup":
			vp.PageUp()
		case "pgdown":
			vp.PageDown()
		}
	case looper.Event:
		switch msg.Kind {
		case looper.TaskStarted:
			m.setState(msg.Task.ID, inProgressState)
			m.prompt.SetContent(msg.Task.String())
			m.prompt.GotoTop()
			m.output = []string{fmt.Sprintf("task %d: %s", msg.Task.ID, msg.Task.Name)}
			m.outPane.SetContentLines(m.output)
			m.outPane.GotoTop()
		case looper.Activity:
			follow := m.outPane.AtBottom()
			m.output = append(m.output, msg.Line)
			m.outPane.SetContentLines(m.output)
			if follow {
				m.outPane.GotoBottom()
			}
		case looper.TaskFinished:
			m.setState(msg.Task.ID, msg.Task.State)
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	for i, vp := range m.panes() {
		vp.Style = border
		if i == m.focus {
			vp.Style = focused
		}
	}
	left := lipgloss.JoinVertical(lipgloss.Left, m.prompt.View(), m.taskPane.View())
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, m.outPane.View())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, top, m.legend.View()))
	v.AltScreen = true
	return v
}

// layout sizes the panes to fill a width x height window, following
// docs/tui_mock_up.txt.
func (m *Model) layout(width, height int) {
	legendH := 3
	topH := max(height-legendH, 0)
	leftW := width * 3 / 5
	promptH := topH / 2

	m.prompt.SetWidth(leftW)
	m.prompt.SetHeight(promptH)
	m.taskPane.SetWidth(leftW)
	m.taskPane.SetHeight(topH - promptH)
	m.outPane.SetWidth(width - leftW)
	m.outPane.SetHeight(topH)
	m.legend.SetWidth(width)
	m.legend.SetHeight(legendH)
}

func (m *Model) panes() []*viewport.Model {
	return []*viewport.Model{&m.prompt, &m.taskPane, &m.outPane}
}

func (m *Model) setState(id int16, state string) {
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks[i].State = state
		}
	}
	m.renderTasks()
}

func (m *Model) renderTasks() {
	rows := []string{"Tasks"}
	for _, t := range m.tasks {
		icon, ok := icons[t.State]
		if !ok {
			icon = "  "
		}
		rows = append(rows, fmt.Sprintf("%s %s: %s", icon, t.Name, t.State))
	}
	m.taskPane.SetContent(strings.Join(rows, "\n"))
}
