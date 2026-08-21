package agent

import (
	"reflect"
	"strings"
	"testing"
)

func TestProfiles(t *testing.T) {
	t.Run("codex expands to a valid generic command", func(t *testing.T) {
		command, err := Profile("codex")
		if err != nil {
			t.Fatalf("Profile(\"codex\") error = %v", err)
		}

		want := AgentCommand{
			Executable: "codex",
			Args:       []string{"exec", promptPlaceholder},
			PromptMode: PromptModeArg,
		}
		if !reflect.DeepEqual(command, want) {
			t.Errorf("Profile(\"codex\") = %#v, want %#v", command, want)
		}
		if err := command.Validate(); err != nil {
			t.Errorf("profile command Validate() error = %v", err)
		}
	})

	t.Run("returned command can be customized without changing the profile", func(t *testing.T) {
		command, err := Profile("codex")
		if err != nil {
			t.Fatalf("Profile(\"codex\") error = %v", err)
		}
		command.Args[0] = "changed"

		freshCommand, err := Profile("codex")
		if err != nil {
			t.Fatalf("Profile(\"codex\") error = %v", err)
		}
		if got := freshCommand.Args[0]; got != "exec" {
			t.Errorf("fresh profile argument = %q, want %q", got, "exec")
		}
	})

	t.Run("unknown profile is rejected", func(t *testing.T) {
		_, err := Profile("not-a-profile")
		if err == nil {
			t.Fatal("Profile() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "unknown agent profile") {
			t.Errorf("Profile() error = %q, want unknown-profile error", err)
		}
	})

	if got, want := ProfileNames(), []string{"codex"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ProfileNames() = %q, want %q", got, want)
	}
}
