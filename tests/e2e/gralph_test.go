// Package e2e provides end-to-end black-box tests for the CLI tool.
// Tests execute the compiled binary and verify behavior from a user's perspective.
// No internal packages are imported — tests interact only with the binary via os/exec.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const defaultTimeout = 10 * time.Second

// testBinaryPath is set by TestMain after building the binary once.
var testBinaryPath string

// fakeClaudeDir is set by TestMain after building the fake claude fixture
// once. It holds a directory whose only entry is an executable named
// "claude" (or "claude.exe" on windows) so it can be prepended to a gralph
// child process's PATH.
var fakeClaudeDir string

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
	defer os.RemoveAll(tmpDir)

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
	fakeClaudeBin := filepath.Join(fakeClaudeDir, fakeClaudeBinaryName())
	build := exec.Command("go", "build", "-o", fakeClaudeBin, "./testdata/fakeclaude")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build fake claude fixture: %v\n", err)
		return 1
	}

	return m.Run()
}

// fakeClaudeBinaryName returns the executable name gralph resolves via PATH
// on the current platform.
func fakeClaudeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "claude.exe"
	}
	return "claude"
}

// runCLI executes the CLI binary with the given arguments and returns the result.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, testBinaryPath, args...) // #nosec G204 -- testBinaryPath is a test fixture

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
			t.Fatalf("CLI command timed out after %v: %v", defaultTimeout, args)
		} else {
			t.Fatalf("failed to execute CLI: %v", err)
		}
	}

	return result
}

// TestVersionFlag verifies that --version exits 0 and produces output.
func TestVersionFlag(t *testing.T) {
	result := runCLI(t, "--version")
	if result.exitCode != 0 {
		t.Errorf("expected exit 0 for --version, got %d; stderr: %s", result.exitCode, result.stderr)
	}
	if result.stdout == "" {
		t.Error("expected non-empty output for --version")
	}
}

// TestHelpFlag verifies that --help exits 0 (pflag exits 0 on ErrHelp).
func TestHelpFlag(t *testing.T) {
	result := runCLI(t, "--help")
	if result.exitCode != 0 {
		t.Errorf("expected exit 0 for --help, got %d; stderr: %s", result.exitCode, result.stderr)
	}
}

// TestMissingPrompt verifies that omitting --prompt exits non-zero.
func TestMissingPrompt(t *testing.T) {
	result := runCLI(t, "--tasks=/tmp/prd.md")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --prompt is missing")
	}
	if !strings.Contains(result.stderr, "--prompt") {
		t.Errorf("expected stderr to mention --prompt; got: %s", result.stderr)
	}
}

// TestMissingPrd verifies that omitting --tasks exits non-zero.
func TestMissingPrd(t *testing.T) {
	result := runCLI(t, "--prompt=/tmp/prompt.md")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --tasks is missing")
	}
	if !strings.Contains(result.stderr, "--tasks") {
		t.Errorf("expected stderr to mention --tasks; got: %s", result.stderr)
	}
}

// TestMissingBothRequiredFlags verifies that omitting both required flags exits non-zero.
func TestMissingBothRequiredFlags(t *testing.T) {
	result := runCLI(t)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when both --prompt and --tasks are missing")
	}
}

// TestNonexistentFiles verifies that valid flags pointing to missing files exits non-zero.
func TestNonexistentFiles(t *testing.T) {
	result := runCLI(t,
		"--prompt=/nonexistent/prompt.md",
		"--tasks=/nonexistent/prd.md",
	)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when referenced files do not exist")
	}
}
