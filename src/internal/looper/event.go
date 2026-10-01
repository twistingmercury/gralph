package looper

import "github.com/twistingmercury/gralph/internal/tasks"

// EventKind says what an Event reports.
type EventKind int

const (
	TaskStarted EventKind = iota
	Activity
	TaskFinished
	RunDone
)

// Event is one progress report from Run to its report hook.
type Event struct {
	Kind EventKind  // TaskStarted, Activity, TaskFinished, RunDone
	Task tasks.Task // TaskStarted, TaskFinished: a copy of the task
	Line string     // Activity
	Err  error      // TaskFinished (failed), RunDone
}
