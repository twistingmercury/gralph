package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandLine(t *testing.T) {
	cases := map[string]struct {
		s    Settings
		want string
	}{
		"folder and sandbox": {
			s:    Settings{Dir: ".local/foo", TasksPath: ".local/foo/tasks.yaml", PromptPath: ".local/foo/prompt.md", SandboxSettings: "/home/u/sb.json"},
			want: "gralph -d .local/foo --sandbox-settings /home/u/sb.json",
		},
		"everything": {
			s: Settings{Dir: ".local/foo", TasksPath: ".local/foo/tasks.yaml", PromptPath: ".local/foo/prompt.md",
				SkipPermissions: true, Commit: true, LogDir: ".local/foo/logs", GateTimeout: "5m"},
			want: "gralph -d .local/foo --skip-permissions --commit --log-dir .local/foo/logs --gate-timeout 5m",
		},
		"flag overrides one file": {
			s:    Settings{Dir: "f", TasksPath: "f/v2.yaml", PromptPath: "f/prompt.md", SkipPermissions: true},
			want: "gralph -d f -t f/v2.yaml --skip-permissions",
		},
		"no folder": {
			s:    Settings{TasksPath: "t.yaml", PromptPath: "p.md", SkipPermissions: true},
			want: "gralph -t t.yaml -p p.md --skip-permissions",
		},
		"spaces and quotes are quoted": {
			s:    Settings{Dir: "my runs/it's", TasksPath: "my runs/it's/tasks.yaml", PromptPath: "my runs/it's/prompt.md", SkipPermissions: true},
			want: `gralph -d 'my runs/it'\''s' --skip-permissions`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := CommandLine(tc.s)
			assert.Equal(t, tc.want, got)
		})
	}
}
