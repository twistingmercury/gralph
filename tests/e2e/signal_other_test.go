//go:build !darwin && !linux

package e2e

import "testing"

// TestClaudeLoop_SignalHandling is a placeholder on platforms other than
// darwin/linux. gralph's process-tree termination (Setpgid + whole-group
// SIGKILL) is only implemented for darwin/linux
// (internal/looper/process_tree_unix.go); on other platforms only the
// direct child is targeted (process_tree_other.go), so there is no
// descendant-cleanup contract this suite can honestly assert here.
func TestClaudeLoop_SignalHandling(t *testing.T) {
	t.Skip("process-tree (descendant) cleanup on signal is only implemented for darwin/linux; see internal/looper/process_tree_unix.go vs process_tree_other.go")
}
