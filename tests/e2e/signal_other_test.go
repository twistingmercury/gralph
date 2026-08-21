//go:build !unix

package e2e

import "os"

func signalTestsSupported() bool {
	return false
}

func interruptSignal() os.Signal {
	return os.Interrupt
}

func terminateSignal() os.Signal {
	return os.Interrupt
}

func processIsRunning(int) bool {
	return false
}
