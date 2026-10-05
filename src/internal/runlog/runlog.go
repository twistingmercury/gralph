// Package runlog writes the record of a run that --log-dir asks for
// (ADR-016): a ledger of what ran and when, and each task's activity. It only
// listens to the looper's events; the loop knows nothing about it.
package runlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

// The record holds Claude's text and whatever a gate or git printed, so it
// is private to the user.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

const ledgerName = "run.jsonl"

const appendOnly = os.O_CREATE | os.O_WRONLY | os.O_APPEND

// Info is what the run_started line says about the run.
type Info struct {
	Version         string
	TasksFile       string
	Permissions     string // "sandbox" or "skip"
	SandboxSettings string // the settings file's path, "" when not passed
	GateTimeout     string // the --gate-timeout value, "" when not passed
	Commit          bool
}

// Log is one run's record. A nil *Log records nothing, so a run without
// --log-dir needs no checks of its own.
type Log struct {
	// mu makes Record safe for the looper, which reports from more than one
	// goroutine.
	mu  sync.Mutex
	now func() time.Time
	// root confines every file to the run folder.
	root   *os.Root
	ledger *os.File
	enc    *json.Encoder
	// detail is the running task's activity file; nil between tasks.
	detail *os.File
	// failed says the last finished task failed, which is what tells a
	// failed run from one stopped for any other reason.
	failed bool
}

// RunDir names the folder for a run that started at start: its local time,
// so the name matches the clock the user reads.
func RunDir(logDir string, start time.Time) string {
	name := start.Format("20060102T150405")
	return filepath.Join(logDir, name)
}

// Open creates runDir, which must not exist, and starts the ledger with the
// run_started line.
func Open(runDir string, info Info) (*Log, error) {
	return open(runDir, info, time.Now)
}

// open is Open with the clock given, for tests.
func open(runDir string, info Info, now func() time.Time) (*Log, error) {
	parent := filepath.Dir(runDir)
	if err := os.MkdirAll(parent, dirMode); err != nil {
		return nil, err
	}

	// Mkdir, not MkdirAll: an existing run folder holds another run's record.
	if err := os.Mkdir(runDir, dirMode); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(runDir)
	if err != nil {
		return nil, err
	}

	l := &Log{now: now, root: root}
	if err := l.start(info); err != nil {
		_ = l.closeFiles()
		return nil, err
	}

	return l, nil
}

// start creates the ledger and writes its first line.
func (l *Log) start(info Info) error {
	ledger, err := l.root.OpenFile(ledgerName, appendOnly, fileMode)
	if err != nil {
		return err
	}

	l.ledger = ledger
	l.enc = json.NewEncoder(ledger)
	// People read the ledger too: a gate's "a > b && c" stays as written
	// instead of becoming \u003e and \u0026.
	l.enc.SetEscapeHTML(false)

	st := l.stamp("run_started")
	return l.enc.Encode(runStarted{
		stamp:           st,
		Version:         info.Version,
		TasksFile:       info.TasksFile,
		Permissions:     info.Permissions,
		SandboxSettings: info.SandboxSettings,
		GateTimeout:     info.GateTimeout,
		Commit:          info.Commit,
	})
}

// Record writes what e adds to the record. An error means the record is no
// longer complete; the caller stops the run.
func (l *Log) Record(e looper.Event) error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	switch e.Kind {
	case looper.TaskStarted:
		return l.taskStarted(e)
	case looper.Activity:
		return l.activity(e)
	case looper.SessionFinished:
		return l.stepFinished("session_finished", e)
	case looper.GateFinished:
		return l.gateFinished(e)
	case looper.Committed:
		return l.committed(e)
	case looper.TaskFinished:
		return l.taskFinished(e)
	case looper.RunDone:
		return l.runDone(e)
	}

	return nil
}

// Close closes the record's files.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeFiles()
}

func (l *Log) closeFiles() error {
	var detailErr, ledgerErr error
	if l.detail != nil {
		detailErr = l.detail.Close()
		l.detail = nil
	}

	if l.ledger != nil {
		ledgerErr = l.ledger.Close()
	}

	rootErr := l.root.Close()
	return errors.Join(detailErr, ledgerErr, rootErr)
}

func (l *Log) stamp(event string) stamp {
	at := l.now()
	return stamp{Time: at.Format(time.RFC3339), Event: event}
}

func (l *Log) taskStarted(e looper.Event) error {
	name := fmt.Sprintf("task-%d.log", e.Task.ID)
	detail, err := l.root.OpenFile(name, appendOnly, fileMode)
	if err != nil {
		return err
	}

	l.detail = detail
	l.failed = false
	st := l.stamp("task_started")
	return l.enc.Encode(taskStarted{stamp: st, Task: e.Task.ID, Name: e.Task.Name})
}

func (l *Log) activity(e looper.Event) error {
	at := l.now()
	clock := at.Format("15:04:05")
	_, err := fmt.Fprintf(l.detail, "%s %s\n", clock, e.Line)
	return err
}

func (l *Log) stepFinished(event string, e looper.Event) error {
	st := l.stamp(event)
	took := duration(e.Duration)
	return l.enc.Encode(stepFinished{stamp: st, Task: e.Task.ID, State: e.Task.State, Error: e.Task.Error, Duration: took})
}

func (l *Log) gateFinished(e looper.Event) error {
	st := l.stamp("gate_finished")
	// Only the first line, as everywhere a gate is named.
	cmd, _, _ := strings.Cut(e.Gate.Cmd, "\n")
	result := gateResult(e.Err)
	took := duration(e.Duration)
	return l.enc.Encode(gateFinished{stamp: st, Task: e.Task.ID, Cmd: cmd, Result: result, Timeout: e.Limit, Duration: took})
}

func gateResult(err error) string {
	switch {
	case err == nil:
		return "passed"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed_out"
	}

	return "failed"
}

func (l *Log) committed(e looper.Event) error {
	st := l.stamp("committed")
	return l.enc.Encode(committed{stamp: st, Task: e.Task.ID, Hash: e.Hash})
}

func (l *Log) taskFinished(e looper.Event) error {
	l.failed = e.Task.State == tasks.FailedState
	if err := l.stepFinished("task_finished", e); err != nil {
		return err
	}

	detail := l.detail
	l.detail = nil
	return detail.Close()
}

func (l *Log) runDone(e looper.Event) error {
	st := l.stamp("run_finished")
	took := duration(e.Duration)
	done := runFinished{stamp: st, Result: "completed", Duration: took}
	if e.Err != nil {
		done.Result = l.endedBy()
		done.Error = e.Err.Error()
	}

	return l.enc.Encode(done)
}

// endedBy names why a run ended with an error. The looper's error does not
// say whether a task failed or the run was cut short, but the last
// task_finished does: a stopped task never gets one.
func (l *Log) endedBy() string {
	if l.failed {
		return "failed"
	}

	return "stopped"
}

// duration rounds to the millisecond: finer than that is noise in a ledger.
func duration(d time.Duration) string {
	rounded := d.Round(time.Millisecond)
	return rounded.String()
}
