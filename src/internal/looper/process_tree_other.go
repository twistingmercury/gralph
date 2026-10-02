//go:build !unix

package looper

import "os/exec"

// Off Unix there are no process groups to signal, so a cancelled claude
// session, gate, or git stops only the direct child that exec.CommandContext
// kills by default. Descendants it spawned keep running. A killed git can also
// leave .git/index.lock behind, since nothing asks it to exit first.

func configureProcessTree(*exec.Cmd) {}

func configureGitProcessTree(*exec.Cmd) {}
