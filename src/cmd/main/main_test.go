package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/gralph/internal/looper"
)

const validTasksYAML = "tasks:\n  - id: 1\n    name: first\n    prompt: do it\n"

func TestSessionArgs(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0o600))
	sandboxed := []string{"--permission-mode", "acceptEdits", "--settings", `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`}

	tests := []struct {
		name    string
		file    string
		skip    bool
		dryRun  bool
		want    []string
		wantErr string
	}{
		{name: "sandbox file", file: settings, want: sandboxed},
		{name: "skip", skip: true, want: []string{"--dangerously-skip-permissions"}},
		{name: "both", file: settings, skip: true, wantErr: "--sandbox-settings and --skip-permissions cannot be used together"},
		{name: "neither", wantErr: "pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox"},
		{name: "dry run, neither", dryRun: true},
		{name: "dry run, both", file: settings, skip: true, dryRun: true, wantErr: "--sandbox-settings and --skip-permissions cannot be used together"},
		{name: "dry run, sandbox file", file: settings, dryRun: true, want: sandboxed},
		{name: "bad file", file: filepath.Join(t.TempDir(), "missing.json"), wantErr: "--sandbox-settings: file"},
		{name: "dry run, bad file", file: filepath.Join(t.TempDir(), "missing.json"), dryRun: true, wantErr: "--sandbox-settings: file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sessionArgs(tt.file, tt.skip, tt.dryRun)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadGiven_BothEmpty(t *testing.T) {
	prompt, tasklist, err := loadGiven("", "")

	require.NoError(t, err)
	assert.Empty(t, prompt)
	assert.Nil(t, tasklist)
}

func TestLoadGiven_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte(validTasksYAML), 0o600))
	promptPath := filepath.Join(dir, "missing.md")

	prompt, tasklist, err := loadGiven(tasksPath, promptPath)

	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "failed to start loop runner: "), err.Error())
	assert.Empty(t, prompt)
	assert.Nil(t, tasklist)
}

func TestLoadGiven_BadTasks(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "missing.yaml")

	_, tasklist, err := loadGiven(tasksPath, "")

	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "failed to start loop runner: "), err.Error())
	assert.NotErrorIs(t, err, looper.ErrFailedTasks)
	assert.Nil(t, tasklist)
}

func TestLoadGiven_FailedTask(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	failedYAML := "tasks:\n  - id: 1\n    name: first\n    prompt: do it\n    state: failed\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(failedYAML), 0o600))

	_, _, err := loadGiven(tasksPath, "")

	require.ErrorIs(t, err, looper.ErrFailedTasks)
	assert.False(t, strings.HasPrefix(err.Error(), "failed to start loop runner"), err.Error())
}

func TestLoadGiven_Valid(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte(validTasksYAML), 0o600))
	promptPath := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("  shared prompt\n"), 0o600))

	prompt, tasklist, err := loadGiven(tasksPath, promptPath)

	require.NoError(t, err)
	assert.Equal(t, "shared prompt", prompt)
	require.NotNil(t, tasklist)
	require.Len(t, tasklist.Tasks, 1)
	assert.Equal(t, "first", tasklist.Tasks[0].Name)
}

func TestIsPlain(t *testing.T) {
	tests := []struct {
		name                               string
		dryRun, noTUI, stdinTTY, stdoutTTY bool
		want                               bool
	}{
		{name: "dry run", dryRun: true, stdinTTY: true, stdoutTTY: true, want: true},
		{name: "no tui", noTUI: true, stdinTTY: true, stdoutTTY: true, want: true},
		{name: "stdin not a terminal", stdoutTTY: true, want: true},
		{name: "stdout not a terminal", stdinTTY: true, want: true},
		{name: "all terminals, no flags", stdinTTY: true, stdoutTTY: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPlain(tt.dryRun, tt.noTUI, tt.stdinTTY, tt.stdoutTTY))
		})
	}
}

func TestCheckGateTimeout(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "valid", in: "2m"},
		{name: "unitless", in: "30", wantErr: "--gate-timeout: must be a duration string such as 90s or 10m"},
		{name: "empty", in: "", wantErr: "--gate-timeout: must be a duration string such as 90s or 10m"},
		{name: "zero", in: "0s", wantErr: "--gate-timeout: must be greater than zero"},
		{name: "negative", in: "-5m", wantErr: "--gate-timeout: must be greater than zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkGateTimeout(tt.in)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, tt.wantErr)
		})
	}
}
