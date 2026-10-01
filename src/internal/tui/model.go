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

const stopPrompt = "Stop the run? The current task stays pending. [y/N]"

// interruptedMsg reports an outside SIGINT/SIGTERM.
type interruptedMsg struct{}

var (
	border  = lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	focused = border.BorderForeground(lipgloss.Color("12"))
	// Exact colors, not palette slots, so neither the terminal theme nor
	// bold-as-bright rendering can wash out the title's contrast.
	title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#1E40AF"))
)

var icons = map[string]string{
	tasks.CompletedState: "✅",
	tasks.FailedState:    "❌",
	inProgressState:      "▶ ",
}

var rowStyles = map[string]lipgloss.Style{
	tasks.CompletedState: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
	tasks.FailedState:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),
	inProgressState:      lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("3")),
	tasks.PendingState:   lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
}

// Inline keeps the banner to one line even when a task error spans several.
var (
	successBanner = lipgloss.NewStyle().Inline(true).Bold(true).Foreground(lipgloss.Color("10"))
	failureBanner = lipgloss.NewStyle().Inline(true).Bold(true).Foreground(lipgloss.Color("9"))
)

// Model is the run view: prompt, tasks, and output panes over a key legend.
type Model struct {
	tasks  []tasks.Task
	output []string

	prompt, taskPane, outPane, legend viewport.Model
	// focus indexes panes(): prompt, tasks, output.
	focus int

	cancel     func()
	confirming bool
	// stopBy is "user" or "signal" once a stop is under way.
	stopBy string
	// failed is the status line for the last TaskFinished, if it failed.
	failed   string
	done     bool
	status   string
	exitCode int
}

// New returns a run view over a copy of tl's tasks; cancel stops the run.
func New(tl *tasks.TaskList, cancel func()) Model {
	m := Model{tasks: append([]tasks.Task(nil), tl.Tasks...), focus: 2, cancel: cancel}
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
	case interruptedMsg:
		if m.done {
			return m, tea.Quit
		}

		m.confirming = false
		m.stopBy = "signal"
		m.legend.SetContent(legend)
	case tea.KeyPressMsg:
		k := msg.String()
		if m.confirming {
			m.confirming = false
			m.legend.SetContent(legend)
			if k == "y" {
				m.cancel()
				m.stopBy = "user"
			}

			return m, nil
		}

		if k == "q" || k == "ctrl+c" {
			if m.done {
				return m, tea.Quit
			}

			if m.stopBy == "" {
				m.confirming = true
				m.legend.SetContent(stopPrompt)
			}

			return m, nil
		}

		vp := m.panes()[m.focus]
		switch k {
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
			m.failed = ""
			if msg.Task.State == tasks.FailedState {
				m.failed = fmt.Sprintf("Task %d failed: %s", msg.Task.ID, msg.Task.Error)
			}
		case looper.RunDone:
			m.confirming = false
			if m.stopBy != "" {
				for _, t := range m.tasks {
					if t.State == inProgressState {
						m.setState(t.ID, tasks.PendingState)
					}
				}
				m.exitCode = 1
				return m, tea.Quit
			}

			m.done = true
			switch {
			case msg.Err == nil:
				m.status = "All tasks completed"
			case m.failed != "":
				m.status = m.failed
			default:
				m.status = "Run stopped: " + msg.Err.Error()
			}
			if msg.Err != nil {
				m.exitCode = 1
			}

			m.legend.SetContent(m.status + " · " + legend)
			m.renderTasks()
		}
	}
	return m, nil
}

// ExitCode is the exit code for the run: 0 only when it finished with no error.
func (m Model) ExitCode() int { return m.exitCode }

// Summary is the one-line outcome of the run.
func (m Model) Summary() string {
	if m.stopBy != "" {
		return "Run stopped by " + m.stopBy
	}

	return m.status
}

func (m Model) View() tea.View {
	for i, vp := range m.panes() {
		vp.Style = border
		if i == m.focus {
			vp.Style = focused
		}
	}
	left := lipgloss.JoinVertical(lipgloss.Left, pane("Current task", m.prompt), pane("Task progress", m.taskPane))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, pane("Claude activity", m.outPane))
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, top, m.legend.View()))
	v.AltScreen = true
	return v
}

// pane renders the title bar above the viewport rather than as its content so
// the title stays put while the pane scrolls.
func pane(name string, vp viewport.Model) string {
	return lipgloss.JoinVertical(lipgloss.Left, title.Width(vp.Width()).Render(name), vp.View())
}

// layout sizes the panes to fill a width x height window, following
// docs/tui_mock_up.txt; each pane gives up one line to its title bar.
func (m *Model) layout(width, height int) {
	legendH := 3
	topH := max(height-legendH, 0)
	leftW := width * 3 / 5
	promptH := topH / 2

	m.prompt.SetWidth(leftW)
	m.prompt.SetHeight(max(promptH-1, 0))
	m.taskPane.SetWidth(leftW)
	m.taskPane.SetHeight(max(topH-promptH-1, 0))
	m.outPane.SetWidth(width - leftW)
	m.outPane.SetHeight(max(topH-1, 0))
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
	var rows []string
	if m.done {
		rows = append(rows, m.banner())
	}

	for _, t := range m.tasks {
		icon, ok := icons[t.State]
		if !ok {
			icon = "  "
		}

		text := fmt.Sprintf("%d: %s: %s", t.ID, t.Name, t.State)
		rows = append(rows, icon+" "+rowStyles[t.State].Render(text))
	}
	m.taskPane.SetContent(strings.Join(rows, "\n"))
}

func (m *Model) banner() string {
	text := m.status + " · press q to exit"
	if m.exitCode != 0 {
		return failureBanner.Render("✘ " + text)
	}

	return successBanner.Render("✔ " + text)
}
