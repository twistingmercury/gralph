package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// The gate picker's non-gate choices sit below every gate index.
const (
	gateAdd  = -1
	gateDone = -2
)

const (
	gateEdit   = "Edit"
	gateDelete = "Delete"
	gateBack   = "Back"
)

// The list operations copy so the file's list is untouched until Start; the
// caller compares the result with the original to know whether to save.
func addGate(gates []tasks.Gate, g tasks.Gate) []tasks.Gate {
	list := slices.Clone(gates)
	return append(list, g)
}

func replaceGate(gates []tasks.Gate, i int, g tasks.Gate) []tasks.Gate {
	list := slices.Clone(gates)
	list[i] = g
	return list
}

func deleteGate(gates []tasks.Gate, i int) []tasks.Gate {
	list := slices.Clone(gates)
	return slices.Delete(list, i, i+1)
}

func checkGateCmd(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("a gate needs a command")
	}

	return nil
}

// checkGateTimeoutField allows a blank timeout because a gate without one
// falls back to --gate-timeout or the default.
func checkGateTimeoutField(s string) error {
	if s == "" {
		return nil
	}

	_, err := tasks.ParseTimeout(s)
	return err
}

// editGates edits a copy of gates and returns it on Done. It starts from a
// non-nil list so Done on an empty one saves gates: [], a decision, rather
// than leaving the key absent.
func editGates(gates []tasks.Gate, opts ...tea.ProgramOption) ([]tasks.Gate, error) {
	list := append([]tasks.Gate{}, gates...)
	for {
		choice, err := pickGate(list, opts)
		if err != nil {
			return nil, err
		}

		if choice == gateDone {
			return list, nil
		}

		list, err = applyGateChoice(list, choice, opts)
		if err != nil {
			return nil, err
		}
	}
}

func applyGateChoice(list []tasks.Gate, choice int, opts []tea.ProgramOption) ([]tasks.Gate, error) {
	if choice == gateAdd {
		g, err := askGate(tasks.Gate{}, opts)
		if err != nil {
			return nil, err
		}

		return addGate(list, g), nil
	}

	return changeGate(list, choice, opts)
}

func pickGate(list []tasks.Gate, opts []tea.ProgramOption) (int, error) {
	options := make([]huh.Option[int], 0, len(list)+2)
	for i, g := range list {
		label := gateLabel(g)
		opt := huh.NewOption(label, i)
		options = append(options, opt)
	}

	add := huh.NewOption("Add a gate", gateAdd)
	done := huh.NewOption("Done", gateDone)
	options = append(options, add, done)

	description := ""
	if len(list) == 0 {
		description = "No gates yet"
	}

	choice := 0
	sel := huh.NewSelect[int]().
		Title("Gates (run after every completed task, in this order)").
		Description(description).
		Options(options...).
		Value(&choice)
	err := runGateForm(opts, sel)
	return choice, err
}

// gateLabel keeps a multi-line command to one row of the picker.
func gateLabel(g tasks.Gate) string {
	cmd, _, _ := strings.Cut(g.Cmd, "\n")
	timeout := g.Timeout
	if timeout == "" {
		timeout = "default"
	}

	return fmt.Sprintf("%s (%s)", cmd, timeout)
}

func changeGate(list []tasks.Gate, i int, opts []tea.ProgramOption) ([]tasks.Gate, error) {
	action := gateBack
	actions := huh.NewOptions(gateEdit, gateDelete, gateBack)
	sel := huh.NewSelect[string]().
		Title(list[i].Cmd).
		Options(actions...).
		Value(&action)
	err := runGateForm(opts, sel)
	if err != nil {
		return nil, err
	}

	switch action {
	case gateEdit:
		return editOneGate(list, i, opts)
	case gateDelete:
		return deleteGate(list, i), nil
	default:
		return list, nil
	}
}

func editOneGate(list []tasks.Gate, i int, opts []tea.ProgramOption) ([]tasks.Gate, error) {
	g, err := askGate(list[i], opts)
	if err != nil {
		return nil, err
	}

	return replaceGate(list, i, g), nil
}

func askGate(g tasks.Gate, opts []tea.ProgramOption) (tasks.Gate, error) {
	cmd := huh.NewInput().Title("Command").Validate(checkGateCmd).Value(&g.Cmd)
	timeout := huh.NewInput().
		Title("Timeout (optional, such as 90s or 10m)").
		Validate(checkGateTimeoutField).
		Value(&g.Timeout)
	err := runGateForm(opts, cmd, timeout)
	return g, err
}

// runGateForm makes Esc quit like ctrl+c, as everywhere else in the wizard,
// and turns huh's abort into the wizard's cancel.
func runGateForm(opts []tea.ProgramOption, fields ...huh.Field) error {
	km := huh.NewDefaultKeyMap()
	km.Quit.SetKeys("ctrl+c", "esc")
	group := huh.NewGroup(fields...)
	err := huh.NewForm(group).WithKeyMap(km).WithProgramOptions(opts...).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}

	return err
}
