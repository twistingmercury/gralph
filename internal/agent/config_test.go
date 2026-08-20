package agent

import (
	"reflect"
	"testing"
)

func TestAgentCommand(t *testing.T) {
	wantArgs := []string{"run", "--mode", "noninteractive"}
	command := AgentCommand{
		Executable: "example-agent",
		Args:       wantArgs,
		PromptMode: PromptModeStdin,
	}

	if command.Executable != "example-agent" {
		t.Errorf("Executable = %q, want %q", command.Executable, "example-agent")
	}
	if !reflect.DeepEqual(command.Args, wantArgs) {
		t.Errorf("Args = %q, want %q", command.Args, wantArgs)
	}
	if command.PromptMode != PromptModeStdin {
		t.Errorf("PromptMode = %q, want %q", command.PromptMode, PromptModeStdin)
	}
}

func TestPromptModes(t *testing.T) {
	tests := []struct {
		name string
		mode PromptMode
		want string
	}{
		{name: "standard input", mode: PromptModeStdin, want: "stdin"},
		{name: "argument", mode: PromptModeArg, want: "arg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.mode); got != tt.want {
				t.Errorf("PromptMode = %q, want %q", got, tt.want)
			}
		})
	}
}
