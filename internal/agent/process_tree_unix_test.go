//go:build darwin || linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type processTreeRunResult struct {
	capture CommandOutput
	err     error
}

func TestTerminateProcessTree(t *testing.T) {
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	descendantReadyPath := t.TempDir() + "/descendant-ready"
	runner, err := NewCommandRunner(AgentCommand{
		Executable: testExecutable,
		Args: []string{
			"-test.run=^TestProcessTreeHelper$",
			"--",
			"--process-tree-helper=parent",
			"--descendant-ready=" + descendantReadyPath,
		},
		PromptMode: PromptModeStdin,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	resultCh := make(chan processTreeRunResult, 1)
	go func() {
		capture, runErr := runner.Run(ctx, "prompt")
		resultCh <- processTreeRunResult{capture: capture, err: runErr}
	}()

	descendantPID := waitForProcessTreePID(t, descendantReadyPath, resultCh)
	t.Cleanup(func() {
		_ = syscall.Kill(descendantPID, syscall.SIGKILL)
	})
	cancel()

	select {
	case result := <-resultCh:
		t.Cleanup(func() { _ = result.capture.Cleanup() })
		if !errors.Is(result.err, ErrCanceled) || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("Run() error = %v, want cancellation categories", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not return after cancellation")
	}

	deadline := time.Now().Add(2 * time.Second)
	for unixProcessIsRunning(descendantPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if unixProcessIsRunning(descendantPID) {
		t.Fatalf("descendant process %d remained running after cancellation", descendantPID)
	}
}

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
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$", "--", "--process-tree-helper=descendant") // #nosec G204 -- current test binary and fixed helper arguments
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

func waitForProcessTreePID(t *testing.T, path string, resultCh <-chan processTreeRunResult) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path) // #nosec G304 -- path is created by the test
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
		case result := <-resultCh:
			_ = result.capture.Cleanup()
			t.Fatalf("runner exited before descendant was ready: %v", result.err)
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
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) // #nosec G304 -- pid belongs to the test helper
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
