package looper

import (
	"time"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// EventKind says what an Event reports.
type EventKind int

// The last three kinds exist for listeners that keep a record of the run
// (ADR-016); they are appended so the first four keep their values.
const (
	TaskStarted EventKind = iota
	Activity
	TaskFinished
	RunDone
	SessionFinished
	GateFinished
	Committed
)

// Event is one progress report from Run to its report hook.
type Event struct {
	Kind EventKind
	// Task is a copy of the task, on every kind but RunDone. SessionFinished
	// and TaskFinished carry the state and error known at that point.
	Task tasks.Task
	Line string // Activity
	// Err is the run's error on RunDone and the task's failure on
	// TaskFinished. On GateFinished nil means the gate passed and
	// context.DeadlineExceeded means it hit its limit.
	Err      error
	Gate     tasks.Gate    // GateFinished
	Limit    string        // GateFinished: the timeout the gate ran under, as written
	Hash     string        // Committed: the new commit
	Duration time.Duration // SessionFinished, GateFinished, TaskFinished, RunDone
}
