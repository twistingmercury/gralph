package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
