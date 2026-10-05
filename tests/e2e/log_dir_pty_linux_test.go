//go:build linux

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogDirPty pins ADR-016 end to end: a run in the full-screen view with
// --log-dir writes the ledger and the first task's log, and a run stopped from
// the keyboard still leaves a complete record. Only a terminal can start the
// full-screen view, which is why this runs under a pty.
func TestLogDirPty(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, `gates: []
tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`)
	logDir := filepath.Join(dir, "logs")
	readyFile := filepath.Join(dir, "ready")

	// An empty gates list is a decision, so the setup wizard stays shut and
	// the run view opens at once.
	// The fake blocks so the run is still open when q is pressed; its ready
	// file says the task has started, so the record has its first events.
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"TERM":                  "xterm-256color",
		"FAKECLAUDE_BLOCK":      "1",
		"FAKECLAUDE_READY_FILE": readyFile,
	})
	run := startPty(t, dir, env, "--skip-permissions", "-p", promptPath, "-t", tasksPath, "--log-dir="+logDir)

	waitForOutput(t, run.out, "quit", 10*time.Second)
	waitForFile(t, readyFile, 10*time.Second)

	_, err := run.ptmx.WriteString("q")
	require.NoError(t, err)
	waitForOutput(t, run.out, "[y/N]", 10*time.Second)

	_, err = run.ptmx.WriteString("y")
	require.NoError(t, err)

	var exitCode int
	select {
	case exitCode = <-run.exited:
	case <-time.After(15 * time.Second):
		_ = run.cmd.Process.Kill()
		msg := fmt.Sprintf("gralph did not exit after the stop was confirmed\noutput:\n%s", run.out.String())
		require.FailNow(t, msg)
	}

	assert.Equal(t, 1, exitCode, "output:\n%s", run.out.String())

	// Each run gets its own folder under --log-dir, named by its start time.
	runDirs, err := filepath.Glob(filepath.Join(logDir, "*"))
	require.NoError(t, err)
	require.Len(t, runDirs, 1, "expected one run folder under %s", logDir)
	runDir := runDirs[0]

	ledger, err := os.ReadFile(filepath.Join(runDir, "run.jsonl"))
	require.NoError(t, err, "expected the ledger")
	want := []string{"run_started", "task_started", "run_finished"}
	assert.Equal(t, want, ledgerEvents(t, ledger))

	assert.FileExists(t, filepath.Join(runDir, "task-1.log"))
}

// ledgerEvents returns the event name of every line in the ledger, in order.
func ledgerEvents(t *testing.T, ledger []byte) []string {
	t.Helper()

	var events []string
	scanner := bufio.NewScanner(bytes.NewReader(ledger))
	for scanner.Scan() {
		var line struct {
			Event string `json:"event"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line), "ledger line %q", scanner.Text())
		events = append(events, line.Event)
	}

	require.NoError(t, scanner.Err())
	return events
}
