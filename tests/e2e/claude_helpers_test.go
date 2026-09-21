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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := gralphResult{stdout: stdout.String(), stderr: stderr.String()}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("gralph timed out after %v (args=%v)\nstdout:\n%s\nstderr:\n%s", timeout, args, stdout.String(), stderr.String())
		} else {
			t.Fatalf("failed to execute gralph: %v", err)
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
		t.Fatalf("failed to start gralph: %v", err)
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
		t.Fatalf("gralph exited with unexpected error: %v", err)
		return -1
	case <-time.After(timeout):
		t.Fatalf("gralph did not exit within %v; stdout:\n%s\nstderr:\n%s", timeout, gp.stdout.String(), gp.stderr.String())
		return -1
	}
}

// writePRD writes a PRD.md fixture from the given checklist lines (each
// element becomes one line, unmodified) and returns its path.
func writePRD(t *testing.T, dir string, lines ...string) string {
	t.Helper()

	path := filepath.Join(dir, "PRD.md")
	content := "# PRD\n\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write PRD fixture: %v", err)
	}
	return path
}

// writePrompt writes a PROMPT.md fixture with the given body and returns its path.
func writePrompt(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "PROMPT.md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write prompt fixture: %v", err)
	}
	return path
}

// runtimeBlock mirrors the exact "## Runtime paths" block looper.go appends
// to the prompt before sending it to claude on stdin.
func runtimeBlock(prd string) string {
	return fmt.Sprintf("\n## Runtime paths\n- PRD: %s\n", prd)
}

// readFakeClaudeRecord reads and decodes the JSON invocation record written
// by the fake claude fixture.
func readFakeClaudeRecord(t *testing.T, path string) fakeClaudeRecord {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fake claude record %s: %v", path, err)
	}
	var rec fakeClaudeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal fake claude record %s: %v", path, err)
	}
	return rec
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
		t.Fatalf("read attempt log %s: %v", path, err)
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
			t.Fatalf("read %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s to appear", timeout, path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readPIDFile waits for a fixture-written PID file to appear and parses it.
func readPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()

	data := waitForFile(t, path, timeout)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse pid from %s (content %q): %v", path, string(data), err)
	}
	return pid
}
