package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClaudeRecord mirrors the JSON record written by
// tests/e2e/testdata/fakeclaude to FAKECLAUDE_RECORD_FILE.
type fakeClaudeRecord struct {
	Argv  []string `json:"argv"`
	Stdin string   `json:"stdin"`
}

// gralphResult captures the result of running the gralph binary to completion.
type gralphResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// gralphEnv builds an environment for the gralph process under test with
// claudeDir prepended to PATH (so gralph's own `exec.Command("claude", ...)`
// PATH lookup resolves to the fixture there) plus any extra FAKECLAUDE_* (or
// other) variables, which flow through to the claude child since gralph
// never overrides cmd.Env for that child.
func gralphEnv(claudeDir string, extra map[string]string) []string {
	return gralphEnvWithPath(claudeDir+string(os.PathListSeparator)+os.Getenv("PATH"), extra)
}

// gralphEnvWithPath is like gralphEnv but sets PATH to exactly pathValue,
// with no fallback to the outer test process's PATH. Used to simulate
// claude being entirely absent from PATH.
func gralphEnvWithPath(pathValue string, extra map[string]string) []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+len(extra)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "PATH="+pathValue)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// runGralph runs the gralph binary to completion with the given args and
// environment, enforcing timeout as a hang guard.
func runGralph(t *testing.T, timeout time.Duration, args []string, env []string) gralphResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, testBinaryPath, args...)
	cmd.Env = env
	// Bound how long Wait() can block flushing output after the context
	// kills the process, so a child that inherited stdout/stderr can't hang
	// Wait past the deadline.
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
		require.FailNow(t, fmt.Sprintf("gralph did not exit within %v (args=%v)\nstdout:\n%s\nstderr:\n%s", timeout, args, stdout.String(), stderr.String()))
	}

	result := gralphResult{stdout: stdout.String(), stderr: stderr.String()}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.exitCode = exitErr.ExitCode()
		} else {
			require.NoError(t, err, "failed to execute gralph")
		}
	}

	return result
}

// gralphProcess wraps a gralph process started for a signal test, along with
// its captured output buffers and a channel that receives the Wait() result.
type gralphProcess struct {
	cmd    *exec.Cmd
	done   chan error
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

// startGralph starts the gralph binary without waiting for it to finish. A
// safety-net timeout kills the process if the test never sends a signal (or
// the signal has no effect), so a broken test cannot hang the suite forever.
func startGralph(t *testing.T, safetyTimeout time.Duration, args []string, env []string) *gralphProcess {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), safetyTimeout)

	cmd := exec.CommandContext(ctx, testBinaryPath, args...)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		cancel()
		require.NoError(t, err, "failed to start gralph")
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		cancel()
	}()

	t.Cleanup(func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	return &gralphProcess{cmd: cmd, done: done, stdout: &stdout, stderr: &stderr}
}

// waitExit blocks until the gralph process exits, failing the test if it
// doesn't within timeout. Returns the process exit code.
func (gp *gralphProcess) waitExit(t *testing.T, timeout time.Duration) int {
	t.Helper()

	select {
	case err := <-gp.done:
		if err == nil {
			return 0
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		require.NoError(t, err, "gralph exited with unexpected error")
		return -1
	case <-time.After(timeout):
		require.FailNow(t, fmt.Sprintf("gralph did not exit within %v; stdout:\n%s\nstderr:\n%s", timeout, gp.stdout.String(), gp.stderr.String()))
		return -1
	}
}

// writeTasksYAML writes a tasks.yaml fixture with the given content
// (unmodified) and returns its path.
func writeTasksYAML(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// writePrompt writes a prompt.md fixture with the given body and returns its path.
func writePrompt(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// expectedStdin reproduces the exact wire format runLoop sends to claude on
// stdin: sharedPrompt is the already-trimmed prompt file content, and id,
// name, prompt describe the task being run. It mirrors
// fmt.Sprintf("%s\n\n%s\n", p, task.String()) in internal/looper/looper.go
// and Task.String() in internal/tasks/task.go exactly, so tests can
// golden-assert recorded stdin.
func expectedStdin(sharedPrompt string, id int, name, prompt string) string {
	taskStr := strings.TrimSpace(fmt.Sprintf("%d: %s\n\n%s", id, name, prompt))
	return fmt.Sprintf("%s\n\n%s\n", sharedPrompt, taskStr)
}

// readFakeClaudeRecords reads and decodes the newline-delimited JSON
// invocation records appended by the fake claude fixture to
// FAKECLAUDE_RECORD_FILE, one entry per invocation in call order. A missing
// file returns no records.
func readFakeClaudeRecords(t *testing.T, path string) []fakeClaudeRecord {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err, "read fake claude record %s", path)
	}

	var records []fakeClaudeRecord
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec fakeClaudeRecord
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "unmarshal fake claude record line %q", line)
		records = append(records, rec)
	}
	return records
}

// countAttempts returns the number of lines the fake claude fixture appended
// to an attempt log, i.e. the number of times it was invoked. A missing file
// counts as zero invocations.
func countAttempts(t *testing.T, path string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		require.NoError(t, err, "read attempt log %s", path)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// waitForFile polls until path exists and returns its contents, failing the
// test if it doesn't appear within timeout.
func waitForFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return data
		}
		if !os.IsNotExist(err) {
			require.NoError(t, err, "read %s", path)
		}
		if time.Now().After(deadline) {
			require.FailNow(t, fmt.Sprintf("timed out after %v waiting for %s to appear", timeout, path))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readPIDFile waits for a fixture-written PID file to appear and parses it.
func readPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()

	data := waitForFile(t, path, timeout)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err, "parse pid from %s (content %q)", path, string(data))
	return pid
}
