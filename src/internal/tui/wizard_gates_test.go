package tui

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

func TestGateListOps(t *testing.T) {
	a, b, c := tasks.Gate{Cmd: "a"}, tasks.Gate{Cmd: "b"}, tasks.Gate{Cmd: "c"}

	added := addGate([]tasks.Gate{a}, b)
	assert.Equal(t, []tasks.Gate{a, b}, added)

	edited := replaceGate([]tasks.Gate{a, b, c}, 1, tasks.Gate{Cmd: "B", Timeout: "5m"})
	assert.Equal(t, []tasks.Gate{a, {Cmd: "B", Timeout: "5m"}, c}, edited)

	deleted := deleteGate([]tasks.Gate{a, b, c}, 0)
	assert.Equal(t, []tasks.Gate{b, c}, deleted)

	original := []tasks.Gate{a, b}
	_ = deleteGate(original, 0)
	assert.Equal(t, []tasks.Gate{a, b}, original, "editing must not change the file's list until Start")
}

// Spare capacity lets append write into the caller's backing array, and
// replace must not write through to it either.
func TestGateListOps_NeverAlias(t *testing.T) {
	a, b := tasks.Gate{Cmd: "a"}, tasks.Gate{Cmd: "b"}

	roomy := make([]tasks.Gate, 1, 4)
	roomy[0] = a
	_ = addGate(roomy, b)
	assert.Equal(t, []tasks.Gate{a, {}}, roomy[:2], "add must not write into the caller's spare capacity")

	original := []tasks.Gate{a, b}
	_ = replaceGate(original, 0, tasks.Gate{Cmd: "x"})
	assert.Equal(t, []tasks.Gate{a, b}, original)
}

func TestCheckGateCmd(t *testing.T) {
	assert.EqualError(t, checkGateCmd("  "), "a gate needs a command")
	assert.NoError(t, checkGateCmd("make test"))
}

func TestCheckGateTimeoutField(t *testing.T) {
	assert.NoError(t, checkGateTimeoutField(""), "a gate's own timeout is optional")
	assert.NoError(t, checkGateTimeoutField("10m"))
	assert.Error(t, checkGateTimeoutField("10"))
}

type editResult struct {
	gates []tasks.Gate
	err   error
}

// startEditGates runs editGates headless on gates and returns the input
// pipe's writer and a channel with its result.
func startEditGates(t *testing.T, gates []tasks.Gate) (*io.PipeWriter, <-chan editResult) {
	t.Helper()
	return startEditGatesCtx(t, context.Background(), gates)
}

func startEditGatesCtx(t *testing.T, ctx context.Context, gates []tasks.Gate) (*io.PipeWriter, <-chan editResult) {
	t.Helper()
	in, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	done := make(chan editResult, 1)
	go func() {
		got, err := editGates(ctx, gates, tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24))
		done <- editResult{got, err}
	}()
	return w, done
}

func waitEditGates(t *testing.T, done <-chan editResult) editResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("editGates did not return")
		return editResult{}
	}
}

func TestEditGates_DoneReturnsEmptyList(t *testing.T) {
	w, done := startEditGates(t, nil)

	// The options are Add a gate, then Done.
	_, err := io.WriteString(w, "j\r")
	require.NoError(t, err)

	r := waitEditGates(t, done)
	require.NoError(t, r.err)
	assert.Empty(t, r.gates)
	assert.NotNil(t, r.gates, "Done on no gates is a decision, saved as gates: []")
}

func TestEditGates_EscCancels(t *testing.T) {
	w, done := startEditGates(t, nil)

	_, err := io.WriteString(w, "\x1b")
	require.NoError(t, err)

	r := waitEditGates(t, done)
	assert.True(t, errors.Is(r.err, ErrCancelled), "got %v", r.err)
}

func TestEditGates_DoneKeepsGivenList(t *testing.T) {
	given := []tasks.Gate{{Cmd: "make test", Timeout: "5m"}}
	w, done := startEditGates(t, given)

	// The options are the gate, Add a gate, then Done.
	_, err := io.WriteString(w, "jj\r")
	require.NoError(t, err)

	r := waitEditGates(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, given, r.gates)
}

func TestEditGates_CancelledContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, done := startEditGatesCtx(t, ctx, nil)

	cancel()

	r := waitEditGates(t, done)
	assert.True(t, errors.Is(r.err, ErrCancelled), "got %v", r.err)
}

func TestEditGates_AlreadyCancelledContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, done := startEditGatesCtx(t, ctx, nil)

	r := waitEditGates(t, done)
	assert.True(t, errors.Is(r.err, ErrCancelled), "got %v", r.err)
}

// A multi-line gate must come out of the edit form as it went in, so editing
// only the timeout does not flatten the command. The value is written back
// when the field loses focus, which is where a single-line input flattens it.
func TestGateCmdField_KeepsNewlines(t *testing.T) {
	g := tasks.Gate{Cmd: "cd x\nmake test", Timeout: "5m"}
	field := gateCmdField(&g)

	field.Focus()
	field.Blur()

	assert.Equal(t, "cd x\nmake test", g.Cmd)
	assert.Equal(t, "5m", g.Timeout)
}
