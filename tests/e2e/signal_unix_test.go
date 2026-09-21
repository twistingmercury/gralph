//go:build darwin || linux

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
)

// TestClaudeLoop_SIGINT_TerminatesDescendantAndLeavesPRDUntouched sends
// SIGINT to a blocked gralph run and verifies cancellation is honored: a
// non-zero exit, the PRD untouched, and the claude child's own descendant
// process gone (proving the whole process group was killed, not just the
// direct child).
func TestClaudeLoop_SIGINT_TerminatesDescendantAndLeavesPRDUntouched(t *testing.T) {
	testClaudeLoopSignal(t, os.Interrupt)
}

// TestClaudeLoop_SIGTERM_TerminatesDescendantAndLeavesPRDUntouched is the
// SIGTERM counterpart of TestClaudeLoop_SIGINT_TerminatesDescendantAndLeavesPRDUntouched.
func TestClaudeLoop_SIGTERM_TerminatesDescendantAndLeavesPRDUntouched(t *testing.T) {
	testClaudeLoopSignal(t, syscall.SIGTERM)
}

func testClaudeLoopSignal(t *testing.T, sig os.Signal) {
	t.Helper()

	dir := t.TempDir()

	prd := writePRD(t, dir, "- [ ] Only item: block forever")
	prompt := writePrompt(t, dir, "Body.\n")

	original, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD fixture: %v", err)
	}

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
	gp := startGralph(t, 30*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	// Wait for the fake claude fixture to spawn its descendant and signal
	// it's set up and blocking before sending the signal.
	waitForFile(t, readyFile, 10*time.Second)
	descendantPID := readPIDFile(t, descendantPIDFile, 5*time.Second)

	if err := gp.cmd.Process.Signal(sig); err != nil {
		t.Fatalf("send signal to gralph: %v", err)
	}

	exitCode := gp.waitExit(t, 10*time.Second)
	if exitCode == 0 {
		t.Errorf("expected non-zero exit after cancellation, got 0")
	}

	after, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf("expected PRD to be byte-for-byte unchanged after cancellation\nbefore:\n%s\nafter:\n%s", string(original), string(after))
	}

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
			t.Fatalf("descendant process %d is still alive after %v", pid, timeout)
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
