package looper

import (
	"bytes"
	"context"
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

// bypass is the session flags most tests run with; the fake claude ignores
// them, so only the argv tests care which flags they are.
var bypass = BypassArgs()

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

	err := Start(context.Background(), filepath.Join(dir, "missing.md"), tasksPath, "", bypass)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to start loop runner")
	assert.ErrorContains(t, err, "prompt file")
}

func TestStart_MissingTasksFile(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "Do the task.\n")

	err := Start(context.Background(), promptPath, filepath.Join(dir, "missing.yaml"), "", bypass)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to start loop runner")
	assert.ErrorContains(t, err, "tasks file")
}

func TestStart_EmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), promptPath, tasksPath, "", bypass)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is empty")
}

func TestStart_WhitespaceOnlyPrompt(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "  \t\n  ")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), promptPath, tasksPath, "", bypass)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is just whitespace")
}

func TestStart_InvalidTasksYAML(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "Do the task.\n")
	tasksPath := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n")

	err := Start(context.Background(), promptPath, tasksPath, "", bypass)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse tasks yaml")
}

// TestStart_LoadFailuresKeepTheErrorChain pins that Start wraps LoadPrompt
// and LoadTasks failures with %w, so the underlying error is reachable via
// errors.Is/As through Start.
func TestStart_LoadFailuresKeepTheErrorChain(t *testing.T) {
	dir := t.TempDir()
	promptPath := writePromptFile(t, dir, "Do the task.\n")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), filepath.Join(dir, "missing.md"), tasksPath, "", bypass)
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)

	err = Start(context.Background(), promptPath, filepath.Join(dir, "missing.yaml"), "", bypass)
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestStart_RunLoopFailureIsWrappedWithLoopErrorPrefix(t *testing.T) {
	dir := t.TempDir() // empty directory: no claude binary anywhere on PATH
	t.Setenv("PATH", dir)

	promptPath := writePromptFile(t, dir, "Do the task.\n")
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	err := Start(context.Background(), promptPath, tasksPath, "", bypass)
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

	err := Start(context.Background(), promptPath, tasksPath, "", bypass)
	require.NoError(t, err)

	records := readFakeClaudeRecords(t, recordPath)
	assert.Len(t, records, 2, "expected claude invoked once per task")
}

func TestLoadPrompt_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadPrompt(filepath.Join(dir, "missing.md"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "prompt file")
	assert.ErrorContains(t, err, "not accessible")
}

func TestLoadPrompt_ReadError(t *testing.T) {
	// A directory exists but cannot be read as a file.
	dir := t.TempDir()

	_, err := LoadPrompt(dir)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not accessible")
}

func TestLoadPrompt_Empty(t *testing.T) {
	dir := t.TempDir()
	path := writePromptFile(t, dir, "")

	_, err := LoadPrompt(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is empty")
}

func TestLoadPrompt_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := writePromptFile(t, dir, " \t\n ")

	_, err := LoadPrompt(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "the prompt file is just whitespace")
}

func TestLoadPrompt_Success(t *testing.T) {
	dir := t.TempDir()
	path := writePromptFile(t, dir, "  Follow the plan.  \n")

	got, err := LoadPrompt(path)
	require.NoError(t, err)
	assert.Equal(t, "Follow the plan.", got)
}

func TestLoadTasks_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadTasks(filepath.Join(dir, "missing.yaml"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "tasks file")
	assert.ErrorContains(t, err, "not accessible")
}

func TestLoadTasks_ReadError(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadTasks(dir)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not accessible")
}

func TestLoadTasks_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n")

	_, err := LoadTasks(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse tasks yaml")
}

func TestLoadTasks_Success(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, validTasksYAML)

	got, err := LoadTasks(path)
	require.NoError(t, err)
	require.NotNil(t, got)

	ids := make([]int16, 0, len(got.Tasks))
	for _, task := range got.Tasks {
		ids = append(ids, task.ID)
	}
	assert.Equal(t, []int16{1, 2}, ids)
}

func TestLoadTasks_FailedTaskReturnsListAndErrFailedTasks(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 2, name: b, prompt: p, state: failed}\n")

	got, err := LoadTasks(path)
	require.ErrorIs(t, err, ErrFailedTasks)
	require.NotNil(t, got)
	assert.Len(t, got.Tasks, 2)
}

func TestLoadTasksReport_FailedTaskWritesNoticeAndTable(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p}\n  - {id: 2, name: b, prompt: p, state: failed}\n")

	var got bytes.Buffer
	tl, err := LoadTasksReport(&got, path)
	require.ErrorIs(t, err, ErrFailedTasks)
	require.NotNil(t, tl)

	var want bytes.Buffer
	want.WriteString("Some tasks failed previous runs:\n")
	PrintTasks(&want, tl)
	assert.Equal(t, want.String(), got.String())
}

func TestLoadTasksReport_CleanFileWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, validTasksYAML)

	var got bytes.Buffer
	tl, err := LoadTasksReport(&got, path)
	require.NoError(t, err)
	require.NotNil(t, tl)
	assert.Empty(t, got.String())
}

func TestLoadTasks_ParseErrorIsNotErrFailedTasks(t *testing.T) {
	dir := t.TempDir()
	path := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n")

	_, err := LoadTasks(path)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrFailedTasks)
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

	err := runLoop(context.Background(), p, tl, tasksPath, "", bypass, nil, nil)
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

	err := runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "task 1: First failed")
	assert.ErrorContains(t, err, "exit status 1")

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

	err := runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "task")
	assert.ErrorContains(t, err, "failed")
	assert.ErrorContains(t, err, "executable file not found")

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

	err := runLoop(ctx, "prompt", tl, tasksPath, "", bypass, nil, nil)
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

	runErr := runLoop(context.Background(), "Follow the runbook.", tl, tasksPath, "", bypass, nil, nil)

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

	err := runLoop(context.Background(), "prompt", &tasks.TaskList{}, tasksPath, "", bypass, nil, nil)
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

	runErr := runLoop(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil)

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

// TestStart_RefusesWhenAnyTaskFailed proves Start refuses to run while any
// task is failed: it returns the fix-and-reset error, never invokes claude, and
// leaves the tasks file byte-for-byte unchanged.
func TestStart_RefusesWhenAnyTaskFailed(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	promptPath := writePromptFile(t, dir, "Follow the plan.\n")
	content := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
  - id: 2
    name: Second task
    prompt: Do the second thing.
    state: failed
    error: boom
  - id: 3
    name: Third task
    prompt: Do the third thing.
    state: failed
`
	tasksPath := writeTasksFile(t, dir, content)

	err := Start(context.Background(), promptPath, tasksPath, "", bypass)
	require.Error(t, err)
	assert.EqualError(t, err, "fix the failed tasks and set their state to pending before running")
	assert.ErrorIs(t, err, ErrFailedTasks)

	assert.Empty(t, readFakeClaudeRecords(t, recordPath), "claude must never be invoked")
	data, readErr := os.ReadFile(tasksPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data), "the tasks file must be unchanged")
}

func TestDryRun_ValidFile(t *testing.T) {
	dir := t.TempDir()
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	var out bytes.Buffer
	require.NoError(t, DryRun(&out, tasksPath, "", ""))
	want := "   ID  STATE      NAME\n" +
		"   --  ---------  ----\n" +
		"    1  PENDING    First task\n" +
		"    2  PENDING    Second task\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, out.String())
}

// TestDryRun_NoFailedTasksTable pins the exact table, colour codes included,
// for completed and pending tasks, with a 3-digit id widening the ID column.
func TestDryRun_NoFailedTasksTable(t *testing.T) {
	dir := t.TempDir()
	content := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    state: completed
  - id: 123
    name: Second task
    prompt: Do the second thing.
`
	tasksPath := writeTasksFile(t, dir, content)

	var out bytes.Buffer
	require.NoError(t, DryRun(&out, tasksPath, "", ""))
	want := "    ID  STATE      NAME\n" +
		"   ---  ---------  ----\n" +
		"✅   1  \033[92mCOMPLETED\033[0m  First task\n" +
		"   123  PENDING    Second task\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, out.String())
}

func TestDryRun_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	tasksPath := writeTasksFile(t, dir, "tasks:\n  - {id: 1, name: a, prompt: p, state: bogus}\n")

	var out bytes.Buffer
	err := DryRun(&out, tasksPath, "", "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to parse tasks yaml")
	assert.Empty(t, out.String())
}

// TestDryRun_FlagsFailedTasks pins the exact table, colour codes included,
// for a mix of completed, pending, and failed tasks with a 3-digit id, and
// that a file with failed tasks is still accepted.
func TestDryRun_FlagsFailedTasks(t *testing.T) {
	dir := t.TempDir()
	content := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    state: completed
  - id: 2
    name: Second task
    prompt: Do the second thing.
    state: failed
    error: boom
  - id: 100
    name: Third task
    prompt: Do the third thing.
`
	tasksPath := writeTasksFile(t, dir, content)

	var out bytes.Buffer
	require.NoError(t, DryRun(&out, tasksPath, "", ""))
	want := "Some tasks failed previous runs:\n" +
		"    ID  STATE      NAME\n" +
		"   ---  ---------  ----\n" +
		"✅   1  \033[92mCOMPLETED\033[0m  First task\n" +
		"❌   2  \033[1;91mFAILED   \033[0m  Second task  \033[1;91m← Needs review!\033[0m\n" +
		"   100  PENDING    Third task\n"
	assert.Equal(t, want, out.String())

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data), "the tasks file must be unchanged")
}

func TestDryRun_ListsEffectiveGateTimeouts(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	yml := `tasks:
  - id: 1
    name: First
    prompt: p
    gates:
      - cmd: go test ./...
        timeout: 30s
      - cmd: |
          echo one
          echo two
  - id: 2
    name: Second
    prompt: p
  - id: 3
    name: Third
    prompt: p
    gates:
      - cmd: make lint
`
	require.NoError(t, os.WriteFile(tasksPath, []byte(yml), 0o600))

	tests := []struct {
		name     string
		override string
		want     string
	}{
		{name: "gate and default", want: "task 1 gate: go test ./...: 30s (gate)\n" +
			"task 1 gate: echo one: 10m (default)\n" +
			"task 3 gate: make lint: 10m (default)\n"},
		{name: "flag overrides every gate", override: "2m", want: "task 1 gate: go test ./...: 2m (flag)\n" +
			"task 1 gate: echo one: 2m (flag)\n" +
			"task 3 gate: make lint: 2m (flag)\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, DryRun(&out, tasksPath, tt.override, ""))

			got := out.String()
			assert.Contains(t, got, "NAME\n", "the task table comes first")
			assert.True(t, strings.HasSuffix(got, tt.want+tasksPath+" is valid\n"), "gate lines sit between the table and the valid line, got:\n%s", got)
		})
	}
}

func TestDryRun_NoGatesPrintsNoGateLines(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte("tasks:\n  - {id: 1, name: First, prompt: p}\n"), 0o600))

	var out bytes.Buffer
	require.NoError(t, DryRun(&out, tasksPath, "2m", ""))

	assert.NotContains(t, out.String(), "gate")
	assert.True(t, strings.HasSuffix(out.String(), "First\n"+tasksPath+" is valid\n"))
}

func TestRunTaskPlain_ArgvIsPrintThenSessionArgs(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	t.Setenv("FAKE_CLAUDE_ARGS", argsPath)

	sessionArgs := []string{"--permission-mode", "acceptEdits", "--settings", `{"sandbox":{"enabled":true}}`}
	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "Do the first thing."}}}
	require.NoError(t, runLoop(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), "", sessionArgs, nil, nil))

	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, "--print --permission-mode acceptEdits --settings {\"sandbox\":{\"enabled\":true}}\n", string(args))
}

func TestDryRun_NamesSandboxSettings(t *testing.T) {
	dir := t.TempDir()
	tasksPath := writeTasksFile(t, dir, validTasksYAML)

	var out bytes.Buffer
	require.NoError(t, DryRun(&out, tasksPath, "", "sandbox.json"))
	want := "   ID  STATE      NAME\n" +
		"   --  ---------  ----\n" +
		"    1  PENDING    First task\n" +
		"    2  PENDING    Second task\n" +
		"sandbox settings: sandbox.json\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, out.String())
}
