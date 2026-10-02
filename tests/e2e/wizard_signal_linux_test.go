//go:build linux

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer is written by the pty drain goroutine and read by the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

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

	writePrompt(t, runDir, "Body.\n")
	tasksPath := writeTasksYAML(t, runDir, `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`)

	original, err := os.ReadFile(tasksPath)
	require.NoError(t, err)

	cmd := exec.Command(testBinaryPath)
	cmd.Dir = workDir
	cmd.Env = gralphEnvWithPath(os.Getenv("PATH"), map[string]string{"TERM": "xterm-256color"})

	size := pty.Winsize{Rows: 30, Cols: 100}
	ptmx, err := pty.StartWithSize(cmd, &size)
	if err != nil {
		t.Skipf("cannot start a pty: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
	})

	// The drain must outlive the signal: if nothing reads the pty, gralph
	// blocks writing its exit output and looks hung.
	out := &syncBuffer{}
	go func() {
		_, _ = io.Copy(out, ptmx)
	}()

	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
	}()

	waitForOutput(t, out, "Run folder", 10*time.Second)

	require.NoError(t, cmd.Process.Signal(sig), "send %s to gralph", name)

	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		require.FailNow(t, fmt.Sprintf("gralph did not exit after %s\noutput:\n%s", name, out.String()))
	}

	output := out.String()
	assert.Equal(t, 1, cmd.ProcessState.ExitCode(), "output:\n%s", output)
	assert.Contains(t, output, "setup cancelled")
	assert.NotContains(t, output, "panic:")

	after, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after), "expected tasks.yaml to be byte-for-byte unchanged")
}

// waitForOutput polls until the pty output contains want, failing the test
// after timeout.
func waitForOutput(t *testing.T, out *syncBuffer, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			require.FailNow(t, fmt.Sprintf("output never contained %q within %v\noutput:\n%s", want, timeout, out.String()))
		}

		time.Sleep(25 * time.Millisecond)
	}
}
