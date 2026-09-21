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
	"strings"
	"testing"
	"time"
)

const defaultTimeout = 10 * time.Second

// testBinaryPath is set by TestMain after building the binary once.
var testBinaryPath string

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
	// When GRALPH_BINARY is set (e.g. inside the e2e Docker container), use that
	// pre-built binary and skip compilation entirely.
	if path := os.Getenv("GRALPH_BINARY"); path != "" {
		testBinaryPath = path
		return m.Run()
	}

	// Fallback: build from source for local development outside Docker.
	tmpDir, err := os.MkdirTemp("", "gralph-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

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

	return m.Run()
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
	result := runCLI(t, "--prd=/tmp/prd.md")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --prompt is missing")
	}
	if !strings.Contains(result.stderr, "--prompt") {
		t.Errorf("expected stderr to mention --prompt; got: %s", result.stderr)
	}
}

// TestMissingPrd verifies that omitting --prd exits non-zero.
func TestMissingPrd(t *testing.T) {
	result := runCLI(t, "--prompt=/tmp/prompt.md")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --prd is missing")
	}
	if !strings.Contains(result.stderr, "--prd") {
		t.Errorf("expected stderr to mention --prd; got: %s", result.stderr)
	}
}

// TestMissingBothRequiredFlags verifies that omitting both required flags exits non-zero.
func TestMissingBothRequiredFlags(t *testing.T) {
	result := runCLI(t)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when both --prompt and --prd are missing")
	}
}

// TestNonexistentFiles verifies that valid flags pointing to missing files exits non-zero.
func TestNonexistentFiles(t *testing.T) {
	result := runCLI(t,
		"--prompt=/nonexistent/prompt.md",
		"--prd=/nonexistent/prd.md",
	)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when referenced files do not exist")
	}
}
