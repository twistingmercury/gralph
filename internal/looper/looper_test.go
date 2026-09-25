package looper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// recordSeparator must match the constant of the same name in
// testdata/fakeclaude/main.go.
const recordSeparator = "\x00---FAKE-CLAUDE-RECORD-SEPARATOR---\x00"

const validTasksYAML = `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
`

// fakeClaudeDir holds the directory containing the compiled fake claude
// binary for the lifetime of the test binary; it is built once in TestMain
// rather than per test.
var fakeClaudeDir string

// TestMain builds the fake claude fixture (testdata/fakeclaude) once before
// running any test in this package, then cleans it up afterward. Individual
// tests opt into using it via useFakeClaude.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gralph-fakeclaude-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: create temp dir:", err)
		os.Exit(1)
	}

	claudeName := "claude"

	buildCmd := exec.Command("go", "build", "-o", filepath.Join(dir, claudeName), "./testdata/fakeclaude")
	buildCmd.Stdout = os.Stderr
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: build failed:", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	fakeClaudeDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// useFakeClaude prepends the directory holding the fake claude binary to
// PATH for the duration of the calling test. It uses t.Setenv, so the
// calling test must not call t.Parallel().
func useFakeClaude(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", fakeClaudeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// readFakeClaudeRecords recovers the exact stdin of every fake claude
// invocation, in call order. A missing or empty record file means claude was
// never invoked.
func readFakeClaudeRecords(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
	}
	if len(data) == 0 {
		return nil
	}

	parts := strings.Split(string(data), recordSeparator)
	require.NotEmpty(t, parts)
	return parts[:len(parts)-1]
}

// readSavedTasks reads and parses the tasks file runLoop wrote via
// tasks.SaveTasks, so a test can assert on the persisted state.
func readSavedTasks(t *testing.T, path string) tasks.TaskList {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	tl, err := tasks.ParseTasks(data)
	require.NoError(t, err)
	return tl
}

func writePromptFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func writeTasksFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestStart_MissingPromptFile(t *testing.T) {
	dir := t.TempDir()
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), filepath.Join(dir, "missing.md"), tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to start loop runner")
	assert.ErrorContains(t, err, "prompt file")
}

func TestStart_MissingTasksFile(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "Do the task.\n")

	err := Start(context.Background(), promptPath, filepath.Join(dir, "missing.yaml"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to start loop runner")
	assert.ErrorContains(t, err, "tasks file")
}

func TestStart_EmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), promptPath, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is empty")
}

func TestStart_WhitespaceOnlyPrompt(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "  \t\n  ")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), promptPath, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is just whitespace")
}

func TestStart_InvalidTasksYAML(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "Do the task.\n")
	tasksPath := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n")

	err := Start(context.Background(), promptPath, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse tasks yaml")
}

// TestStart_LoadFailuresBreakTheErrorChain pins that Start wraps getPrompt
// and getTasks failures with %s, not %w: the underlying sentinel is not
// reachable via errors.Is/As through Start. This documents the current
// behavior rather than asserting it as a desired design; production code is
// not changed to fix it.
func TestStart_LoadFailuresBreakTheErrorChain(t *testing.T) {
	dir := t.TempDir()
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), filepath.Join(dir, "missing.md"), tasksPath)
	require.Error(t, err)
	assert.False(t, errors.Is(err, os.ErrNotExist), "expected %%s wrapping in Start to break the error chain")
}

func TestStart_RunLoopFailureIsWrappedWithLoopErrorPrefix(t *testing.T) {
	dir := t.TempDir() // empty directory: no claude binary anywhere on PATH
	t.Setenv("PATH", dir)

	promptPath := writePromptFile(t, dir, "Do the task.\n")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), promptPath, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "loop error")
	assert.ErrorContains(t, err, "task 1: First task failed")
}

func TestStart_Success(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "Follow the plan.\n")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	recordPath := filepath.Join(dir, "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	err := Start(context.Background(), promptPath, tasksPath)
	require.NoError(t, err)

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 2, "expected claude invoked once per task")
}

func TestGetPrompt_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := getPrompt(filepath.Join(dir, "missing.md"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "prompt file")
	assert.ErrorContains(t, err, "not accessible")
}

func TestGetPrompt_ReadError(t *testing.T) {
	// A directory exists (Stat succeeds) but cannot be read as a file.
	dir := t.TempDir()

	_, err := getPrompt(dir)
	require.Error(t, err)
	assert.ErrorContains(t, err, "could not be read")
}

func TestGetPrompt_Empty(t *testing.T) {
	dir := t.TempDir()
	path := writePromptFile(t, dir, "")

	_, err := getPrompt(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is empty")
}

func TestGetPrompt_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := writePromptFile(t, dir, " \t\n ")

	_, err := getPrompt(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is just whitespace")
}

func TestGetPrompt_Success(t *testing.T) {
	dir := t.TempDir()
	path := writePromptFile(t, dir, "  Follow the plan.  \n")

	got, err := getPrompt(path)
	require.NoError(t, err)
	assert.Equal(t, "Follow the plan.", got)
}

func TestGetTasks_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := getTasks(filepath.Join(dir, "missing.yaml"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "tasks file")
	assert.ErrorContains(t, err, "not accessible")
}

func TestGetTasks_ReadError(t *testing.T) {
	dir := t.TempDir()

	_, err := getTasks(dir)
	require.Error(t, err)
	assert.ErrorContains(t, err, "could not be read")
}

func TestGetTasks_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n")

	_, err := getTasks(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse tasks yaml")
}

func TestGetTasks_Success(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, validTasksYAML)

	got, err := getTasks(path)
	require.NoError(t, err)
	require.NotNil(t, got)

	ids := make([]int16, 0, len(got.Tasks))
	for _, task := range got.Tasks {
		ids = append(ids, task.ID)
	}
	assert.Equal(t, []int16{1, 2}, ids)
}

func TestRunLoop_HappyPathInvokesInOrderWithExactStdin(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	p := "Follow the runbook."
	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "Do the first thing."},
		{ID: 2, Name: "Second", Prompt: "Do the second thing."},
		{ID: 3, Name: "Third", Prompt: "Do the third thing."},
	}}

	err := runLoop(context.Background(), p, tl, tasksPath)
	require.NoError(t, err)

	records := readFakeClaudeRecords(t, recordPath)
	require.Len(t, records, 3)
	for i, task := range tl.Tasks {
		want := fmt.Sprintf("%s\n\n%s\n", p, task.String())
		assert.Equal(t, want, records[i], "stdin for task %d must match the wire contract exactly", task.ID)
	}

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 3)
	for _, task := range saved.Tasks {
		assert.Equal(t, tasks.CompletedState, task.State, "task %d must be saved as completed", task.ID)
	}
}

func TestRunLoop_NonZeroExitStopsAtFirstTask(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	t.Setenv("FAKE_CLAUDE_EXIT", "1")

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1"},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	err := runLoop(context.Background(), "prompt", tl, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "task 1: First failed")

	var exitErr *exec.ExitError
	assert.ErrorAs(t, err, &exitErr)

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 1, "expected the second task never to run")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 2)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State, "the failed task must be saved as failed")
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State, "a task never reached stays pending")
}

func TestRunLoop_ClaudeMissingFromPath(t *testing.T) {
	dir := t.TempDir() // empty directory: no claude binary anywhere on PATH
	t.Setenv("PATH", dir)
	tasksPath := filepath.Join(dir, "tasks.yaml")

	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "Only", Prompt: "p"}}}

	err := runLoop(context.Background(), "prompt", tl, tasksPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "task")
	assert.ErrorContains(t, err, "failed")
	assert.ErrorIs(t, err, exec.ErrNotFound)

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 1)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
}

func TestRunLoop_ContextAlreadyCancelled(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml") // deliberately never created
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "Only", Prompt: "p"}}}

	err := runLoop(ctx, "prompt", tl, tasksPath)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	records := readFakeClaudeRecords(t, recordPath)
	assert.Empty(t, records, "expected claude never to be invoked")

	_, statErr := os.Stat(tasksPath)
	assert.True(t, os.IsNotExist(statErr), "expected the tasks file never to be written")
}

func TestRunLoop_PrintsPromptToStdout(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", filepath.Join(dir, "record.log"))

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "Do the first thing."},
		{ID: 2, Name: "Second", Prompt: "Do the second thing."},
	}}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runLoop(context.Background(), "Follow the runbook.", tl, tasksPath)

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	require.NoError(t, runErr)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()
	for _, task := range tl.Tasks {
		assert.Contains(t, output, task.String())
	}
}

func TestRunLoop_EmptyTaskList(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	err := runLoop(context.Background(), "prompt", &tasks.TaskList{}, tasksPath)
	require.NoError(t, err)

	records := readFakeClaudeRecords(t, recordPath)
	assert.Empty(t, records)
}

// TestRunLoop_SkipsCompletedTasks proves a task already marked completed is
// neither echoed nor handed to claude, while later pending tasks still run.
func TestRunLoop_SkipsCompletedTasks(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1", State: tasks.CompletedState},
		{ID: 2, Name: "Second", Prompt: "p2", State: tasks.PendingState},
	}}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runLoop(context.Background(), "prompt", tl, tasksPath)

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	require.NoError(t, runErr)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	output := buf.String()

	assert.Contains(t, output, "task 1: First already completed, skipping")
	assert.NotContains(t, output, "p1", "a completed task's prompt must not be echoed")

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 1, "claude must only be invoked for the pending task")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 2)
	assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State, "the already-completed task is unchanged")
	assert.Equal(t, tasks.CompletedState, saved.Tasks[1].State, "the pending task is now completed")
}

// TestRunLoop_RerunsPreviouslyFailedTask proves a task left in the failed
// state from an earlier run is retried, not skipped.
func TestRunLoop_RerunsPreviouslyFailedTask(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "Only", Prompt: "p1", State: tasks.FailedState},
	}}

	err := runLoop(context.Background(), "prompt", tl, tasksPath)
	require.NoError(t, err)

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 1, "a previously failed task must be run again")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 1)
	assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State, "a retried task that now succeeds is saved as completed")
}
