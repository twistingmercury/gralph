//go:build linux

package e2e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stopPromptAnchor = "[y/N]"

// stopRun is a one-task run in the full-screen view whose fake claude blocks,
// so the task is still running when the keys arrive.
type stopRun struct {
	*ptyRun
	tasksPath string
	tasksYAML string
	claudePID int
}

// startStopRun starts the run and waits until the run view is up and the fake
// claude is running.
func startStopRun(t *testing.T) *stopRun {
	t.Helper()

	dir := t.TempDir()
	tasksYAML := withShared("Body.", "    []\n", `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)
	readyFile := filepath.Join(dir, "ready")

	// An empty gates list is a decision, so the setup wizard stays shut and
	// the run view opens at once.
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"TERM":                  "xterm-256color",
		"FAKECLAUDE_BLOCK":      "1",
		"FAKECLAUDE_READY_FILE": readyFile,
	})
	run := startPty(t, dir, env, "--skip-permissions", "-t", tasksPath)

	waitForOutput(t, run.out, "quit", 10*time.Second)
	waitForFile(t, readyFile, 10*time.Second)

	pid := readPID(t, readyFile)

	return &stopRun{ptyRun: run, tasksPath: tasksPath, tasksYAML: tasksYAML, claudePID: pid}
}

// readPID reads the fake claude's pid from its ready file. The fake truncates
// then writes, so the file can be seen empty first.
func readPID(t *testing.T, readyFile string) int {
	t.Helper()

	var pid int
	parse := func() bool {
		data, err := os.ReadFile(readyFile)
		if err != nil {
			return false
		}

		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil
	}
	require.Eventually(t, parse, 10*time.Second, 20*time.Millisecond, "fake claude's pid")

	return pid
}

func (r *stopRun) press(t *testing.T, key string) {
	t.Helper()

	_, err := r.ptmx.WriteString(key)
	require.NoError(t, err)
}

// waitForExit returns gralph's exit code, failing the test on a hang.
func (r *stopRun) waitForExit(t *testing.T) int {
	t.Helper()

	select {
	case code := <-r.exited:
		return code
	case <-time.After(15 * time.Second):
		_ = r.cmd.Process.Kill()
		output := r.out.String()
		msg := fmt.Sprintf("gralph did not exit\noutput:\n%s", output)
		require.FailNow(t, msg)
	}

	return 0
}

func (r *stopRun) claudeRunning() bool {
	err := syscall.Kill(r.claudePID, 0)
	return !errors.Is(err, syscall.ESRCH)
}

// assertStopped checks the outcomes of a confirmed stop: the exit code, an
// untouched task file, and no fake claude left behind.
func (r *stopRun) assertStopped(t *testing.T) {
	t.Helper()

	exitCode := r.waitForExit(t)
	output := r.out.String()
	assert.Equal(t, 1, exitCode, "output:\n%s", output)

	got, err := os.ReadFile(r.tasksPath)
	require.NoError(t, err)
	assert.Equal(t, r.tasksYAML, string(got), "a stop leaves the task pending")

	gone := func() bool { return !r.claudeRunning() }
	assert.Eventually(t, gone, 10*time.Second, 20*time.Millisecond, "fake claude still running")
}

// TestRunStopPtyYes pins the stop confirmation: q asks, y stops the run and
// leaves the task pending.
func TestRunStopPtyYes(t *testing.T) {
	run := startStopRun(t)

	run.press(t, "q")
	waitForOutput(t, run.out, stopPromptAnchor, 10*time.Second)
	run.press(t, "y")

	run.assertStopped(t)
}

// TestRunStopPtyNo pins that answering n keeps the run going, and that a later
// q then y still stops it.
func TestRunStopPtyNo(t *testing.T) {
	run := startStopRun(t)

	run.press(t, "q")
	waitForOutput(t, run.out, stopPromptAnchor, 10*time.Second)
	run.press(t, "n")

	// Nothing on screen says the n was handled, so watch for a stop that
	// should not come. A wrong stop shows within moments of the key.
	stopped := func() bool {
		select {
		case <-run.exited:
			return true
		default:
		}

		return !run.claudeRunning()
	}
	require.Never(t, stopped, time.Second, 25*time.Millisecond, "n must keep the run going")

	// The keys arrive in order, so this q opens a fresh prompt for the y.
	run.press(t, "q")
	run.press(t, "y")

	run.assertStopped(t)
}
