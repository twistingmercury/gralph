package agent

import (
	"reflect"
	"strings"
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

func TestAgentCommandValidate(t *testing.T) {
	tests := []struct {
		name    string
		command AgentCommand
		wantErr string
	}{
		{
			name: "stdin mode without arguments",
			command: AgentCommand{
				Executable: "agent",
				PromptMode: PromptModeStdin,
			},
		},
		{
			name: "stdin mode preserves literal arguments",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{"argument with spaces", "$(not-executed)", "; rm -rf nowhere"},
				PromptMode: PromptModeStdin,
			},
		},
		{
			name: "arg mode with one whole placeholder",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{"run", "argument with spaces & metacharacters", promptPlaceholder},
				PromptMode: PromptModeArg,
			},
		},
		{
			name: "blank executable",
			command: AgentCommand{
				Executable: " \t\n",
				PromptMode: PromptModeStdin,
			},
			wantErr: "executable must not be blank",
		},
		{
			name: "unsupported prompt mode",
			command: AgentCommand{
				Executable: "agent",
				PromptMode: PromptMode("environment"),
			},
			wantErr: "unsupported prompt mode",
		},
		{
			name: "stdin mode with placeholder",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{promptPlaceholder},
				PromptMode: PromptModeStdin,
			},
			wantErr: "placeholder is not allowed",
		},
		{
			name: "stdin mode with embedded placeholder",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{"--prompt=" + promptPlaceholder},
				PromptMode: PromptModeStdin,
			},
			wantErr: "placeholder is not allowed",
		},
		{
			name: "arg mode without placeholder",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{"run"},
				PromptMode: PromptModeArg,
			},
			wantErr: "exactly one",
		},
		{
			name: "arg mode with duplicate placeholders",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{promptPlaceholder, promptPlaceholder},
				PromptMode: PromptModeArg,
			},
			wantErr: "exactly one",
		},
		{
			name: "arg mode with embedded placeholder",
			command: AgentCommand{
				Executable: "agent",
				Args:       []string{"--prompt=" + promptPlaceholder},
				PromptMode: PromptModeArg,
			},
			wantErr: "whole argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.command.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
