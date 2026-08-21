//go:build !darwin && !linux

package agent

import "os/exec"

// configureProcessTree leaves exec.CommandContext's direct-process
// cancellation in place outside the supported Linux and macOS targets.
// Windows descendant termination requires assigning the agent to a Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; that strategy is not yet implemented, so
// process-tree termination is not supported on Windows.
func configureProcessTree(*exec.Cmd) {}
