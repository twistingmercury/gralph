// Package e2e provides end-to-end black-box tests for the CLI tool.
// Tests execute the compiled binary and verify behavior from a user's perspective.
// No internal packages are imported — tests interact only with the binary via os/exec.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	help := result.stdout + result.stderr
	for _, flag := range []string{"--agent-exec", "--agent-arg", "--prompt-mode"} {
		if !strings.Contains(help, flag) {
			t.Errorf("expected help to document %s; got: %s", flag, help)
		}
	}
}

// TestMissingPrompt verifies that omitting --prompt exits non-zero.
func TestMissingPrompt(t *testing.T) {
	result := runCLI(t, "--prd=/tmp/prd.md", "--agent-exec=unused-agent")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --prompt is missing")
	}
	if !strings.Contains(result.stderr, "--prompt") {
		t.Errorf("expected stderr to mention --prompt; got: %s", result.stderr)
	}
}

// TestMissingPrd verifies that omitting --prd exits non-zero.
func TestMissingPrd(t *testing.T) {
	result := runCLI(t, "--prompt=/tmp/prompt.md", "--agent-exec=unused-agent")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --prd is missing")
	}
	if !strings.Contains(result.stderr, "--prd") {
		t.Errorf("expected stderr to mention --prd; got: %s", result.stderr)
	}
}

// TestMissingBothRequiredFlags verifies that omitting both required flags exits non-zero.
func TestMissingBothRequiredFlags(t *testing.T) {
	result := runCLI(t, "--agent-exec=unused-agent")
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when both --prompt and --prd are missing")
	}
}

// TestNonexistentFiles verifies that valid flags pointing to missing files exits non-zero.
func TestNonexistentFiles(t *testing.T) {
	result := runCLI(t,
		"--prompt=/nonexistent/prompt.md",
		"--prd=/nonexistent/prd.md",
		"--agent-exec=unused-agent",
	)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when referenced files do not exist")
	}
}

func TestMissingAgentExecutable(t *testing.T) {
	result := runCLI(t,
		"--prompt=/nonexistent/prompt.md",
		"--prd=/nonexistent/prd.md",
	)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit when --agent-exec is missing")
	}
	if !strings.Contains(result.stderr, "--agent-exec") {
		t.Errorf("expected stderr to mention --agent-exec; got: %s", result.stderr)
	}
}

func TestInvalidPromptMode(t *testing.T) {
	result := runCLI(t,
		"--prompt=/nonexistent/prompt.md",
		"--prd=/nonexistent/prd.md",
		"--agent-exec=unused-agent",
		"--prompt-mode=environment",
	)
	if result.exitCode == 0 {
		t.Fatal("expected non-zero exit for an invalid prompt mode")
	}
	if !strings.Contains(result.stderr, "unsupported prompt mode") {
		t.Errorf("expected stderr to identify the invalid prompt mode; got: %s", result.stderr)
	}
}

type agentFlagsInvocation struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

func TestAgentFlags(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	recordPath := filepath.Join(dir, "agent invocation.json")
	shellMarkerPath := filepath.Join(dir, "shell-marker")
	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] Verify agent flags\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	agentExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	agentArgs := []string{
		"-test.run=^TestAgentFlagsHelper$",
		"--",
		"--agent-flags-helper",
		recordPath,
		prdPath,
		"argument with spaces",
		"comma,value",
		"$(touch " + shellMarkerPath + ") ; & |",
	}
	cliArgs := []string{
		"--prompt=" + promptPath,
		"--prd=" + prdPath,
		"--agent-exec=" + agentExecutable,
		"--prompt-mode=stdin",
		"--iterations=1",
	}
	for _, arg := range agentArgs {
		cliArgs = append(cliArgs, "--agent-arg="+arg)
	}

	result := runCLI(t, cliArgs...)
	if result.exitCode != 0 {
		t.Fatalf("expected configured agent invocation to succeed; exit=%d stderr=%s", result.exitCode, result.stderr)
	}

	data, err := os.ReadFile(recordPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	var invocation agentFlagsInvocation
	if err := json.Unmarshal(data, &invocation); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(invocation.Args, agentArgs) {
		t.Fatalf("agent arguments lost order or boundaries:\nwant: %#v\n got: %#v", agentArgs, invocation.Args)
	}
	if !strings.Contains(invocation.Stdin, "# Prompt\n") {
		t.Fatalf("expected prompt on stdin, got %q", invocation.Stdin)
	}
	if _, err := os.Stat(shellMarkerPath); !os.IsNotExist(err) {
		t.Fatalf("agent arguments were interpreted by a shell: %v", err)
	}
}

func TestAgentFlagsHelper(t *testing.T) {
	const marker = "--agent-flags-helper"

	markerIndex := -1
	for i, arg := range os.Args {
		if arg == marker {
			markerIndex = i
			break
		}
	}
	if markerIndex == -1 {
		return
	}
	if markerIndex+2 >= len(os.Args) {
		os.Exit(2)
	}

	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(3)
	}
	record := agentFlagsInvocation{
		Args:  append([]string(nil), os.Args[1:]...),
		Stdin: string(stdin),
	}
	data, err := json.Marshal(record)
	if err != nil {
		os.Exit(4)
	}
	if err := os.WriteFile(os.Args[markerIndex+1], data, 0o600); err != nil {
		os.Exit(5)
	}
	if err := os.WriteFile(os.Args[markerIndex+2], []byte("# PRD\n\n- [x] Verify agent flags\n"), 0o600); err != nil {
		os.Exit(6)
	}
	os.Exit(0)
}
