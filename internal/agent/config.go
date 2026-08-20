// Package agent defines provider-neutral configuration for invoking an AI agent.
package agent

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
