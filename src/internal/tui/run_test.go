package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

// fakeClaudeDir holds the fake claude built once in TestMain.
var fakeClaudeDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gralph-tui-fakeclaude-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: create temp dir:", err)
		os.Exit(1)
	}

	build := exec.Command("go", "build", "-o", filepath.Join(dir, "claude"), "../looper/testdata/fakeclaude")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: build failed:", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	fakeClaudeDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type runResult struct {
	code    int
	summary string
	err     error
}

// startRun runs Run headless over the tasks in tasksPath and returns the
// input pipe's writer and a channel with Run's result.
func startRun(t *testing.T, ctx context.Context, tasksPath string) (*io.PipeWriter, <-chan runResult) {
	t.Helper()
	return startRunIn(t, ctx, tasksPath, nil)
}

// startRunIn is startRun for a run that commits into repo; nil means no git.
func startRunIn(t *testing.T, ctx context.Context, tasksPath string, repo *looper.Repo) (*io.PipeWriter, <-chan runResult) {
	t.Helper()
	return startRunWith(t, ctx, tasksPath, repo, nil)
}

// startRunWith is startRunIn with an observe hook; nil means none.
func startRunWith(t *testing.T, ctx context.Context, tasksPath string, repo *looper.Repo, observe func(looper.Event) error) (*io.PipeWriter, <-chan runResult) {
	t.Helper()
	t.Setenv("PATH", fakeClaudeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	tl, err := tasks.ParseTasks(data)
	require.NoError(t, err)

	in, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	done := make(chan runResult, 1)
	go func() {
		code, summary, err := Run(ctx, "prompt", &tl, tasksPath, "", looper.BypassArgs(), repo, observe,
			tea.WithInput(in), tea.WithOutput(io.Discard), tea.WithWindowSize(120, 30))
		done <- runResult{code, summary, err}
	}()
	return w, done
}

// pressQUntilClosed keeps pressing q until the view closes: a q before
// RunDone opens the stop prompt and the next one dismisses it. Closing w at
// cleanup ends it.
func pressQUntilClosed(w *io.PipeWriter) {
	for {
		if _, err := w.Write([]byte("q")); err != nil {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func waitRun(t *testing.T, done <-chan runResult) runResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return runResult{}
	}
}

func TestRun_TwoTasksCompleteThenQuit(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte(
		"tasks:\n  - {id: 1, name: First, prompt: p1}\n  - {id: 2, name: Second, prompt: p2}\n"), 0o600))

	w, done := startRun(t, context.Background(), tasksPath)

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(tasksPath)
		if err != nil {
			return false
		}

		tl, err := tasks.ParseTasks(data)
		return err == nil && tl.Tasks[0].State == tasks.CompletedState && tl.Tasks[1].State == tasks.CompletedState
	}, 10*time.Second, 10*time.Millisecond, "tasks never completed")

	// The file is saved before the model sees RunDone, so an early q opens the
	// stop prompt and the next dismisses it; keep pressing q until Run returns.
	// Closing w at cleanup ends the writer.
	go func() {
		for {
			if _, err := w.Write([]byte("q")); err != nil {
				return
			}

			time.Sleep(50 * time.Millisecond)
		}
	}()
	r := waitRun(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, 0, r.code)
	assert.Equal(t, "All tasks completed", r.summary)
}

func TestRun_OutsideCancelStopsRun(t *testing.T) {
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_BLOCK", "1")
	t.Setenv("FAKE_CLAUDE_READY", readyPath)
	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_, done := startRun(t, ctx, tasksPath)

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "fake claude never became ready")

	cancel()

	r := waitRun(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, 1, r.code)
	assert.Equal(t, "Run stopped by signal", r.summary)
	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got))
}

// The model tests only show that y calls whatever cancel the model was given;
// this one shows Run gives it the cancel that stops the real session. The
// context is never cancelled by the test itself, so only the keys can end the
// run; the cleanup cancel is there to stop the fake claude if they do not.
func TestRun_ConfirmedStopCancelsRun(t *testing.T) {
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_BLOCK", "1")
	t.Setenv("FAKE_CLAUDE_READY", readyPath)
	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w, done := startRun(t, ctx, tasksPath)

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "fake claude never became ready")

	_, err := w.Write([]byte("q"))
	require.NoError(t, err)
	_, err = w.Write([]byte("y"))
	require.NoError(t, err)

	r := waitRun(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, 1, r.code)
	assert.Equal(t, "Run stopped by user", r.summary)
	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got))
}

// initGitRepo makes a repository with one commit that ignores run/, where the
// task file lives, and moves the test into it so a gate's work lands there.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("run/\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "run"), 0o750))
	for _, args := range [][]string{{"init", "-q"}, {"add", ".gitignore"}, {"commit", "-q", "-m", "init"}} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	return dir
}

func TestRun_CommitsEachTaskIntoTheRepo(t *testing.T) {
	dir := initGitRepo(t)
	tasksPath := filepath.Join(dir, "run", "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte(
		"tasks:\n  - id: 1\n    name: First\n    prompt: p1\n    gates:\n      - cmd: echo work > one.txt\n"), 0o600))
	repo, err := looper.OpenRepo(tasksPath)
	require.NoError(t, err)
	require.NotNil(t, repo)

	w, done := startRunIn(t, context.Background(), tasksPath, repo)

	// Keep pressing q until Run returns, as TestRun_TwoTasksCompleteThenQuit
	// does; the view only closes after the run, commit included, is done.
	go func() {
		for {
			if _, err := w.Write([]byte("q")); err != nil {
				return
			}

			time.Sleep(50 * time.Millisecond)
		}
	}()
	r := waitRun(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, 0, r.code)

	out, err := exec.Command("git", "log", "--format=%s").Output()
	require.NoError(t, err)
	assert.Equal(t, "First\ninit\n", string(out))
}

// kindLog is an observe hook that keeps the kinds it saw; observe is called
// from more than one goroutine.
type kindLog struct {
	mu    sync.Mutex
	kinds []looper.EventKind
}

func (k *kindLog) observe(e looper.Event) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.kinds = append(k.kinds, e.Kind)
	return nil
}

func (k *kindLog) snapshot() []looper.EventKind {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]looper.EventKind(nil), k.kinds...)
}

func TestRun_ObserveSeesTheWholeRun(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte("tasks:\n  - {id: 1, name: First, prompt: p1}\n"), 0o600))

	var seen kindLog
	w, done := startRunWith(t, context.Background(), tasksPath, nil, seen.observe)
	go pressQUntilClosed(w)

	r := waitRun(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, 0, r.code)
	assert.Equal(t, "All tasks completed", r.summary)
	want := []looper.EventKind{looper.TaskStarted, looper.Activity, looper.SessionFinished, looper.TaskFinished, looper.RunDone}
	assert.Equal(t, want, seen.snapshot())
}

func TestRun_ObserveErrorStopsTheRun(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))

	var calls atomic.Int32
	failing := func(looper.Event) error {
		calls.Add(1)
		return errors.New("disk full")
	}
	w, done := startRunWith(t, context.Background(), tasksPath, nil, failing)
	go pressQUntilClosed(w)

	r := waitRun(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, 1, r.code)
	assert.Equal(t, "Run stopped: log: disk full", r.summary)
	assert.Equal(t, int32(1), calls.Load(), "observe is not called again after it failed")
	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got), "the stopped task stays as it was")
}
