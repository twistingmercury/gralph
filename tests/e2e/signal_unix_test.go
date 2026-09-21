package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoop_SIGINT_KillsProcessTree sends SIGINT to a blocked gralph run and
// verifies cancellation is honored: a non-zero exit, tasks.yaml untouched,
// the second task never started, and the claude child's own descendant
// process gone (proving the whole process group was killed, not just the
// direct child).
func TestLoop_SIGINT_KillsProcessTree(t *testing.T) {
	testLoopSignal(t, os.Interrupt)
}

// TestLoop_SIGTERM_KillsProcessTree is the SIGTERM counterpart of
// TestLoop_SIGINT_KillsProcessTree.
func TestLoop_SIGTERM_KillsProcessTree(t *testing.T) {
	testLoopSignal(t, syscall.SIGTERM)
}

func testLoopSignal(t *testing.T, sig os.Signal) {
	t.Helper()

	dir := t.TempDir()

	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `tasks:
  - id: 1
    name: First task
    prompt: Block forever.
  - id: 2
    name: Second task
    prompt: Should never start.
`)

	original, err := os.ReadFile(tasksPath)
	require.NoError(t, err)

	readyFile := filepath.Join(dir, "ready")
	descendantPIDFile := filepath.Join(dir, "descendant.pid")

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_BLOCK":               "1",
		"FAKECLAUDE_READY_FILE":          readyFile,
		"FAKECLAUDE_DESCENDANT_PID_FILE": descendantPIDFile,
	})

	// Generous safety-net timeout: if the signal never terminates gralph
	// (a real bug, not expected flakiness), the process is still killed and
	// the test still fails via waitExit rather than hanging the suite.
	gp := startGralph(t, 30*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	// Wait for the fake claude fixture to spawn its descendant and signal
	// it's set up and blocking before sending the signal.
	waitForFile(t, readyFile, 10*time.Second)
	descendantPID := readPIDFile(t, descendantPIDFile, 5*time.Second)

	require.NoError(t, gp.cmd.Process.Signal(sig), "send signal to gralph")

	exitCode := gp.waitExit(t, 10*time.Second)
	assert.NotEqual(t, 0, exitCode, "expected non-zero exit after cancellation")

	after, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after), "expected tasks.yaml to be byte-for-byte unchanged after cancellation")

	assert.NotContains(t, gp.stdout.String(), "Should never start.", "expected the second task's prompt never to be printed")

	waitProcessGone(t, descendantPID, 10*time.Second)
}

// waitProcessGone polls until pid no longer refers to a live process,
// failing the test if it's still around after timeout.
func waitProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return
		}
		if time.Now().After(deadline) {
			require.FailNow(t, fmt.Sprintf("descendant process %d is still alive after %v", pid, timeout))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// processAlive reports whether pid refers to a live, non-zombie process.
// kill(pid, 0) alone is insufficient on Linux: it still succeeds for
// zombies (exited but not yet reaped by their parent), which can briefly be
// true for an orphaned descendant reparented to a subreaper after its
// direct parent (the fake claude fixture) is killed alongside it.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	if runtime.GOOS != "linux" {
		return true
	}

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	closingParen := strings.LastIndexByte(string(stat), ')')
	if closingParen == -1 {
		return true
	}
	fields := strings.Fields(string(stat[closingParen+1:]))
	return len(fields) == 0 || (fields[0] != "Z" && fields[0] != "X")
}
