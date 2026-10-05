// Package e2e provides end-to-end black-box tests for the CLI tool.
// Tests execute the compiled binary and verify behavior from a user's perspective.
// No internal packages are imported — tests interact only with the binary via os/exec.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultTimeout = 10 * time.Second

// testBinaryPath is set by TestMain after building the binary once.
var testBinaryPath string

// fakeClaudeDir is set by TestMain after building the fake claude fixture
// once. It holds a directory whose only entry is an executable named
// "claude" so it can be prepended to a gralph
// child process's PATH.
var fakeClaudeDir string

// testHome is set by TestMain to an empty directory; the env helpers give
// every gralph run this HOME.
var testHome string

// cliResult captures the result of executing the CLI binary.
type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// TestMain is the entry point for the e2e test suite.
// It delegates to runTests so that deferred cleanup runs before os.Exit.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

// runTests sets up the binary path, runs the suite, and returns the exit code.
// Using a helper function ensures defer executes before the process exits.
func runTests(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "gralph-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		return 1
	}

	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to remove temp dir %s: %v\n", tmpDir, err)
		}
	}()

	// When GRALPH_BINARY is set (e.g. inside the e2e Docker container), use
	// that pre-built binary and skip compiling gralph itself. The fake
	// claude fixture still needs to be compiled from tests/e2e's own module.
	if path := os.Getenv("GRALPH_BINARY"); path != "" {
		testBinaryPath = path
	} else {
		testBinaryPath = filepath.Join(tmpDir, "gralph")

		// During `go test`, working directory is set to the package directory (tests/e2e).
		// Two levels up reaches the repo root; gralph's own module lives in
		// its src/ directory, and `go build` must run inside that module.
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get working dir: %v\n", err)
			return 1
		}

		moduleRoot := filepath.Join(wd, "..", "..", "src")

		build := exec.Command("go", "build", "-o", testBinaryPath, "./cmd/main") // #nosec G204 -- fixed literal args
		build.Dir = moduleRoot
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to build gralph binary: %v\n", err)
			return 1
		}
	}

	fakeClaudeDir = filepath.Join(tmpDir, "fakeclaude-bin")
	if err := os.Mkdir(fakeClaudeDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create fake claude bin dir: %v\n", err)
		return 1
	}

	fakeClaudeBin := filepath.Join(fakeClaudeDir, "claude")
	build := exec.Command("go", "build", "-o", fakeClaudeBin, "./testdata/fakeclaude")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build fake claude fixture: %v\n", err)
		return 1
	}

	testHome = filepath.Join(tmpDir, "home")
	if err := os.Mkdir(testHome, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create test HOME: %v\n", err)
		return 1
	}

	return m.Run()
}

// runCLI executes the CLI binary with the given arguments and returns the result.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, testBinaryPath, args...) // #nosec G204 -- testBinaryPath is a test fixture
	cmd.Env = gralphEnvWithPath(os.Getenv("PATH"), nil)
	// Bound how long Wait() can block flushing output after the context kills
	// the process, so a child that inherited stdout/stderr can't hang Wait
	// past the deadline.
	cmd.WaitDelay = 2 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Check for a timed-out context before interpreting the exit status: a
	// killed process also returns a non-nil *exec.ExitError, so checking exit
	// status first would let a hung gralph masquerade as a normal non-zero
	// exit instead of failing the test outright.
	if ctx.Err() != nil {
		require.FailNow(t, fmt.Sprintf("gralph did not exit within %v (args=%v)\nstdout:\n%s\nstderr:\n%s", defaultTimeout, args, stdout.String(), stderr.String()))
	}

	result := cliResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: 0,
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.exitCode = exitErr.ExitCode()
		} else {
			require.NoError(t, err, "failed to execute CLI")
		}
	}

	return result
}

// TestVersionFlag verifies that --version exits 0 and prints the version block.
func TestVersionFlag(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--version")
	assert.Equal(t, 0, result.exitCode, "stderr: %s", result.stderr)
	assert.True(t, strings.HasPrefix(result.stdout, "gralph version:"), "stdout: %s", result.stdout)
	wantOS := fmt.Sprintf("os: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	assert.Contains(t, result.stdout, wantOS)
}

// TestHelpFlag verifies that --help exits 0 (pflag exits 0 on ErrHelp) and
// lists every flag in the HOWTO's flag table. pflag writes usage to stderr.
func TestHelpFlag(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--help")
	assert.Equal(t, 0, result.exitCode, "stderr: %s", result.stderr)
	for _, flag := range []string{"--tasks", "--sandbox-settings", "--skip-permissions", "--gate-timeout", "--commit", "--dry-run", "--no-tui", "--install-skill", "--version"} {
		assert.Contains(t, result.stderr, flag)
	}
}

// TestMissingTasks verifies that omitting --tasks exits 1 and names only that
// flag. Usage follows the error and names every flag, so the error line
// itself is matched.
func TestMissingTasks(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--skip-permissions")
	require.Equal(t, 1, result.exitCode, "stderr: %s", result.stderr)
	assert.Contains(t, result.stderr, "error: required flag --tasks not set\n")
}

// TestPromptFlagIsRemoved pins ADR-020: the shared prompt lives in the task
// file, so -p and --prompt are unknown flags with pflag's exit code 2, and
// the missing --tasks is never reached.
func TestPromptFlagIsRemoved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "short", args: []string{"-p", "x", "-t", "y", "--skip-permissions"}, want: "unknown shorthand flag: 'p'"},
		{name: "long", args: []string{"--prompt", "x", "-t", "y", "--skip-permissions"}, want: "unknown flag: --prompt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := runCLI(t, tt.args...)
			require.Equal(t, 2, result.exitCode, "stdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
			assert.Contains(t, result.stderr, tt.want)
			assert.NotContains(t, result.stderr, "required flag")
		})
	}
}

// TestNonexistentFiles verifies that a valid flag pointing at a missing file
// exits 1 with an error naming the missing file.
func TestNonexistentFiles(t *testing.T) {
	t.Parallel()
	missingTasks := filepath.Join(t.TempDir(), "missing.yaml")

	result := runCLI(t, "--skip-permissions", "--tasks="+missingTasks)
	require.Equal(t, 1, result.exitCode, "stderr: %s", result.stderr)
	assert.Contains(t, result.stderr, fmt.Sprintf("tasks file %q is not accessible", missingTasks))
}
