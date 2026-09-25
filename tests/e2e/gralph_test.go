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

// skillHome is set by TestMain to a HOME in which `gralph --install-skill`
// has already run; the env helpers give every gralph run this HOME.
var skillHome string

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
		// Two levels up reaches the module root.
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get working dir: %v\n", err)
			return 1
		}
		projectRoot := filepath.Join(wd, "..", "..")

		build := exec.Command("go", "build", "-o", testBinaryPath, "./cmd/main") // #nosec G204 -- fixed literal args
		build.Dir = projectRoot
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

	skillHome = filepath.Join(tmpDir, "home")
	install := exec.Command(testBinaryPath, "--install-skill")
	install.Env = append(os.Environ(), "HOME="+skillHome)
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr
	if err := install.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to install skill into test HOME: %v\n", err)
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

// TestVersionFlag verifies that --version exits 0 and produces output.
func TestVersionFlag(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--version")
	assert.Equal(t, 0, result.exitCode, "stderr: %s", result.stderr)
	assert.NotEmpty(t, result.stdout)
}

// TestHelpFlag verifies that --help exits 0 (pflag exits 0 on ErrHelp).
func TestHelpFlag(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--help")
	assert.Equal(t, 0, result.exitCode, "stderr: %s", result.stderr)
}

// TestMissingPrompt verifies that omitting --prompt exits non-zero.
func TestMissingPrompt(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--tasks=/tmp/tasks.yaml")
	require.NotEqual(t, 0, result.exitCode, "expected non-zero exit when --prompt is missing")
	assert.Contains(t, result.stderr, "--prompt")
}

// TestMissingTasks verifies that omitting --tasks exits non-zero.
func TestMissingTasks(t *testing.T) {
	t.Parallel()
	result := runCLI(t, "--prompt=/tmp/prompt.md")
	require.NotEqual(t, 0, result.exitCode, "expected non-zero exit when --tasks is missing")
	assert.Contains(t, result.stderr, "--tasks")
}

// TestMissingBothRequiredFlags verifies that omitting both required flags exits non-zero.
func TestMissingBothRequiredFlags(t *testing.T) {
	t.Parallel()
	result := runCLI(t)
	require.NotEqual(t, 0, result.exitCode, "expected non-zero exit when both --prompt and --tasks are missing")
}

// TestNonexistentFiles verifies that valid flags pointing to missing files exit non-zero
// with an error naming the missing prompt/tasks file.
func TestNonexistentFiles(t *testing.T) {
	t.Parallel()
	result := runCLI(t,
		"--prompt=/nonexistent/prompt.md",
		"--tasks=/nonexistent/tasks.yaml",
	)
	require.NotEqual(t, 0, result.exitCode, "expected non-zero exit when referenced files do not exist")
	assert.Contains(t, result.stderr, "prompt file")
	assert.Contains(t, result.stderr, "is not accessible")
}
