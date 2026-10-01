package tui

import (
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/twistingmercury/gralph/internal/looper"
)

// forwarder is the loop's report hook: it shows each event to observe, when
// there is one, and then sends it to the view. observe is how a run is
// recorded (ADR-016); the view never learns it exists.
type forwarder struct {
	observe func(looper.Event) error
	send    func(tea.Msg)
	cancel  func()

	// mu guards err: report is called from more than one goroutine.
	mu sync.Mutex
	// err is observe's first error, worded for the run's final status.
	err error
}

func (f *forwarder) report(e looper.Event) {
	seen := f.observed(e)
	f.send(seen)
}

// observed hands e to observe and returns the event the view should get. It
// is e, except that once observe has failed the run ends with that failure
// instead of the cancellation it caused.
func (f *forwarder) observed(e looper.Event) looper.Event {
	if f.observe == nil {
		return e
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.observeOnce(e)
	if f.err != nil && e.Kind == looper.RunDone {
		e.Err = f.err
	}

	return e
}

// observeOnce stops the run on observe's first error and never calls it
// again: a record with a hole in it is what observe exists to prevent, and a
// failing writer has nothing more to add.
func (f *forwarder) observeOnce(e looper.Event) {
	if f.err != nil {
		return
	}

	err := f.observe(e)
	if err == nil {
		return
	}

	f.err = fmt.Errorf("log: %w", err)
	f.cancel()
}
