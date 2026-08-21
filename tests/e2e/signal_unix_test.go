//go:build unix

package e2e

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

func signalTestsSupported() bool {
	return true
}

func interruptSignal() os.Signal {
	return os.Interrupt
}

func terminateSignal() os.Signal {
	return syscall.SIGTERM
}

func processIsRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	if runtime.GOOS != "linux" {
		return true
	}

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) // #nosec G304 -- pid is read from the test-owned fake agent
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
