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
		return m.handleInterrupt()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case looper.Event:
		return m.handleEvent(msg)
	}
	return m, nil
}

func (m Model) handleInterrupt() (tea.Model, tea.Cmd) {
	if m.done {
		return m, tea.Quit
	}

	m.confirming = false
	m.stopBy = "signal"
	m.legend.SetContent(legend)
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.confirming {
		return m.answerStopPrompt(k)
	}

	if k == "q" || k == "ctrl+c" {
		return m.handleQuitKey()
	}

	m.navigate(k)
	return m, nil
}

// answerStopPrompt closes the stop confirmation; only "y" stops the run.
func (m Model) answerStopPrompt(k string) (tea.Model, tea.Cmd) {
	m.confirming = false
	m.legend.SetContent(legend)
	if k == "y" {
		m.cancel()
		m.stopBy = "user"
	}

	return m, nil
}

// handleQuitKey quits a finished run at once; a running one is asked about
// first, unless a stop is already under way.
func (m Model) handleQuitKey() (tea.Model, tea.Cmd) {
	if m.done {
		return m, tea.Quit
	}

	if m.stopBy == "" {
		m.confirming = true
		m.legend.SetContent(stopPrompt)
	}

	return m, nil
}

func (m *Model) navigate(k string) {
	panes := m.panes()
	vp := panes[m.focus]
	switch k {
	case "tab":
		m.focus = (m.focus + 1) % len(panes)
	case "up":
		vp.ScrollUp(1)
	case "down":
		vp.ScrollDown(1)
	case "pgup":
		vp.PageUp()
	case "pgdown":
		vp.PageDown()
	}
}

func (m Model) handleEvent(ev looper.Event) (tea.Model, tea.Cmd) {
	switch ev.Kind {
	case looper.TaskStarted:
		m.taskStarted(ev)
	case looper.Activity:
		m.activity(ev)
	case looper.TaskFinished:
		m.taskFinished(ev)
	case looper.RunDone:
		return m.runDone(ev)
	}
	return m, nil
}

func (m *Model) taskStarted(ev looper.Event) {
	m.setState(ev.Task.ID, inProgressState)
	taskText := ev.Task.String()
	m.prompt.SetContent(taskText)
	m.prompt.GotoTop()
	m.output = []string{fmt.Sprintf("task %d: %s", ev.Task.ID, ev.Task.Name)}
	m.outPane.SetContentLines(m.output)
	m.outPane.GotoTop()
}

func (m *Model) activity(ev looper.Event) {
	follow := m.outPane.AtBottom()
	m.output = append(m.output, ev.Line)
	m.outPane.SetContentLines(m.output)
	if follow {
		m.outPane.GotoBottom()
	}
}

func (m *Model) taskFinished(ev looper.Event) {
	m.setState(ev.Task.ID, ev.Task.State)
	m.failed = ""
	if ev.Task.State == tasks.FailedState {
		m.failed = fmt.Sprintf("Task %d failed: %s", ev.Task.ID, ev.Task.Error)
	}
}

func (m Model) runDone(ev looper.Event) (tea.Model, tea.Cmd) {
	m.confirming = false
	if m.stopBy != "" {
		m.resetInProgress()
		m.exitCode = 1
		return m, tea.Quit
	}

	m.done = true
	m.status = m.doneStatus(ev.Err)
	if ev.Err != nil {
		m.exitCode = 1
	}

	m.legend.SetContent(m.status + " · " + legend)
	m.renderTasks()
	return m, nil
}

// resetInProgress puts a stopped run's task back to pending on screen, which
// is what its state in the file still is.
func (m *Model) resetInProgress() {
	for _, t := range m.tasks {
		if t.State == inProgressState {
			m.setState(t.ID, tasks.PendingState)
		}
	}
}

// doneStatus prefers the failed task's own message over the run error.
func (m *Model) doneStatus(err error) string {
	if err == nil {
		return "All tasks completed"
	}

	if m.failed != "" {
		return m.failed
	}

	return "Run stopped: " + err.Error()
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
	currentTask := pane("Current task", m.prompt)
	taskProgress := pane("Task progress", m.taskPane)
	left := lipgloss.JoinVertical(lipgloss.Left, currentTask, taskProgress)
	activity := pane("Claude activity", m.outPane)
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, activity)
	legendBar := m.legend.View()
	screen := lipgloss.JoinVertical(lipgloss.Left, top, legendBar)
	v := tea.NewView(screen)
	v.AltScreen = true
	return v
}

// pane renders the title bar above the viewport rather than as its content so
// the title stays put while the pane scrolls.
func pane(name string, vp viewport.Model) string {
	width := vp.Width()
	titleBar := title.Width(width).Render(name)
	body := vp.View()
	return lipgloss.JoinVertical(lipgloss.Left, titleBar, body)
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
		banner := m.banner()
		rows = append(rows, banner)
	}

	for _, t := range m.tasks {
		icon, ok := icons[t.State]
		if !ok {
			icon = "  "
		}

		text := fmt.Sprintf("%d: %s: %s", t.ID, t.Name, t.State)
		rows = append(rows, icon+" "+rowStyles[t.State].Render(text))
	}
	content := strings.Join(rows, "\n")
	m.taskPane.SetContent(content)
}

func (m *Model) banner() string {
	text := m.status + " · press q to exit"
	if m.exitCode != 0 {
		return failureBanner.Render("✘ " + text)
	}

	return successBanner.Render("✔ " + text)
}
