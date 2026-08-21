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
	"runtime"
	"strings"
	"testing"
	"time"
)

const defaultTimeout = 10 * time.Second

// Binary paths are set by TestMain after building the test fixtures once.
var (
	testBinaryPath      string
	fakeAgentBinaryPath string
)

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

	fakeAgentBinaryPath = filepath.Join(tmpDir, "fake-agent")
	fakeAgentBuild := exec.Command("go", "build", "-o", fakeAgentBinaryPath, "./testdata/fakeagent") // #nosec G204 -- fixed literal args and test-owned output path
	fakeAgentBuild.Stdout = os.Stdout
	fakeAgentBuild.Stderr = os.Stderr
	if err := fakeAgentBuild.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build fake agent: %v\n", err)
		return 1
	}

	// When GRALPH_BINARY is set (e.g. inside the e2e Docker container), use that
	// pre-built binary and skip compilation entirely.
	if path := os.Getenv("GRALPH_BINARY"); path != "" {
		testBinaryPath = path
		return m.Run()
	}

	// Fallback: build from source for local development outside Docker.
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

type fakeAgentInvocation struct {
	Prompt       string `json:"prompt"`
	PRDPath      string `json:"prd_path"`
	ProgressPath string `json:"progress_path"`
}

func TestAgentLoopSuccess(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "PROMPT.md")
	prdPath := filepath.Join(dir, "PRD.md")
	progressPath := filepath.Join(dir, "progress.txt")
	recordPath := filepath.Join(dir, "fake-agent-invocation.json")
	promptTemplate := "# Fake agent prompt\n\nComplete the first open checklist item.\n"
	initialPRD := "# Test PRD\n\n- [ ] Cycle 1 - Exercise the fake agent\n"
	completedPRD := "# Test PRD\n\n- [x] Cycle 1 - Exercise the fake agent\n"

	if err := os.WriteFile(promptPath, []byte(promptTemplate), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte(initialPRD), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runCLI(t,
		"--prompt="+promptPath,
		"--prd="+prdPath,
		"--progress="+progressPath,
		"--agent-exec="+fakeAgentBinaryPath,
		"--agent-arg=--record="+recordPath,
		"--prompt-mode=stdin",
		"--iterations=1",
	)
	if result.exitCode != 0 {
		t.Fatalf("expected successful agent loop; exit=%d stdout=%q stderr=%q", result.exitCode, result.stdout, result.stderr)
	}
	if result.stderr != "" {
		t.Errorf("expected empty gralph stderr, got %q", result.stderr)
	}
	for _, expected := range []string{
		"Cycle 1: Exercise the fake agent",
		"- Status: Complete",
		"fake agent completed first checklist item",
	} {
		if !strings.Contains(result.stdout, expected) {
			t.Errorf("expected stdout to contain %q; got %q", expected, result.stdout)
		}
	}

	recordData, err := os.ReadFile(recordPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	var invocation fakeAgentInvocation
	if err := json.Unmarshal(recordData, &invocation); err != nil {
		t.Fatal(err)
	}
	wantPrompt := promptTemplate + fmt.Sprintf("\n## Runtime paths\n- PRD: %s\n- Progress: %s\n", prdPath, progressPath)
	if invocation.Prompt != wantPrompt {
		t.Errorf("unexpected prompt:\nwant: %q\n got: %q", wantPrompt, invocation.Prompt)
	}
	if invocation.PRDPath != prdPath {
		t.Errorf("unexpected PRD runtime path: want %q, got %q", prdPath, invocation.PRDPath)
	}
	if invocation.ProgressPath != progressPath {
		t.Errorf("unexpected progress runtime path: want %q, got %q", progressPath, invocation.ProgressPath)
	}

	prdData, err := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	if string(prdData) != completedPRD {
		t.Errorf("unexpected PRD transition:\nwant: %q\n got: %q", completedPRD, string(prdData))
	}
	if _, err := os.Stat(progressPath); err != nil {
		t.Errorf("expected progress file at runtime path: %v", err)
	}
}

func TestAgentRetry(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "PROMPT.md")
	prdPath := filepath.Join(dir, "PRD.md")
	progressPath := filepath.Join(dir, "progress.txt")
	recordPath := filepath.Join(dir, "fake-agent-invocation.json")
	attemptLogPath := filepath.Join(dir, "attempts.log")
	initialPRD := "# Test PRD\n\n- [ ] Cycle 1 - Retry a started agent\n"

	if err := os.WriteFile(promptPath, []byte("# Retry prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte(initialPRD), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runCLI(t,
		"--prompt="+promptPath,
		"--prd="+prdPath,
		"--progress="+progressPath,
		"--agent-exec="+fakeAgentBinaryPath,
		"--agent-arg=--record="+recordPath,
		"--agent-arg=--attempt-log="+attemptLogPath,
		"--agent-arg=--exit-code=7",
		"--prompt-mode=stdin",
		"--iterations=3",
	)
	if result.exitCode != 0 {
		t.Fatalf("expected exhausted retry policy to finish successfully; exit=%d stdout=%q stderr=%q", result.exitCode, result.stdout, result.stderr)
	}

	attemptData, err := os.ReadFile(attemptLogPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	if attempts := strings.Count(string(attemptData), "attempt\n"); attempts != 3 {
		t.Errorf("expected 3 started-agent attempts, got %d; log=%q", attempts, attemptData)
	}
	if runningStatuses := strings.Count(result.stdout, "- Status: Running"); runningStatuses != 3 {
		t.Errorf("expected 3 running statuses, got %d; stdout=%q", runningStatuses, result.stdout)
	}
	if !strings.Contains(result.stdout, "- Status: Failed") {
		t.Errorf("expected final failed status; stdout=%q", result.stdout)
	}
	if !strings.Contains(result.stdout, "fake agent exiting with status 7") {
		t.Errorf("expected final agent error output; stdout=%q", result.stdout)
	}

	prdData, err := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	wantPRD := strings.Replace(initialPRD, "- [ ]", "- [~]", 1)
	if string(prdData) != wantPRD {
		t.Errorf("unexpected PRD after retries:\nwant: %q\n got: %q", wantPRD, string(prdData))
	}
}

func TestAgentStartupFailure(t *testing.T) {
	t.Run("missing executable", func(t *testing.T) {
		dir := t.TempDir()
		assertAgentStartupFailure(t, dir, filepath.Join(dir, "missing-agent"), "stdin", nil, 1)
	})

	t.Run("non-executable file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not use Unix executable permission bits")
		}
		dir := t.TempDir()
		nonExecutablePath := filepath.Join(dir, "non-executable-agent")
		if err := os.WriteFile(nonExecutablePath, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertAgentStartupFailure(t, dir, nonExecutablePath, "stdin", nil, 1)
	})

	t.Run("invalid prompt configuration", func(t *testing.T) {
		dir := t.TempDir()
		recordPath := filepath.Join(dir, "unexpected-invocation.json")
		assertAgentStartupFailure(t, dir, fakeAgentBinaryPath, "arg", []string{"--record=" + recordPath}, 0)
		if _, err := os.Stat(recordPath); !os.IsNotExist(err) {
			t.Errorf("invalid prompt configuration invoked the agent: %v", err)
		}
	})
}

func assertAgentStartupFailure(t *testing.T, dir, executable, promptMode string, agentArgs []string, wantAttempts int) {
	t.Helper()
	promptPath := filepath.Join(dir, "PROMPT.md")
	prdPath := filepath.Join(dir, "PRD.md")
	progressPath := filepath.Join(dir, "progress.txt")
	initialPRD := []byte("# Test PRD\n\n- [ ] Cycle 1 - Preserve this item\n")
	if err := os.WriteFile(promptPath, []byte("# Fatal failure prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, initialPRD, 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"--prompt=" + promptPath,
		"--prd=" + prdPath,
		"--progress=" + progressPath,
		"--agent-exec=" + executable,
		"--prompt-mode=" + promptMode,
		"--iterations=3",
	}
	for _, arg := range agentArgs {
		args = append(args, "--agent-arg="+arg)
	}
	result := runCLI(t, args...)
	if result.exitCode == 0 {
		t.Fatalf("expected fatal agent failure to exit non-zero; stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	if attempts := strings.Count(result.stdout, "- Status: Running"); attempts != wantAttempts {
		t.Errorf("expected %d attempted starts, got %d; stdout=%q", wantAttempts, attempts, result.stdout)
	}

	prdData, err := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prdData, initialPRD) {
		t.Errorf("fatal failure mutated PRD:\nwant: %q\n got: %q", initialPRD, prdData)
	}
	if bytes.Contains(prdData, []byte("- [~]")) {
		t.Errorf("fatal failure abandoned PRD item: %q", prdData)
	}
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
