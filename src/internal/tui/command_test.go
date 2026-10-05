package tui

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandLine(t *testing.T) {
	cases := map[string]struct {
		s    Settings
		want string
	}{
		"folder and sandbox": {
			s:    Settings{Dir: ".local/foo", TasksPath: ".local/foo/tasks.yaml", SandboxSettings: "/home/u/sb.json"},
			want: "gralph -d .local/foo --sandbox-settings /home/u/sb.json",
		},
		"everything": {
			s: Settings{Dir: ".local/foo", TasksPath: ".local/foo/tasks.yaml",
				SkipPermissions: true, Commit: true, LogDir: ".local/foo/logs", GateTimeout: "5m"},
			want: "gralph -d .local/foo --skip-permissions --commit --log-dir .local/foo/logs --gate-timeout 5m",
		},
		"sandbox file implied by the folder": {
			s:    Settings{Dir: "f", TasksPath: "f/tasks.yaml", SandboxSettings: "f/sandbox.json"},
			want: "gralph -d f",
		},
		"a different sandbox path stays": {
			s:    Settings{Dir: "f", TasksPath: "f/tasks.yaml", SandboxSettings: "other/sandbox.json"},
			want: "gralph -d f --sandbox-settings other/sandbox.json",
		},
		"sandbox file with no folder stays": {
			s:    Settings{TasksPath: "t.yaml", SandboxSettings: "sandbox.json"},
			want: "gralph -t t.yaml --sandbox-settings sandbox.json",
		},
		"flag overrides the file": {
			s:    Settings{Dir: "f", TasksPath: "f/v2.yaml", SkipPermissions: true},
			want: "gralph -d f -t f/v2.yaml --skip-permissions",
		},
		"no folder": {
			s:    Settings{TasksPath: "t.yaml", SkipPermissions: true},
			want: "gralph -t t.yaml --skip-permissions",
		},
		"spaces and quotes are quoted": {
			s:    Settings{Dir: "my runs/it's", TasksPath: "my runs/it's/tasks.yaml", SkipPermissions: true},
			want: `gralph -d 'my runs/it'\''s' --skip-permissions`,
		},
		"leading tilde is quoted": {
			s:    Settings{Dir: "~old", TasksPath: "~old/tasks.yaml", SkipPermissions: true},
			want: "gralph -d '~old' --skip-permissions",
		},
		"leading equals is quoted": {
			s:    Settings{Dir: "=x", TasksPath: "=x/tasks.yaml", SkipPermissions: true},
			want: "gralph -d '=x' --skip-permissions",
		},
		"tilde and equals inside a word stay bare": {
			s:    Settings{Dir: "a~b=c", TasksPath: "a~b=c/tasks.yaml", SkipPermissions: true},
			want: "gralph -d a~b=c --skip-permissions",
		},
		"space is quoted": {
			s:    Settings{Dir: "my runs", TasksPath: "my runs/tasks.yaml", SkipPermissions: true},
			want: "gralph -d 'my runs' --skip-permissions",
		},
		"single quote is quoted": {
			s:    Settings{Dir: "it's", TasksPath: "it's/tasks.yaml", SkipPermissions: true},
			want: `gralph -d 'it'\''s' --skip-permissions`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := CommandLine(tc.s)
			assert.Equal(t, tc.want, got)
		})
	}
}

// zsh is the shell whose expansions the quoting must beat, so the test asks
// it rather than trusting a reading of its rules.
func TestShellQuote_ZshReadsItBack(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not on PATH")
	}

	words := []string{"=x", "~old", "~", "a~b=c", "my runs/it's", "plain/dir", "=", "a b", "$HOME", "x;y"}
	for _, word := range words {
		t.Run(word, func(t *testing.T) {
			quoted := shellQuote(word)
			script := "print -r -- " + quoted
			out, err := exec.Command(zsh, "-fc", script).Output()
			require.NoError(t, err)
			assert.Equal(t, word+"\n", string(out))
		})
	}
}
