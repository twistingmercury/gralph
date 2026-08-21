package agent

import (
	"reflect"
	"strings"
	"testing"
)

func TestProfiles(t *testing.T) {
	profiles := map[string]AgentCommand{
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
	for name, want := range profiles {
		t.Run(name+" expands to a valid generic command", func(t *testing.T) {
			command, err := Profile(name)
			if err != nil {
				t.Fatalf("Profile(%q) error = %v", name, err)
			}
			if !reflect.DeepEqual(command, want) {
				t.Errorf("Profile(%q) = %#v, want %#v", name, command, want)
			}
			if err := command.Validate(); err != nil {
				t.Errorf("profile command Validate() error = %v", err)
			}
		})
	}

	for name, want := range profiles {
		t.Run(name+" returns an independent command copy", func(t *testing.T) {
			command, err := Profile(name)
			if err != nil {
				t.Fatalf("Profile(%q) error = %v", name, err)
			}
			command.Args[0] = "changed"

			freshCommand, err := Profile(name)
			if err != nil {
				t.Fatalf("Profile(%q) error = %v", name, err)
			}
			if got, want := freshCommand.Args[0], want.Args[0]; got != want {
				t.Errorf("fresh profile argument = %q, want %q", got, want)
			}
		})
	}

	t.Run("unknown profile is rejected", func(t *testing.T) {
		_, err := Profile("not-a-profile")
		if err == nil {
			t.Fatal("Profile() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "unknown agent profile") {
			t.Errorf("Profile() error = %q, want unknown-profile error", err)
		}
	})

	if got, want := ProfileNames(), []string{"claude-code", "codex"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ProfileNames() = %q, want %q", got, want)
	}
}
