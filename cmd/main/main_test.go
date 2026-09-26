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
