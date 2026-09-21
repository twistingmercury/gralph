//go:build darwin || linux

package looper

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
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
	if process == nil {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}
