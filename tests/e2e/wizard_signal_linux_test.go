//go:build linux

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWizardSignal pins that SIGTERM and SIGINT while the setup wizard is
// open cancel it cleanly. Bubble Tea's own signal handler used to race
// gralph's signal context, so gralph panicked (exit 2) or hung. The race was
// intermittent, so each signal runs several times.
func TestWizardSignal(t *testing.T) {
	signals := []struct {
		name string
		sig  syscall.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGINT", syscall.SIGINT},
	}

	for _, s := range signals {
		for i := range 3 {
			t.Run(fmt.Sprintf("%s/%d", s.name, i), func(t *testing.T) {
				testWizardSignal(t, s.name, s.sig)
			})
		}
	}
}

func testWizardSignal(t *testing.T, name string, sig syscall.Signal) {
	t.Helper()

	workDir := t.TempDir()
	runDir := filepath.Join(workDir, ".local", "run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	tasksPath := writeTasksYAML(t, runDir, withShared("Body.", "", `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`))

	original, err := os.ReadFile(tasksPath)
	require.NoError(t, err)

	path := os.Getenv("PATH")
	env := gralphEnvWithPath(path, map[string]string{"TERM": "xterm-256color"})
	run := startPty(t, workDir, env)

	waitForOutput(t, run.out, "Run folder", 10*time.Second)

	require.NoError(t, run.cmd.Process.Signal(sig), "send %s to gralph", name)

	var exitCode int
	select {
	case exitCode = <-run.exited:
	case <-time.After(10 * time.Second):
		_ = run.cmd.Process.Kill()
		partial := run.out.String()
		msg := fmt.Sprintf("gralph did not exit after %s\noutput:\n%s", name, partial)
		require.FailNow(t, msg)
	}

	output := run.out.String()
	assert.Equal(t, 1, exitCode, "output:\n%s", output)
	assert.Contains(t, output, "setup cancelled")
	assert.NotContains(t, output, "panic:")

	after, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after), "expected tasks.yaml to be byte-for-byte unchanged")
}
