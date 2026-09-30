package looper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// runGates runs task's gates in file order after its session completed and
// returns the task's final outcome: completed when every gate exits zero,
// failed at the first gate that does not. err is non-nil only when ctx was
// cancelled, which must leave the task's state and the tasks file untouched.
func runGates(ctx context.Context, task tasks.Task, report func(Event)) (state, errMsg string, err error) {
	for _, gate := range task.Gates {
		runErr := runGate(ctx, task, gate, report)
		if runErr == nil {
			continue
		}

		if ctx.Err() != nil {
			return "", "", fmt.Errorf("task %d: %s failed: gate %q: %w", task.ID, task.Name, gate.Cmd, runErr)
		}

		return tasks.FailedState, fmt.Sprintf("gate %q failed: %s", gate.Cmd, runErr), nil
	}

	return tasks.CompletedState, "", nil
}

// runGate runs one gate through `sh -c` so the command can use shell syntax
// (pipes, &&, redirects). gosec's G204 finding is accepted on purpose: running
// a command from the task file is the feature (ADR-013). The gate gets no
// stdin and its own process group, like claude, so a cancel kills everything
// it started.
func runGate(ctx context.Context, task tasks.Task, gate tasks.Gate, report func(Event)) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", gate.Cmd) // #nosec G204 -- a gate is a command from the task file, run by design (ADR-013); suppression approved by the owner
	configureProcessTree(cmd)

	if report != nil {
		// Only the first line, so a multi-line command stays one activity line.
		first, _, _ := strings.Cut(gate.Cmd, "\n")
		report(Event{Kind: Activity, Task: task, Line: "→ gate " + first})
		return runGateStream(cmd, task, report)
	}

	fmt.Printf("gate: %s\n", gate.Cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runGateStream runs cmd with each stdout and stderr line reported as an
// Activity event, so nothing reaches gralph's own stdout or stderr. Both
// pipes are drained before Wait, as runTaskStream does.
func runGateStream(cmd *exec.Cmd, task tasks.Task, report func(Event)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	st := &streamTask{task: task, report: report}
	stderrDone := make(chan struct{})
	go st.readStderr(stderr, stderrDone)
	readLines(stdout, st.rawLine)
	<-stderrDone

	return cmd.Wait()
}
