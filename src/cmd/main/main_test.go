package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
