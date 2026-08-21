package agent

import (
	"fmt"
	"sort"
	"strings"
)

// profiles contains verified, noninteractive agent invocation contracts. Profile
// arguments never include permission-bypass options; callers must opt into any
// provider-specific security-sensitive arguments explicitly.
var profiles = map[string]AgentCommand{
	"claude-code": {
		Executable: "claude",
		Args:       []string{"--print", promptPlaceholder},
		PromptMode: PromptModeArg,
	},
	"codex": {
		Executable: "codex",
		Args:       []string{"exec", promptPlaceholder},
		PromptMode: PromptModeArg,
	},
}

// Profile returns a copy of the command associated with name. The returned
// command is an ordinary AgentCommand and may be validated or customized by the
// caller before execution.
func Profile(name string) (AgentCommand, error) {
	command, ok := profiles[strings.TrimSpace(name)]
	if !ok {
		return AgentCommand{}, fmt.Errorf("unknown agent profile %q", name)
	}

	command.Args = append([]string(nil), command.Args...)
	if err := command.Validate(); err != nil {
		return AgentCommand{}, fmt.Errorf("invalid agent profile %q: %w", name, err)
	}
	return command, nil
}

// ProfileNames returns the registered profile names in deterministic order.
func ProfileNames() []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
