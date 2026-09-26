package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

type setupField struct {
	input textinput.Model
	// isTasks marks the tasks path field; otherwise it is the prompt path.
	isTasks bool
	err     string
}

// SetupModel asks for the missing tasks and prompt paths and validates each with
// looper.LoadTasks or looper.LoadPrompt.
type SetupModel struct {
	fields []setupField
	focus  int

	tasksPath, promptPath, prompt string
	taskList                      *tasks.TaskList
	cancelled                     bool
}

// NewSetup returns a setup screen with one field per empty path, tasks first.
func NewSetup(tasksPath, promptPath string) SetupModel {
	s := SetupModel{tasksPath: tasksPath, promptPath: promptPath}
	if tasksPath == "" {
		s.fields = append(s.fields, newSetupField("tasks file: ", true))
	}
	if promptPath == "" {
		s.fields = append(s.fields, newSetupField("prompt file: ", false))
	}
	if len(s.fields) > 0 {
		s.fields[0].input.Focus()
	}
	return s
}

// Setup runs the setup screen for the empty paths and returns its final
// state. opts are extra program options, for tests.
func Setup(tasksPath, promptPath string, opts ...tea.ProgramOption) (SetupModel, error) {
	final, err := tea.NewProgram(NewSetup(tasksPath, promptPath), opts...).Run()
	if err != nil {
		return SetupModel{}, err
	}
	return final.(SetupModel), nil
}

func newSetupField(prompt string, isTasks bool) setupField {
	in := textinput.New()
	in.Prompt = prompt
	return setupField{input: in, isTasks: isTasks}
}

func (s SetupModel) Init() tea.Cmd { return nil }

func (s SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Every field is done; messages can still arrive before Quit lands.
	if s.focus == len(s.fields) {
		return s, nil
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "esc", "ctrl+c":
			s.cancelled = true
			return s, tea.Quit
		case "enter":
			return s.submit()
		}
	}
	var cmd tea.Cmd
	s.fields[s.focus].input, cmd = s.fields[s.focus].input.Update(msg)
	return s, cmd
}

// submit validates the focused field and, if valid, moves on or quits.
func (s SetupModel) submit() (tea.Model, tea.Cmd) {
	f := &s.fields[s.focus]
	path := f.input.Value()
	var err error
	if f.isTasks {
		var tl *tasks.TaskList
		if tl, err = looper.LoadTasks(path); err == nil {
			s.tasksPath, s.taskList = path, tl
		}
	} else {
		var prompt string
		if prompt, err = looper.LoadPrompt(path); err == nil {
			s.promptPath, s.prompt = path, prompt
		}
	}
	if err != nil {
		f.err = err.Error()
		return s, nil
	}
	f.err = ""
	f.input.Blur()
	s.focus++
	if s.focus == len(s.fields) {
		return s, tea.Quit
	}
	return s, s.fields[s.focus].input.Focus()
}

func (s SetupModel) View() tea.View {
	var b strings.Builder
	for _, f := range s.fields {
		b.WriteString(f.input.View() + "\n")
		if f.err != "" {
			b.WriteString("  " + f.err + "\n")
		}
	}
	b.WriteString("\nenter confirm · esc quit")
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// TasksPath is the tasks file path, given or entered.
func (s SetupModel) TasksPath() string { return s.tasksPath }

// PromptPath is the prompt file path, given or entered.
func (s SetupModel) PromptPath() string { return s.promptPath }

// Prompt is the loaded prompt text when the prompt path was entered here.
func (s SetupModel) Prompt() string { return s.prompt }

// Tasks is the loaded task list when the tasks path was entered here.
func (s SetupModel) Tasks() *tasks.TaskList { return s.taskList }

// Cancelled reports whether setup was quit with Esc or ctrl+c.
func (s SetupModel) Cancelled() bool { return s.cancelled }
