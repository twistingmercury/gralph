// Package e2e provides end-to-end black-box tests for the CLI tool.
// These tests execute the compiled binary and verify behavior from a user's perspective.
// No internal packages are imported - tests interact only with the binary via os/exec.
package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// defaultTimeout is the maximum duration for any single CLI command execution.
	defaultTimeout = 10 * time.Second

	// binaryAbsPath is the relative path from project root to the binary.
	binaryAbsPath = "$HOME/go/bin/gralph"
)

// cliResult captures the result of executing the CLI binary.
type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// getBinaryPath returns the absolute path to the CLI binary.
// It looks for the binary in the .bin directory relative to the project root.
func getBinaryPath(t *testing.T) string {
	t.Helper()

	// Walk up from the test directory to find the project root
	// Test runs from: /project/test/e2e
	// Binary is at: /project/.bin/cli-tool
	wd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")

	// Try to find the binary relative to the working directory
	// When running tests, wd could be the test directory or project root
	possiblePaths := []string{
		filepath.Join(wd, binaryAbsPath),
		filepath.Join(wd, "..", "..", binaryAbsPath),
		filepath.Join(wd, "..", binaryAbsPath),
	}

	for _, path := range possiblePaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	// If not found via relative paths, check if BINARY_PATH env var is set
	if envPath := os.Getenv("CLI_BINARY_PATH"); envPath != "" {
		absPath, err := filepath.Abs(envPath)
		if err == nil {
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}

	t.Skipf("CLI binary not found. Please run 'make build' first to compile the binary. "+
		"Searched paths: %v", possiblePaths)
	return ""
}

// runCLI executes the CLI binary with the given arguments and returns the result.
// It captures stdout, stderr, and the exit code.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	binaryPath := getBinaryPath(t)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := cliResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("CLI command timed out after %v: %s %v", defaultTimeout, binaryPath, args)
		} else {
			t.Fatalf("failed to execute CLI: %v", err)
		}
	}

	return result
}

// TestVersionSubcommand tests the 'version' subcommand output format.
func TestVersionSubcommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		wantExitCode  int
		wantStdout    []string // Lines that should be present in stdout
		wantStderrLen int      // Expected length of stderr (0 for no stderr)
		wantPattern   *regexp.Regexp
	}{
		{
			name:          "version command produces correct output format",
			args:          []string{"version"},
			wantExitCode:  0,
			wantStdout:    []string{"version:", "date:", "commit:"},
			wantStderrLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"unexpected exit code\nstdout: %s\nstderr: %s", result.stdout, result.stderr)

			for _, line := range tt.wantStdout {
				assert.Contains(t, result.stdout, line,
					"stdout should contain %q", line)
			}

			if tt.wantStderrLen == 0 {
				assert.Empty(t, result.stderr, "stderr should be empty")
			}
		})
	}
}

// TestVersionOutputFormat validates the exact format of the version output.
func TestVersionOutputFormat(t *testing.T) {
	t.Parallel()

	result := runCLI(t, "version")

	assert.Equal(t, 0, result.exitCode, "version command should exit with code 0")

	lines := strings.Split(strings.TrimSpace(result.stdout), "\n")
	require.Len(t, lines, 3, "version output should have exactly 3 lines")

	// Validate each line format: "key: value" (value may be empty)
	versionPattern := regexp.MustCompile(`^version:\s*.*$`)
	datePattern := regexp.MustCompile(`^date:\s*.*$`)
	commitPattern := regexp.MustCompile(`^commit:\s*.*$`)

	assert.Regexp(t, versionPattern, lines[0],
		"first line should match 'version: <value>' format")
	assert.Regexp(t, datePattern, lines[1],
		"second line should match 'date: <value>' format")
	assert.Regexp(t, commitPattern, lines[2],
		"third line should match 'commit: <value>' format")
}

// TestHelpFlag tests the --help flag behavior for the root command.
func TestHelpFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		args               []string
		wantExitCode       int
		wantStdoutContains []string
		wantStderrEmpty    bool
	}{
		{
			name:         "root help with --help flag",
			args:         []string{"--help"},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"cli-build-example",
				"Available Commands:",
				"version",
				"Flags:",
				"--help",
			},
			wantStderrEmpty: true,
		},
		{
			name:         "root help with -h flag",
			args:         []string{"-h"},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"cli-build-example",
				"Available Commands:",
			},
			wantStderrEmpty: true,
		},
		{
			name:         "no arguments shows help",
			args:         []string{},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"cli-build-example",
			},
			wantStderrEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"unexpected exit code\nstdout: %s\nstderr: %s", result.stdout, result.stderr)

			for _, want := range tt.wantStdoutContains {
				assert.Contains(t, result.stdout, want,
					"stdout should contain %q", want)
			}

			if tt.wantStderrEmpty {
				assert.Empty(t, result.stderr, "stderr should be empty")
			}
		})
	}
}

// TestVersionHelp tests the 'version --help' output.
func TestVersionHelp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		args               []string
		wantExitCode       int
		wantStdoutContains []string
	}{
		{
			name:         "version --help shows version command help",
			args:         []string{"version", "--help"},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"version",
				"Usage:",
			},
		},
		{
			name:         "version -h shows version command help",
			args:         []string{"version", "-h"},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"version",
				"Usage:",
			},
		},
		{
			name:         "help version shows version command help",
			args:         []string{"help", "version"},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"version",
				"Usage:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"unexpected exit code\nstdout: %s\nstderr: %s", result.stdout, result.stderr)

			for _, want := range tt.wantStdoutContains {
				assert.Contains(t, result.stdout, want,
					"stdout should contain %q", want)
			}
		})
	}
}

// TestInvalidSubcommand tests handling of invalid/unknown subcommands.
func TestInvalidSubcommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		args               []string
		wantExitCode       int
		wantStderrContains []string
	}{
		{
			name:               "unknown subcommand returns error",
			args:               []string{"nonexistent"},
			wantExitCode:       1,
			wantStderrContains: []string{"unknown command"},
		},
		{
			name:               "unknown subcommand with random name",
			args:               []string{"foobar123"},
			wantExitCode:       1,
			wantStderrContains: []string{"unknown command"},
		},
		{
			name:               "typo in version command",
			args:               []string{"versoin"},
			wantExitCode:       1,
			wantStderrContains: []string{"unknown command"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"unexpected exit code\nstdout: %s\nstderr: %s", result.stdout, result.stderr)

			for _, want := range tt.wantStderrContains {
				assert.Contains(t, result.stderr, want,
					"stderr should contain %q", want)
			}
		})
	}
}

// TestInvalidFlags tests handling of invalid/unknown flags.
func TestInvalidFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		args               []string
		wantExitCode       int
		wantStderrContains []string
	}{
		{
			name:               "unknown flag on root command",
			args:               []string{"--nonexistent"},
			wantExitCode:       1,
			wantStderrContains: []string{"unknown flag"},
		},
		{
			name:               "unknown flag on version command",
			args:               []string{"version", "--nonexistent"},
			wantExitCode:       1,
			wantStderrContains: []string{"unknown flag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"unexpected exit code\nstdout: %s\nstderr: %s", result.stdout, result.stderr)

			for _, want := range tt.wantStderrContains {
				assert.Contains(t, result.stderr, want,
					"stderr should contain %q", want)
			}
		})
	}
}

// TestExitCodes verifies proper exit codes for various scenarios.
func TestExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantExitCode int
		description  string
	}{
		{
			name:         "version command success",
			args:         []string{"version"},
			wantExitCode: 0,
			description:  "successful version command should exit with 0",
		},
		{
			name:         "help flag success",
			args:         []string{"--help"},
			wantExitCode: 0,
			description:  "help flag should exit with 0",
		},
		{
			name:         "no args success",
			args:         []string{},
			wantExitCode: 0,
			description:  "no arguments should exit with 0 (shows help)",
		},
		{
			name:         "unknown command error",
			args:         []string{"unknown"},
			wantExitCode: 1,
			description:  "unknown command should exit with non-zero",
		},
		{
			name:         "unknown flag error",
			args:         []string{"--badFlag"},
			wantExitCode: 1,
			description:  "unknown flag should exit with non-zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"%s\nstdout: %s\nstderr: %s", tt.description, result.stdout, result.stderr)
		})
	}
}

// TestCompletionCommand tests the built-in completion command if available.
func TestCompletionCommand(t *testing.T) {
	t.Parallel()

	// Cobra typically includes a completion command
	result := runCLI(t, "completion", "--help")

	// If completion command exists, it should show help (exit 0)
	// If it doesn't exist, it will show "unknown command" (exit 1)
	if result.exitCode == 0 {
		assert.Contains(t, result.stdout, "completion",
			"completion help should mention 'completion'")
	} else {
		// Completion command not enabled - this is acceptable
		t.Log("completion command not available - skipping detailed tests")
	}
}

// TestHelpCommand tests the built-in 'help' command.
func TestHelpCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		args               []string
		wantExitCode       int
		wantStdoutContains []string
		wantStderrContains []string
	}{
		{
			name:         "help command shows help",
			args:         []string{"help"},
			wantExitCode: 0,
			wantStdoutContains: []string{
				"cli-build-example",
				"Available Commands:",
			},
			wantStderrContains: []string{},
		},
		{
			// Note: Cobra's help command returns exit 0 even for unknown topics,
			// but prints "Unknown help topic" to stderr
			name:               "help with unknown command shows error in stderr",
			args:               []string{"help", "nonexistent"},
			wantExitCode:       0, // Cobra's help returns 0 even for unknown topics
			wantStdoutContains: []string{},
			wantStderrContains: []string{"Unknown help topic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := runCLI(t, tt.args...)

			assert.Equal(t, tt.wantExitCode, result.exitCode,
				"unexpected exit code\nstdout: %s\nstderr: %s", result.stdout, result.stderr)

			for _, want := range tt.wantStdoutContains {
				assert.Contains(t, result.stdout, want,
					"stdout should contain %q", want)
			}

			for _, want := range tt.wantStderrContains {
				assert.Contains(t, result.stderr, want,
					"stderr should contain %q", want)
			}
		})
	}
}
