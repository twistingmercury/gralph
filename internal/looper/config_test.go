package looper

import (
	"errors"
	"io"
	"testing"

	"github.com/twistingmercury/gralph/internal/agent"
)

func TestStart_ConfigValidation(t *testing.T) {
	validConfig := Config{
		PromptPath:   "prompt.md",
		PRDPath:      "PRD.md",
		MaxAttempts:  1,
		OutputWriter: io.Discard,
		AgentCommand: agent.AgentCommand{
			Executable: "agent",
			PromptMode: agent.PromptModeStdin,
		},
	}

	tests := []struct {
		name            string
		config          Config
		wantErr         bool
		wantAgentConfig bool
	}{
		{name: "valid", config: validConfig},
		{name: "blank prompt path", config: func() Config { c := validConfig; c.PromptPath = " "; return c }(), wantErr: true},
		{name: "blank PRD path", config: func() Config { c := validConfig; c.PRDPath = " "; return c }(), wantErr: true},
		{name: "invalid maximum attempts", config: func() Config { c := validConfig; c.MaxAttempts = 0; return c }(), wantErr: true},
		{name: "nil output writer", config: func() Config { c := validConfig; c.OutputWriter = nil; return c }(), wantErr: true},
		{name: "invalid agent command", config: func() Config { c := validConfig; c.AgentCommand.Executable = ""; return c }(), wantErr: true, wantAgentConfig: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if tt.wantAgentConfig && !errors.Is(err, agent.ErrInvalidConfiguration) {
				t.Fatalf("Validate() error = %v, want agent configuration category", err)
			}
		})
	}
}
