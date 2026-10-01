package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/looper"
)

// forwarded runs events through a forwarder and returns what the view was
// sent, what observe saw, and how often the run was cancelled.
type forwarded struct {
	sent     []looper.Event
	observed []looper.Event
	cancels  int
	failOn   looper.EventKind
	failWith error
}

func (f *forwarded) send(msg tea.Msg) {
	f.sent = append(f.sent, msg.(looper.Event))
}

func (f *forwarded) observe(e looper.Event) error {
	f.observed = append(f.observed, e)
	if f.failWith != nil && e.Kind == f.failOn {
		return f.failWith
	}

	return nil
}

func (f *forwarded) cancel() { f.cancels++ }

func TestForwarder_NoObserverSendsEventsUnchanged(t *testing.T) {
	var f forwarded
	fwd := &forwarder{send: f.send, cancel: f.cancel}
	runErr := errors.New("boom")

	fwd.report(looper.Event{Kind: looper.TaskStarted})
	fwd.report(looper.Event{Kind: looper.RunDone, Err: runErr})

	require.Len(t, f.sent, 2)
	assert.Equal(t, runErr, f.sent[1].Err)
	assert.Zero(t, f.cancels)
}

func TestForwarder_ObserverSeesEveryEventBeforeTheView(t *testing.T) {
	var f forwarded
	fwd := &forwarder{observe: f.observe, send: f.send, cancel: f.cancel}

	fwd.report(looper.Event{Kind: looper.TaskStarted})
	fwd.report(looper.Event{Kind: looper.SessionFinished})
	fwd.report(looper.Event{Kind: looper.RunDone})

	assert.Equal(t, f.sent, f.observed)
	assert.Len(t, f.observed, 3)
	assert.NoError(t, f.sent[2].Err)
	assert.Zero(t, f.cancels)
}

func TestForwarder_ObserverErrorCancelsOnceAndEndsTheRunWithIt(t *testing.T) {
	f := forwarded{failOn: looper.Activity, failWith: errors.New("disk full")}
	fwd := &forwarder{observe: f.observe, send: f.send, cancel: f.cancel}

	fwd.report(looper.Event{Kind: looper.TaskStarted})
	fwd.report(looper.Event{Kind: looper.Activity, Line: "one"})
	fwd.report(looper.Event{Kind: looper.Activity, Line: "two"})
	fwd.report(looper.Event{Kind: looper.RunDone, Err: errors.New("task 1: First failed: signal: killed")})

	assert.Len(t, f.observed, 2, "observe is not called again after its first error")
	assert.Equal(t, 1, f.cancels)
	require.Len(t, f.sent, 4, "the view still gets every event")
	assert.EqualError(t, f.sent[3].Err, "log: disk full")
}
