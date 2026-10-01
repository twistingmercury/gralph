package looper

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureProcessTree puts the claude child in its own process group and
// arranges for context cancellation to kill that entire group. Without this,
// exec.CommandContext only signals the direct child, leaving any descendants
// the agent spawns still running after gralph exits.
//
// One consequence: because the child leaves gralph's foreground process
// group, a terminal Ctrl-C is delivered only to gralph, not to the child
// directly. gralph's context cancellation (via signal.NotifyContext) is what
// then kills the group; nothing may rely on the child receiving SIGINT
// itself.
func configureProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return terminateProcessTree(cmd.Process)
	}
}

func terminateProcessTree(process *os.Process) error {
	return signalProcessTree(process, syscall.SIGKILL)
}

// signalProcessTree sends sig to process's whole group. A group that is
// already gone reads as os.ErrProcessDone, which os/exec treats as "nothing to
// cancel".
func signalProcessTree(process *os.Process, sig syscall.Signal) error {
	if process == nil {
		return os.ErrProcessDone
	}

	if err := syscall.Kill(-process.Pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}

		return err
	}

	return nil
}

// gitStopGrace is how long git gets to exit after SIGTERM before its group is
// killed.
const gitStopGrace = 2 * time.Second

// gitStopPoll is how often stopProcessTree checks whether the group is gone.
const gitStopPoll = 10 * time.Millisecond

// configureGitProcessTree is configureProcessTree for git, which differs from
// a gate in how it is stopped: git removes its .git/index.lock when it gets
// SIGTERM, but a SIGKILLed git leaves the lock behind and every later git
// command in the user's repository fails until someone deletes it. So a cancel
// asks first and kills only after gitStopGrace.
func configureGitProcessTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return stopProcessTree(cmd.Process, gitStopGrace)
	}

	// Backstop for a hook that detached from the group yet still holds git's
	// output pipes open; the group kill below cannot reach it.
	cmd.WaitDelay = gitStopGrace
}

// stopProcessTree sends the group SIGTERM, then SIGKILL if any member is
// still alive after grace. It runs inside cmd.Cancel, where os/exec calls it
// once and Wait does not return until it does, so it starts no goroutine or
// timer that could outlive the command.
func stopProcessTree(process *os.Process, grace time.Duration) error {
	if err := signalProcessTree(process, syscall.SIGTERM); err != nil {
		return err
	}

	for deadline := time.Now().Add(grace); time.Now().Before(deadline); {
		time.Sleep(gitStopPoll)

		// Signal 0 only probes. The group leader is reaped by Wait, which runs
		// concurrently, so a zombie does not keep the probe succeeding.
		probeErr := signalProcessTree(process, 0)
		if errors.Is(probeErr, os.ErrProcessDone) {
			return nil
		}
	}

	return terminateProcessTree(process)
}
