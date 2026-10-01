package looper

import (
	"context"
	"errors"
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
func runGates(ctx context.Context, task tasks.Task, override string, report func(Event)) (state, errMsg string, err error) {
	for _, gate := range task.Gates {
		limit, _ := gateLimit(gate, override)
		runErr := runGate(ctx, task, gate, limit, report)
		if runErr == nil {
			continue
		}

		if ctx.Err() != nil {
			gateName := firstLine(gate.Cmd)
			return "", "", fmt.Errorf("task %d: %s failed: gate %q: %w", task.ID, task.Name, gateName, runErr)
		}

		return tasks.FailedState, gateFailure(gate, limit, runErr), nil
	}

	return tasks.CompletedState, "", nil
}

// firstLine is the part of a multi-line value that messages and activity
// lines quote, so the value cannot push the rest off a one-line display.
func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}

// defaultGateTimeout limits every gate that neither the flag nor the file
// gives a timeout; there is deliberately no way to run a gate without a limit.
const defaultGateTimeout = "10m"

// Where a gate's effective timeout came from.
const (
	limitFromFlag    = "flag"
	limitFromGate    = "gate"
	limitFromDefault = "default"
)

// gateLimit picks the timeout that applies to gate, as written where it came
// from: the --gate-timeout override beats the gate's own timeout, which beats
// the default. The text stays as written so a failure message can repeat it.
func gateLimit(gate tasks.Gate, override string) (limit, source string) {
	switch {
	case override != "":
		return override, limitFromFlag
	case gate.Timeout != "":
		return gate.Timeout, limitFromGate
	}

	return defaultGateTimeout, limitFromDefault
}

// gateFailure words a gate's failure. A timeout reads differently from a
// non-zero exit because "signal: killed" would hide that the gate was cut off.
func gateFailure(gate tasks.Gate, limit string, runErr error) string {
	gateName := firstLine(gate.Cmd)
	if errors.Is(runErr, context.DeadlineExceeded) {
		return fmt.Sprintf("gate %q timed out after %s", gateName, limit)
	}

	return fmt.Sprintf("gate %q failed: %s", gateName, runErr)
}

// runGate runs one gate through `sh -c` so the command can use shell syntax
// (pipes, &&, redirects). gosec's G204 finding is accepted on purpose: running
// a command from the task file is the feature (ADR-013). The gate gets no
// stdin and its own process group, like claude, so a cancel kills everything
// it started. Every gate runs under its limit; when that deadline, not the
// run's ctx, ended the gate, the error is context.DeadlineExceeded.
func runGate(ctx context.Context, task tasks.Task, gate tasks.Gate, limit string, report func(Event)) error {
	d, err := tasks.ParseTimeout(limit)
	if err != nil {
		return err
	}

	gateCtx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	err = runGateCmd(gateCtx, task, gate, report)
	if err == nil || ctx.Err() != nil {
		return err
	}

	gateCtxErr := gateCtx.Err()
	if errors.Is(gateCtxErr, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}

	return err
}

func runGateCmd(ctx context.Context, task tasks.Task, gate tasks.Gate, report func(Event)) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", gate.Cmd) // #nosec G204 -- a gate is a command from the task file, run by design (ADR-013); suppression approved by the owner
	configureProcessTree(cmd)

	if report != nil {
		// Only the first line, so a multi-line command stays one activity line.
		report(Event{Kind: Activity, Task: task, Line: "→ gate " + firstLine(gate.Cmd)})
		return runCmdStream(cmd, task, report)
	}

	fmt.Printf("gate: %s\n", gate.Cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runCmdStream runs cmd with each stdout and stderr line reported as an
// Activity event, so nothing reaches gralph's own stdout or stderr. Both
// pipes are drained before Wait, as runTaskStream does. Gates and the commit
// step share it.
func runCmdStream(cmd *exec.Cmd, task tasks.Task, report func(Event)) error {
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
