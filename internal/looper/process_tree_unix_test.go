//go:build darwin || linux

package looper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestTerminateProcessTree proves that a descendant of the claude child
// (i.e. a process the child itself spawns) is killed when the run context is
// canceled. It does not require a real claude binary: it re-execs the test
// binary itself as a "parent" helper that spawns a "descendant" helper.
func TestTerminateProcessTree(t *testing.T) {
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	descendantReadyPath := t.TempDir() + "/descendant-ready"

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, testExecutable,
		"-test.run=^TestProcessTreeHelper$",
		"--",
		"--process-tree-helper=parent",
		"--descendant-ready="+descendantReadyPath,
	)
	configureProcessTree(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Wait()
	}()

	descendantPID := waitForProcessTreePID(t, descendantReadyPath, waitErrCh)
	t.Cleanup(func() {
		_ = syscall.Kill(descendantPID, syscall.SIGKILL)
	})
	cancel()

	select {
	case err := <-waitErrCh:
		if err == nil {
			t.Fatal("cmd.Wait() error = nil, want an error from cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after cancellation")
	}

	deadline := time.Now().Add(2 * time.Second)
	for unixProcessIsRunning(descendantPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if unixProcessIsRunning(descendantPID) {
		t.Fatalf("descendant process %d remained running after cancellation", descendantPID)
	}
}

// TestProcessTreeHelper is not a real test; it is re-exec'd by
// TestTerminateProcessTree as a "parent" or "descendant" helper process. When
// run normally by `go test`, --process-tree-helper is unset and it returns
// immediately.
func TestProcessTreeHelper(t *testing.T) {
	mode := processTreeHelperArg("--process-tree-helper=")
	switch mode {
	case "":
		return
	case "descendant":
		for {
			time.Sleep(time.Hour)
		}
	case "parent":
		readyPath := processTreeHelperArg("--descendant-ready=")
		if readyPath == "" {
			os.Exit(2)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$", "--", "--process-tree-helper=descendant")
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(readyPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			_ = cmd.Process.Kill()
			os.Exit(4)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(5)
	}
}

func processTreeHelperArg(prefix string) string {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func waitForProcessTreePID(t *testing.T, path string, waitErrCh <-chan error) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("parse descendant PID %q: %v", data, err)
			}
			return pid
		}
		if !os.IsNotExist(err) {
			t.Fatalf("read descendant ready file: %v", err)
		}
		select {
		case err := <-waitErrCh:
			t.Fatalf("process exited before descendant was ready: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant did not become ready")
	return 0
}

func unixProcessIsRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	closingParen := strings.LastIndexByte(string(stat), ')')
	if closingParen == -1 {
		return true
	}
	fields := strings.Fields(string(stat[closingParen+1:]))
	return len(fields) == 0 || (fields[0] != "Z" && fields[0] != "X")
}
