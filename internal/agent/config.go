// Package agent defines provider-neutral configuration for invoking an AI agent.
package agent

import (
	"fmt"
	"strings"
)

const promptPlaceholder = "{prompt}"

// PromptMode identifies how an agent receives its prompt.
type PromptMode string

const (
	// PromptModeStdin sends the prompt to the agent through standard input.
	PromptModeStdin PromptMode = "stdin"
	// PromptModeArg sends the prompt to the agent as a command-line argument.
	PromptModeArg PromptMode = "arg"
)

// AgentCommand describes how to invoke an AI agent without interpreting its
// arguments through a shell.
type AgentCommand struct {
	Executable string
	Args       []string
	PromptMode PromptMode
}

// Validate checks that the command has an executable and an unambiguous prompt
// transport configuration.
func (c AgentCommand) Validate() error {
	if strings.TrimSpace(c.Executable) == "" {
		return fmt.Errorf("agent executable must not be blank")
	}

	switch c.PromptMode {
	case PromptModeStdin:
		for _, arg := range c.Args {
			if strings.Contains(arg, promptPlaceholder) {
				return fmt.Errorf("prompt placeholder is not allowed in stdin mode")
			}
		}
	case PromptModeArg:
		placeholderCount := 0
		for _, arg := range c.Args {
			switch {
			case arg == promptPlaceholder:
				placeholderCount++
			case strings.Contains(arg, promptPlaceholder):
				return fmt.Errorf("prompt placeholder must be a whole argument")
			}
		}
		if placeholderCount != 1 {
			return fmt.Errorf("arg mode requires exactly one %q argument", promptPlaceholder)
		}
	default:
		return fmt.Errorf("unsupported prompt mode %q", c.PromptMode)
	}

	return nil
}
