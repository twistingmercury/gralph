//go:build linux

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
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

// ptyRun is a gralph process running under a pty.
type ptyRun struct {
	cmd *exec.Cmd
	// out holds everything gralph has written to the terminal so far.
	out *syncBuffer
	// exited receives the exit code once the process ends.
	exited <-chan int
	// ptmx is the terminal's master side; write to it to send keys.
	ptmx *os.File
}

// startPty starts the gralph binary under a pty and skips the test when no
// pty is available. The cleanup kills the process and closes the pty.
func startPty(t *testing.T, dir string, env []string, args ...string) *ptyRun {
	t.Helper()

	cmd := exec.Command(testBinaryPath, args...)
	cmd.Dir = dir
	cmd.Env = env

	size := pty.Winsize{Rows: 30, Cols: 100}
	ptmx, err := pty.StartWithSize(cmd, &size)
	if err != nil {
		t.Skipf("cannot start a pty: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
	})

	// The drain must outlive any signal: if nothing reads the pty, gralph
	// blocks writing its exit output and looks hung.
	out := &syncBuffer{}
	go func() {
		_, _ = io.Copy(out, ptmx)
	}()

	exited := make(chan int, 1)
	go func() {
		_ = cmd.Wait()
		exited <- cmd.ProcessState.ExitCode()
	}()

	return &ptyRun{cmd: cmd, out: out, exited: exited, ptmx: ptmx}
}

// waitForOutput polls until the pty output contains want, failing the test
// after timeout.
func waitForOutput(t *testing.T, out *syncBuffer, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			got := out.String()
			msg := fmt.Sprintf("output never contained %q within %v\noutput:\n%s", want, timeout, got)
			require.FailNow(t, msg)
		}

		time.Sleep(25 * time.Millisecond)
	}
}
