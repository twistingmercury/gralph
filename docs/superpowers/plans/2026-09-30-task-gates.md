# Task Gates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Status:** executed on 2026-09-30 (commits `1:` to `5:` on `feature/gates`). Afterwards the gate call was changed from passing the command in a `GRALPH_GATE` environment variable to plain `sh -c <cmd>` under an owner-approved `#nosec G204`; this plan was updated to show the code as shipped.
>
> This plan is executed as a Gralph loop: `.local/gates/tasks.yaml` and `.local/gates/prompt.md` carry one loop task per plan task.

**Goal:** Let a task in `tasks.yaml` list `gates`, shell commands gralph runs itself after a session reports `completed`, and mark the task completed only when every gate exits zero.

**Architecture:** `internal/tasks` gains a `Gate` type and strict parse-time validation of the optional `gates` sequence. `internal/looper` gains `gates.go` with `runGates`, called from a new `runTask` wrapper after either task path returns `completed`; a failing gate turns the outcome into `failed`. No TUI code changes: gate lines arrive as ordinary `Activity` events.

**Tech Stack:** Go 1.27.1, `gopkg.in/yaml.v3`, `os/exec`, `github.com/stretchr/testify`. No new dependencies.

**Spec:** ADR-013 in `docs/architecture/02_architectural_decisions.md`, with the Gate Runner component in `docs/architecture/03_system_architecture.md`. Read both before starting a task.

## Global Constraints

- Work on branch `feature/gates`. Never push.
- Gates are never sent to Claude. The stdin text stays `fmt.Sprintf("%s\n\n%s\n", prompt, task.String())` and `Task.String()` must not include gates.
- A task with no `gates`, or an empty list, behaves exactly as before. Existing unit and e2e tests keep passing unchanged.
- Gates run only after a session that exited zero with the `completed` result line.
- Each gate runs as `exec.CommandContext(ctx, "sh", "-c", gate.Cmd)`. gosec flags that call (G204); the finding is accepted by design, and the line carries the one `// #nosec G204` the owner explicitly approved. Keep that comment and its reason on the line.
- Failed gate error text is exactly `gate "<cmd>" failed: <exit error>`, built with `fmt.Sprintf("gate %q failed: %s", cmd, err)`.
- Plain mode prints exactly `gate: <cmd>` and a newline to stdout before each gate. The stream path reports the `Activity` line `→ gate <first line of cmd>`.
- No timeout, no retry, no file-level gates, no new event kinds, no new interfaces, no new dependencies.
- `looper` never imports `tui` or Bubble Tea. Unix only.
- Never add `//nolint` or `// #nosec`, apart from the one owner-approved `#nosec G204` on the gate call. Go tests use testify (`require` for preconditions, `assert` for checks).
- Code style, for every line written or moved: return early, keep the happy path at the left margin, a blank line after every `if` block (except before a closing brace or `else`), callbacks longer than a line or two are named functions, comments say why.
- YAML files use `.yaml`. Architecture docs are edited in place with `Version`/`Date`/`Notes` bumped; never create `_vNN` copies.
- Quality gates for every task, from the repository root: `make test && make analyze && make build`. The e2e suite runs only inside `make build`.
- Never run `make install`, `gralph --install-skill` against the real home directory, or `scripts/install_skill.sh`: the loop is driven by the installed gralph and its installed skill.

## Review Focus

Inputs the spec implies and a user is likely to hit. Each has a pinning test in the task named.

1. A gate names a command that is not installed: the task fails with `exit status 127`, gralph does not crash (Task 2, `TestRunLoop_GateShellBehavior`).
2. A gate reads stdin (`cat`, a prompt): it sees empty input and returns, it must not hang on gralph's terminal (Task 2, `TestRunLoop_GateShellBehavior`).
3. A multi-line or pipe-using `cmd`: it runs as one shell script (Task 2, `TestRunLoop_GateShellBehavior`), and the activity line shows only its first line (Task 2, `TestRun_GateOutputArrivesAsActivity`).
4. A `cmd` containing double quotes fails: the saved `error` text must survive the YAML save and the next load (Task 2, `TestRunLoop_GateFailureFailsTaskAndStopsRun`; Task 3, the second run in `TestGates_FailingGateFailsTheTaskAndBlocksTheNextRun`).
5. `gates:` written with nothing under it, or with a misspelled key such as `command:`: the file is rejected with an error naming the task and gate (Task 1, `TestParseTasks_GateErrors`; Task 3, `TestDryRun_InvalidGates`).

## File Structure

| File | Change | Responsibility |
| --- | --- | --- |
| `internal/tasks/task.go` | Modify | `Gate` type, `Task.Gates`, `checkGates`/`checkGate` validation |
| `internal/tasks/task_test.go` | Modify | Gate parse, error, and save tests |
| `internal/looper/gates.go` | Create | `runGates`, `runGate`, `runGateStream` |
| `internal/looper/gates_test.go` | Create | Gate runner tests on both task paths |
| `internal/looper/looper.go` | Modify | `runTask` wrapper; `runLoop` calls it |
| `internal/looper/stream.go` | Modify | Rename `stderrLine` to `rawLine` (gates reuse it for stdout too) |
| `tests/e2e/claude_helpers_test.go` | Modify | `gateFixture`, `taskFixture.Gates` |
| `tests/e2e/gates_test.go` | Create | Black-box gate tests |
| `tests/e2e/dry_run_test.go` | Modify | Dry-run gate validation tests |
| `skills/gralph-docs-writer/SKILL.md` | Modify | `gates` field rules and guidance |
| `skills/gralph-docs-writer/templates/tasks_template.yaml` | Modify | `gates` in the template |
| `README.md`, `CLAUDE.md`, `docs/architecture/05_deployment_architecture.md` | Modify | User and contributor docs |

---

### Task 1: Gate model and validation in `internal/tasks`

**Files:**
- Modify: `internal/tasks/task.go`
- Test: `internal/tasks/task_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `type Gate struct { Cmd string }` (yaml key `cmd`) and `Task.Gates []Gate` (yaml key `gates`, omitted when empty). `ParseTasks` rejects a malformed `gates` value. `Task.String()` is unchanged.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tasks/task_test.go`:

```go
func TestParseTasks_ReadsGates(t *testing.T) {
	yml := []byte(`tasks:
  - id: 1
    name: Gated
    prompt: p
    gates:
      - cmd: go test ./...
      - cmd: |
          test -z "$(gofmt -l .)"
  - id: 2
    name: Ungated
    prompt: p
  - id: 3
    name: Empty list
    prompt: p
    gates: []
`)

	got, err := ParseTasks(yml)
	require.NoError(t, err)
	require.Len(t, got.Tasks, 3)

	want := []Gate{{Cmd: "go test ./..."}, {Cmd: "test -z \"$(gofmt -l .)\"\n"}}
	assert.Equal(t, want, got.Tasks[0].Gates, "cmd text must be stored unaltered, in file order")
	assert.Empty(t, got.Tasks[1].Gates)
	assert.Empty(t, got.Tasks[2].Gates)
}

func TestTaskString_OmitsGates(t *testing.T) {
	task := Task{ID: 1, Name: "First task", Prompt: "Do it.", Gates: []Gate{{Cmd: "go test ./..."}}}
	assert.Equal(t, "1: First task\n\nDo it.", task.String(), "gates are gralph's check and must never reach the session")
}

func TestParseTasks_GateErrors(t *testing.T) {
	tests := []struct {
		name    string
		gates   string
		wantErr string
	}{
		{name: "null gates", gates: "gates:", wantErr: "tasks[0] (id 1): gates: must be a sequence"},
		{name: "scalar gates", gates: "gates: go test ./...", wantErr: "tasks[0] (id 1): gates: must be a sequence"},
		{name: "mapping gates", gates: "gates: {cmd: go test ./...}", wantErr: "tasks[0] (id 1): gates: must be a sequence"},
		{name: "string element", gates: "gates: [go test ./...]", wantErr: "tasks[0] (id 1): gates[0]: must be a mapping"},
		{name: "missing cmd", gates: "gates: [{}]", wantErr: "tasks[0] (id 1): gates[0]: cmd: is required"},
		{name: "misspelled key", gates: "gates: [{command: a}]", wantErr: "tasks[0] (id 1): gates[0]: command: unknown key; a gate has only cmd"},
		{name: "extra key", gates: "gates: [{cmd: a, timeout: 5}]", wantErr: "tasks[0] (id 1): gates[0]: timeout: unknown key; a gate has only cmd"},
		{name: "duplicate cmd", gates: "gates: [{cmd: a, cmd: b}]", wantErr: "tasks[0] (id 1): gates[0]: cmd: duplicate key"},
		{name: "integer cmd", gates: "gates: [{cmd: 5}]", wantErr: "tasks[0] (id 1): gates[0]: cmd: must be a string"},
		{name: "null cmd", gates: "gates: [{cmd: }]", wantErr: "tasks[0] (id 1): gates[0]: cmd: must be a string"},
		{name: "blank cmd", gates: `gates: [{cmd: "  "}]`, wantErr: "tasks[0] (id 1): gates[0]: cmd: must not be empty or whitespace"},
		{name: "second gate invalid", gates: "gates: [{cmd: a}, {cmd: ''}]", wantErr: "tasks[0] (id 1): gates[1]: cmd: must not be empty or whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yml := "tasks:\n  - id: 1\n    name: a\n    prompt: p\n    " + tt.gates + "\n"
			got, err := ParseTasks([]byte(yml))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, TaskList{}, got, "an error must return the zero TaskList, never a partial one")
		})
	}
}

func TestSaveTasks_GatesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")

	want := TaskList{Tasks: []Task{
		{ID: 1, Name: "First", Prompt: "p", State: PendingState, Gates: []Gate{
			{Cmd: "go test ./..."},
			{Cmd: "echo one\necho \"two\"\n"},
		}},
	}}
	require.NoError(t, SaveTasks(path, want))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	got, err := ParseTasks(data)
	require.NoError(t, err)
	assert.Equal(t, want, got, "gates must round-trip verbatim and in order")
}

func TestSaveTasks_OmitsEmptyGates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")

	require.NoError(t, SaveTasks(path, TaskList{Tasks: []Task{{ID: 1, Name: "First", Prompt: "p", State: PendingState}}}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "gates", "a task without gates must not gain a gates key on save")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tasks -run 'Gate' -count=1`
Expected: build failure, `undefined: Gate` and `unknown field Gates`.

- [ ] **Step 3: Add the type and field**

In `internal/tasks/task.go`, replace the `Task` struct with:

```go
// Gate is one command gralph runs itself after a task's session reports
// completed. It is never sent to the session.
type Gate struct {
	Cmd string `yaml:"cmd"`
}

type Task struct {
	ID     int16  `yaml:"id"`
	Name   string `yaml:"name"`
	Prompt string `yaml:"prompt"`
	State  string `yaml:"state"`
	Error  string `yaml:"error,omitempty"`
	Gates  []Gate `yaml:"gates,omitempty"`
}
```

- [ ] **Step 4: Add the validation**

In `parseTask`, between the `checkStringFields` block and `decodeTask`, add:

```go
	if err := checkGates(where, fields["gates"]); err != nil {
		return Task{}, where, err
	}

```

Add these functions after `checkStringField`:

```go
// checkGates checks the optional gates sequence before it is decoded. A
// malformed gate must reject the file: decoding would silently drop what it
// does not recognize, and the next save would then erase it.
func checkGates(where string, node *yaml.Node) error {
	if node == nil {
		return nil
	}

	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s: gates: must be a sequence", where)
	}

	for j, el := range node.Content {
		if err := checkGate(fmt.Sprintf("%s: gates[%d]", where, j), el); err != nil {
			return err
		}
	}

	return nil
}

// checkGate allows only the cmd key so a misspelled or invented option is an
// error instead of a gate that quietly does less than its author meant.
func checkGate(where string, el *yaml.Node) error {
	if el.Kind == yaml.AliasNode {
		el = el.Alias
	}

	if el.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: must be a mapping", where)
	}

	fields, err := taskFields(where, el)
	if err != nil {
		return err
	}

	for j := 0; j+1 < len(el.Content); j += 2 {
		if key := el.Content[j].Value; key != "cmd" {
			return fmt.Errorf("%s: %s: unknown key; a gate has only cmd", where, key)
		}
	}

	cmd := fields["cmd"]
	switch {
	case cmd == nil:
		return fmt.Errorf("%s: cmd: is required", where)
	case cmd.Kind != yaml.ScalarNode || cmd.Tag != "!!str":
		return fmt.Errorf("%s: cmd: must be a string", where)
	case strings.TrimSpace(cmd.Value) == "":
		return fmt.Errorf("%s: cmd: must not be empty or whitespace", where)
	}

	return nil
}
```

`taskFields` already reports a duplicate key as `<where>: <key>: duplicate key` and resolves aliases; reuse it as shown.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tasks -count=1`
Expected: `ok`. Every existing test still passes.

- [ ] **Step 6: Run the quality gates and commit**

Run: `make test && make analyze && make build`
Expected: all exit 0, analyze reports no issues.

```bash
git add internal/tasks/task.go internal/tasks/task_test.go
git commit -m "1: Add gates to the task model"
```

---

### Task 2: Gate runner in `internal/looper`

**Files:**
- Create: `internal/looper/gates.go`
- Create: `internal/looper/gates_test.go`
- Modify: `internal/looper/looper.go` (`runLoop`, new `runTask`)
- Modify: `internal/looper/stream.go` (rename `stderrLine` to `rawLine`)

**Interfaces:**
- Consumes: `tasks.Gate{Cmd string}`, `tasks.Task.Gates []tasks.Gate` from Task 1. Existing helpers: `configureProcessTree(cmd *exec.Cmd)`, `readLines(r io.Reader, fn func(line []byte))`, `streamTask{task, report}` with `readStderr(stderr io.Reader, done chan<- struct{})`, `Event`, `Activity`.
- Produces: `runGates(ctx context.Context, task tasks.Task, report func(Event)) (state, errMsg string, err error)` and `runTask(ctx context.Context, p string, task tasks.Task, report func(Event)) (state, errMsg string, err error)`. Behavior visible to later tasks: plain stdout line `gate: <cmd>`, saved error `gate "<cmd>" failed: <exit error>`.

- [ ] **Step 1: Write the failing tests**

Create `internal/looper/gates_test.go`:

```go
package looper

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// touchGate is a gate that leaves proof it ran.
func touchGate(path string) tasks.Gate {
	return tasks.Gate{Cmd: "touch '" + path + "'"}
}

func gatedTask(gates ...tasks.Gate) *tasks.TaskList {
	return &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "p1", Gates: gates}}}
}

func TestRunLoop_GatesPassCompletesTask(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	first, second := filepath.Join(dir, "gate1"), filepath.Join(dir, "gate2")

	// The second gate passes only if the first already ran: file order.
	gates := []tasks.Gate{touchGate(first), {Cmd: "test -e '" + first + "' && touch '" + second + "'"}}
	tl := gatedTask(gates...)

	require.NoError(t, runLoop(context.Background(), "prompt", tl, tasksPath, nil))

	assert.FileExists(t, first)
	assert.FileExists(t, second)

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 1)
	assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
	assert.Empty(t, saved.Tasks[0].Error)
	assert.Equal(t, gates, saved.Tasks[0].Gates, "gates must survive the save")
}

func TestRunLoop_GateFailureFailsTaskAndStopsRun(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	never := filepath.Join(dir, "never")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1", Gates: []tasks.Gate{{Cmd: `test "a" = "b"`}, touchGate(never)}},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	const wantErr = `gate "test \"a\" = \"b\"" failed: exit status 1`
	err := runLoop(context.Background(), "prompt", tl, tasksPath, nil)
	require.EqualError(t, err, "task 1: First failed: "+wantErr)

	assert.NoFileExists(t, never, "gates after a failed gate must not run")
	assert.Len(t, readFakeClaudeRecords(t, recordPath), 1, "the second task must never run")

	saved := readSavedTasks(t, tasksPath)
	require.Len(t, saved.Tasks, 2)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, wantErr, saved.Tasks[0].Error, "the quoted command must survive the yaml save and reload")
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State)
}

func TestRunLoop_GatesSkippedWhenSessionFails(t *testing.T) {
	tests := []struct {
		name    string
		env     [2]string
		wantErr string
	}{
		{name: "non-zero exit", env: [2]string{"FAKE_CLAUDE_EXIT", "1"}, wantErr: "exit status 1"},
		{name: "session reports failed", env: [2]string{"FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"nope"}`}, wantErr: "nope"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			t.Setenv(tt.env[0], tt.env[1])
			dir := t.TempDir()
			marker := filepath.Join(dir, "gate-ran")

			err := runLoop(context.Background(), "prompt", gatedTask(touchGate(marker)), filepath.Join(dir, "tasks.yaml"), nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.NotContains(t, err.Error(), "gate")
			assert.NoFileExists(t, marker, "a failed session must not run gates")
		})
	}
}

func TestRunLoop_GatesNeverReachClaude(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)

	tl := gatedTask(tasks.Gate{Cmd: "true # gate-only-text"})
	require.NoError(t, runLoop(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), nil))

	records := readFakeClaudeRecords(t, recordPath)
	require.Len(t, records, 1)
	assert.Equal(t, "prompt\n\n1: First\n\np1\n", records[0], "the stdin contract must not change when a task has gates")
}

func TestRunLoop_GateShellBehavior(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantErr string
	}{
		{name: "pipes and substitution", cmd: `test "$(printf ab | tr a-z A-Z)" = AB`},
		{name: "multi-line command", cmd: "x=1\ntest \"$x\" = 1"},
		{name: "stdin is empty, not inherited", cmd: `test -z "$(cat)"`},
		{name: "exit code is reported", cmd: "exit 3", wantErr: `gate "exit 3" failed: exit status 3`},
		{name: "command not found", cmd: "gralph-no-such-command", wantErr: `gate "gralph-no-such-command" failed: exit status 127`},
		{name: "shell syntax error", cmd: `echo "unterminated`, wantErr: `gate "echo \"unterminated" failed: exit status`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

			err := runLoop(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: tt.cmd}), tasksPath, nil)
			saved := readSavedTasks(t, tasksPath)
			require.Len(t, saved.Tasks, 1)

			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
				return
			}

			require.Error(t, err)
			assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
			assert.Contains(t, saved.Tasks[0].Error, tt.wantErr)
		})
	}
}

func TestRunLoop_PlainPrintsGateLineAndOutput(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runLoop(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: "echo gate-says-hi"}), tasksPath, nil)

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	require.NoError(t, runErr)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "gate: echo gate-says-hi\ngate-says-hi\n")
}

func TestRun_GateOutputArrivesAsActivity(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	outCh := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		outCh <- string(out)
	}()

	var rec recorder
	tl := gatedTask(tasks.Gate{Cmd: "echo from-stdout\necho from-stderr >&2"})
	runErr := Run(context.Background(), "prompt", tl, tasksPath, rec.report)
	os.Stdout = orig
	require.NoError(t, w.Close())
	require.NoError(t, runErr)
	assert.Empty(t, <-outCh, "the stream path must write nothing to gralph's stdout, gates included")

	events := rec.snapshot()
	lines := activityLines(events)
	assert.Contains(t, lines, "→ gate echo from-stdout", "only the command's first line names the gate")
	assert.Contains(t, lines, "from-stdout")
	assert.Contains(t, lines, "from-stderr")

	kinds := eventKinds(events)
	require.GreaterOrEqual(t, len(kinds), 2)
	assert.Equal(t, []EventKind{TaskFinished, RunDone}, kinds[len(kinds)-2:], "TaskFinished must follow the gate's activity")
}

func TestRun_GateFailureReportsFailedTask(t *testing.T) {
	useFakeClaude(t)
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")

	var rec recorder
	err := Run(context.Background(), "prompt", gatedTask(tasks.Gate{Cmd: "exit 3"}), tasksPath, rec.report)
	require.Error(t, err)

	var finished []Event
	for _, e := range rec.snapshot() {
		if e.Kind == TaskFinished {
			finished = append(finished, e)
		}
	}

	require.Len(t, finished, 1)
	assert.Equal(t, tasks.FailedState, finished[0].Task.State)
	assert.Equal(t, `gate "exit 3" failed: exit status 3`, finished[0].Task.Error)
	assert.Error(t, finished[0].Err)
}

func TestRun_CancelDuringGateLeavesFileUntouched(t *testing.T) {
	tests := []struct {
		name   string
		stream bool
	}{
		{name: "plain"},
		{name: "stream", stream: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { cancelDuringGate(t, tt.stream) })
	}
}

func cancelDuringGate(t *testing.T, stream bool) {
	useFakeClaude(t)
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	tasksPath := filepath.Join(dir, "tasks.yaml")

	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(tasksYAML), 0o600))
	tl := gatedTask(tasks.Gate{Cmd: "touch '" + readyPath + "'; sleep 60"})

	var rec recorder
	var report func(Event)
	if stream {
		report = rec.report
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, "prompt", tl, tasksPath, report) }()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the gate never started")

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.ErrorContains(t, err, "task 1: First failed: gate")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a cancel during a gate; the gate's process group was not killed")
	}

	assert.NotContains(t, eventKinds(rec.snapshot()), TaskFinished)
	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got), "the tasks file must be byte-for-byte unchanged after a cancel during a gate")
}
```

`useFakeClaude`, `readFakeClaudeRecords`, `readSavedTasks`, `recorder`, `activityLines`, and `eventKinds` already exist in this package's tests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/looper -run 'Gate' -count=1`
Expected: FAIL. Gates are ignored, so for example `TestRunLoop_GatesPassCompletesTask` reports the marker file does not exist.

- [ ] **Step 3: Rename the raw-line callback**

In `internal/looper/stream.go`, rename the method `stderrLine` to `rawLine` (its definition and its one use in `readStderr`). Gates report stdout lines through it too, so the old name would mislead.

```go
func (st *streamTask) readStderr(stderr io.Reader, done chan<- struct{}) {
	defer close(done)
	readLines(stderr, st.rawLine)
}

func (st *streamTask) rawLine(line []byte) {
	st.report(Event{Kind: Activity, Task: st.task, Line: strings.TrimRight(string(line), "\r\n")})
}
```

- [ ] **Step 4: Write the gate runner**

Create `internal/looper/gates.go`:

```go
package looper

import (
	"context"
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
func runGates(ctx context.Context, task tasks.Task, report func(Event)) (state, errMsg string, err error) {
	for _, gate := range task.Gates {
		runErr := runGate(ctx, task, gate, report)
		if runErr == nil {
			continue
		}

		if ctx.Err() != nil {
			return "", "", fmt.Errorf("task %d: %s failed: gate %q: %w", task.ID, task.Name, gate.Cmd, runErr)
		}

		return tasks.FailedState, fmt.Sprintf("gate %q failed: %s", gate.Cmd, runErr), nil
	}

	return tasks.CompletedState, "", nil
}

// runGate runs one gate through `sh -c` so the command can use shell syntax
// (pipes, &&, redirects). gosec's G204 finding is accepted on purpose: running
// a command from the task file is the feature (ADR-013). The gate gets no
// stdin and its own process group, like claude, so a cancel kills everything
// it started.
func runGate(ctx context.Context, task tasks.Task, gate tasks.Gate, report func(Event)) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", gate.Cmd) // #nosec G204 -- a gate is a command from the task file, run by design (ADR-013); suppression approved by the owner
	configureProcessTree(cmd)

	if report != nil {
		// Only the first line, so a multi-line command stays one activity line.
		first, _, _ := strings.Cut(gate.Cmd, "\n")
		report(Event{Kind: Activity, Task: task, Line: "→ gate " + first})
		return runGateStream(cmd, task, report)
	}

	fmt.Printf("gate: %s\n", gate.Cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runGateStream runs cmd with each stdout and stderr line reported as an
// Activity event, so nothing reaches gralph's own stdout or stderr. Both
// pipes are drained before Wait, as runTaskStream does.
func runGateStream(cmd *exec.Cmd, task tasks.Task, report func(Event)) error {
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
```

- [ ] **Step 5: Call the gates from the loop**

In `internal/looper/looper.go`, inside `runLoop`, replace this block:

```go
		var state, errMsg string
		var err error
		if report == nil {
			state, errMsg, err = runTaskPlain(ctx, p, *task)
		} else {
			state, errMsg, err = runTaskStream(ctx, p, *task, report)
		}

		if err != nil {
			return err
		}
```

with:

```go
		state, errMsg, err := runTask(ctx, p, *task, report)
		if err != nil {
			return err
		}
```

and add this function directly after `runLoop`:

```go
// runTask runs one task's session on the path report selects and, only when
// the session completed, the task's gates. It returns the task's outcome, or
// an error when ctx was cancelled.
func runTask(ctx context.Context, p string, task tasks.Task, report func(Event)) (state, errMsg string, err error) {
	if report == nil {
		state, errMsg, err = runTaskPlain(ctx, p, task)
	} else {
		state, errMsg, err = runTaskStream(ctx, p, task, report)
	}

	if err != nil || state != tasks.CompletedState {
		return state, errMsg, err
	}

	return runGates(ctx, task, report)
}
```

Nothing else in `runLoop` changes: it already saves the state and error and stops the run on `failed`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/looper -race -count=1`
Expected: `ok`. Every existing looper test still passes.

Run: `go test ./internal/tui -count=1`
Expected: `ok` (no TUI code changed; gate lines are ordinary `Activity` events).

- [ ] **Step 7: Run the quality gates and commit**

Run: `make test && make analyze && make build`
Expected: all exit 0. `make analyze` must report no gosec issue for `gates.go` (the approved `#nosec G204` covers the gate call).

```bash
git add internal/looper/gates.go internal/looper/gates_test.go internal/looper/looper.go internal/looper/stream.go
git commit -m "2: Run a task's gates after a completed session"
```

---

### Task 3: End-to-end coverage

**Files:**
- Modify: `tests/e2e/claude_helpers_test.go` (`taskFixture`)
- Create: `tests/e2e/gates_test.go`
- Modify: `tests/e2e/dry_run_test.go`

**Interfaces:**
- Consumes: the built gralph binary's behavior from Tasks 1 and 2: stdout line `gate: <cmd>`, stderr `task <id>: <name> failed: gate "<cmd>" failed: <exit error>`, parse error `tasks[<i>] (id <id>): gates[<j>]: <key>: unknown key; a gate has only cmd`. Existing helpers: `gralphEnv`, `runGralph`, `writePrompt`, `writeTasksYAML`, `readFakeClaudeRecords`, `expectedStdin`, `readTasksYAML`, `taskStates`, `taskErrors`, `assertNoTmpFile`, `runDryRun`, `fakeClaudeDir`.
- Produces: nothing later tasks use.

This module is black-box: no imports from the main module. The e2e container is Alpine, so gates here use only POSIX `sh` builtins plus `touch`, `echo`, `test`.

- [ ] **Step 1: Let the fixture read gates**

In `tests/e2e/claude_helpers_test.go`, replace the `taskFixture` type with:

```go
type gateFixture struct {
	Cmd string `yaml:"cmd"`
}

type taskFixture struct {
	ID     int           `yaml:"id"`
	Name   string        `yaml:"name"`
	Prompt string        `yaml:"prompt"`
	State  string        `yaml:"state"`
	Error  string        `yaml:"error,omitempty"`
	Gates  []gateFixture `yaml:"gates,omitempty"`
}
```

- [ ] **Step 2: Write the gate tests**

Create `tests/e2e/gates_test.go`:

```go
package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers gates (ADR-013): commands in a task's gates list that
// gralph runs itself, through sh, after the session reports completed. A
// task is completed only when every gate exits zero, and gates are never
// sent to claude.

func TestGates_PassingGatesCompleteTheTask(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	marker := filepath.Join(dir, "gate-ran")

	tasksYAML := fmt.Sprintf(`tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: echo gate-output
      - cmd: touch '%s'
`, marker)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, "gate: echo gate-output\ngate-output\n")
	assert.FileExists(t, marker)

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 1)
	assert.Equal(t, expectedStdin("Body.", 1, "First task", "Do the first thing."), records[0].Stdin,
		"gates must never be sent to claude")

	after := readTasksYAML(t, tasksPath)
	require.Len(t, after.Tasks, 1)
	assert.Equal(t, "completed", after.Tasks[0].State)
	assert.Equal(t, []gateFixture{{Cmd: "echo gate-output"}, {Cmd: "touch '" + marker + "'"}}, after.Tasks[0].Gates,
		"gates must survive the save")
	assertNoTmpFile(t, tasksPath)
}

func TestGates_FailingGateFailsTheTaskAndBlocksTheNextRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	never := filepath.Join(dir, "never")

	tasksYAML := fmt.Sprintf(`tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: test "a" = "b"
      - cmd: touch '%s'
  - id: 2
    name: Second task
    prompt: Do the second thing.
`, never)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	recordFile := filepath.Join(dir, "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{"FAKECLAUDE_RECORD_FILE": recordFile})
	args := []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}

	res := runGralph(t, 15*time.Second, args, env)

	const gateErr = `gate "test \"a\" = \"b\"" failed: exit status 1`
	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: "+gateErr)
	assert.NoFileExists(t, never, "gates after a failed gate must not run")
	require.Len(t, readFakeClaudeRecords(t, recordFile), 1, "expected the loop to stop before the second task")

	after := readTasksYAML(t, tasksPath)
	assert.Equal(t, "failed", taskStates(after)[1])
	assert.Equal(t, gateErr, taskErrors(after)[1])
	assert.Equal(t, "pending", taskStates(after)[2])
	assertNoTmpFile(t, tasksPath)

	// A gate failure is an ordinary failed task: the saved file must load
	// again, and the next run must refuse to start.
	again := runGralph(t, 15*time.Second, args, env)
	require.NotEqual(t, 0, again.exitCode, "stdout:\n%s\nstderr:\n%s", again.stdout, again.stderr)
	assert.Contains(t, again.stdout, "Some tasks failed previous runs:")
	assert.Len(t, readFakeClaudeRecords(t, recordFile), 1, "a blocked run must not invoke claude")
}

func TestGates_SkippedWhenTheSessionFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	promptPath := writePrompt(t, dir, "Body.\n")
	marker := filepath.Join(dir, "gate-ran")

	tasksYAML := fmt.Sprintf(`tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: touch '%s'
`, marker)
	tasksPath := writeTasksYAML(t, dir, tasksYAML)

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKE_CLAUDE_OUTPUT": `{"state":"failed","error":"something broke"}` + "\n",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "task 1: First task failed: something broke")
	assert.NotContains(t, res.stdout, "gate:")
	assert.NoFileExists(t, marker, "a failed session must not run gates")
	assert.Equal(t, "something broke", taskErrors(readTasksYAML(t, tasksPath))[1])
}
```

- [ ] **Step 3: Add the dry-run tests**

Append to `tests/e2e/dry_run_test.go`:

```go
func TestDryRun_ValidGatesAreNotRun(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "gate-ran")
	tasksYAML := "tasks:\n  - id: 1\n    name: First task\n    prompt: p\n    gates:\n      - cmd: touch '" + marker + "'\n"

	res, tasksPath := runDryRun(t, tasksYAML, func(p string) []string {
		return []string{"-t", p, "--dry-run"}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, tasksPath+" is valid\n")
	assert.NoFileExists(t, marker, "a dry run must never run a gate")
}

func TestDryRun_InvalidGates(t *testing.T) {
	t.Parallel()
	tasksYAML := "tasks:\n  - id: 1\n    name: First task\n    prompt: p\n    gates:\n      - command: go test ./...\n"

	res, tasksPath := runDryRun(t, tasksYAML, func(p string) []string {
		return []string{"--tasks=" + p, "--dry-run"}
	})

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "tasks[0] (id 1): gates[0]: command: unknown key; a gate has only cmd")
	assert.NotContains(t, res.stdout, tasksPath+" is valid")
}
```

`dry_run_test.go` already imports `path/filepath`, `assert`, and `require`; add nothing unless the compiler asks.

- [ ] **Step 4: Check the module compiles**

Run: `cd tests/e2e && go vet ./... && cd ../..`
Expected: exits 0. Do not run the e2e tests natively.

- [ ] **Step 5: Run the quality gates and commit**

Run: `make test && make analyze && make build`
Expected: all exit 0. The e2e output inside `make build` shows `TestGates_PassingGatesCompleteTheTask`, `TestGates_FailingGateFailsTheTaskAndBlocksTheNextRun`, `TestGates_SkippedWhenTheSessionFails`, `TestDryRun_ValidGatesAreNotRun`, and `TestDryRun_InvalidGates` passing.

```bash
git add tests/e2e/claude_helpers_test.go tests/e2e/gates_test.go tests/e2e/dry_run_test.go
git commit -m "3: Cover gates end to end"
```

---

### Task 4: Teach the skill about gates

**Files:**
- Modify: `skills/gralph-docs-writer/SKILL.md`
- Modify: `skills/gralph-docs-writer/templates/tasks_template.yaml`

**Interfaces:**
- Consumes: the field rules from Task 1 (`gates` is a sequence of mappings whose only key is `cmd`, a nonblank string) and the run rules from Task 2.
- Produces: nothing later tasks use.

The skill folder is embedded in the binary, so these edits change the binary's skill hash. That is expected; do not install the skill.

- [ ] **Step 1: Explain gates in "How Gralph uses the pair"**

In `SKILL.md`, after the paragraph that ends `resets its `state` to `pending` by hand.`, add this paragraph:

```markdown
A task may also list `gates`: commands Gralph runs itself, in order, after a
session that reported `completed`. The task is `completed` only when every gate
exits zero; the first gate that fails marks the task `failed` and stops the
run. Gates are never sent to the session, so the prompt must still tell Claude
what to verify.
```

- [ ] **Step 2: Tie the agreed quality gates to the field**

In step 3 of "Generate the pair", replace the last sentence

```markdown
   Every task's Verification lists the agreed gates, plus its own
   task-specific checks.
```

with

```markdown
   Every task's Verification lists the agreed gates, plus its own
   task-specific checks, so Claude runs them. The same commands also go in
   each task's `gates` list (step 5), so Gralph checks them itself.
```

- [ ] **Step 3: Add the field rules**

In step 5, replace

```markdown
   write it; preserve it if present when updating an existing file. Write no
   other keys: Gralph ignores them when reading and drops them the first time
   it saves the file.
```

with

```markdown
   write it; preserve it if present when updating an existing file. The
   optional `gates` field is a sequence of mappings, each with exactly one
   key, `cmd`, a nonblank string holding one shell command; any other key in
   a gate makes the file invalid. Gralph runs each `cmd` through `sh` from the
   directory it was started in and judges it only by its exit code. Write
   commands that exit non-zero when the check fails
   (`test -z "$(gofmt -l .)"`, not `gofmt -w .`), that need no input, and that
   finish on their own: there is no timeout. Give every task the agreed
   quality gates plus any task-specific check that is a plain command; leave
   `gates` out when a task has nothing to check by command. Write no other
   task keys: Gralph ignores them when reading and drops them the first time
   it saves the file.
```

- [ ] **Step 4: Add gates to the template**

In `templates/tasks_template.yaml`, add this line to the header comment, after the `Leave `state` empty` line:

```yaml
# - `gates` are run by Gralph itself after the session: one command per `cmd`.
#   Repeat the agreed quality gates here and in the prompt's Verification.
#   Drop `gates` when the task has no command to check.
```

and append to the task entry, after the `prompt` block, at the same indent as `prompt:`:

```yaml
    gates:
      - cmd: "GENERATE_AGREED_QUALITY_GATE_COMMAND"
      - cmd: "GENERATE_TASK_SPECIFIC_CHECK_COMMAND_OR_DROP_THIS_ENTRY"
```

- [ ] **Step 5: Check the template against the real parser**

Run, from the repository root:

```bash
make local
CHECK_HOME="$(mktemp -d)"
HOME="$CHECK_HOME" .bin/local/gralph --install-skill
HOME="$CHECK_HOME" .bin/local/gralph -t skills/gralph-docs-writer/templates/tasks_template.yaml --dry-run
rm -rf "$CHECK_HOME"
```

Expected: the dry run prints the table with one `PENDING` row and `skills/gralph-docs-writer/templates/tasks_template.yaml is valid`, exit 0. The temporary `HOME` keeps the real `~/.claude/skills` untouched.

Run: `grep -c 'gates' skills/gralph-docs-writer/SKILL.md`
Expected: 5 or more.

- [ ] **Step 6: Run the quality gates and commit**

Run: `make test && make analyze && make build`
Expected: all exit 0.

```bash
git add skills/gralph-docs-writer/SKILL.md skills/gralph-docs-writer/templates/tasks_template.yaml
git commit -m "4: Teach the docs-writer skill about gates"
```

---

### Task 5: User and contributor docs

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/architecture/05_deployment_architecture.md`

**Interfaces:**
- Consumes: the shipped behavior from Tasks 1 to 4.
- Produces: nothing.

`docs/architecture/00_overview.md`, `01_requirements.md`, `02_architectural_decisions.md`, and `03_system_architecture.md` already describe gates (they are the spec). Read them against the code; change them only where the code differs, and bump that file's metadata if you do.

- [ ] **Step 1: README, the task file**

In "The task file", change the sentence ending `and a `prompt`. `state` is optional:` so it ends `and a `prompt`. `state` and `gates` are optional:`.

In the YAML example, add to task 1, after its `prompt` block:

```yaml
    gates:
      - cmd: go test ./internal/widget/...
```

Add this bullet after the bullet about the `error` field:

```markdown
- `gates` is a list of commands gralph runs itself once the task's session says
  it's done (see [Gates](#gates)). Each entry has exactly one key, `cmd`.
```

- [ ] **Step 2: README, how it works**

Replace the bullet

```markdown
- If that line is valid JSON with `state: "completed"` **and** the session
  exits zero, the task is `completed` and gralph saves the file.
```

with

```markdown
- If that line is valid JSON with `state: "completed"` **and** the session
  exits zero, gralph runs the task's [gates](#gates), if it has any. When they
  all pass, or there are none, the task is `completed` and gralph saves the
  file.
```

Add this subsection immediately before `### The full-screen view`:

```markdown
### Gates

Claude saying a task is done isn't proof. A task can list `gates`: commands
gralph runs itself once the session exits zero and reports `completed`.

- Gates run in file order, one at a time, through `sh`, from the directory you
  started gralph in. Pipes and other shell syntax work.
- A gate passes when it exits zero. Gralph doesn't read its output.
- If every gate passes, the task is `completed`. The first gate that fails
  marks the task `failed` with `gate "<cmd>" failed: <exit status>` as its
  error, the rest of its gates are skipped, and the run stops like any other
  failure.
- Gates don't run when the session itself failed.
- Claude never sees the gates. If you want Claude to run the same checks
  before it finishes (you do), say so in the prompt.

In plain mode gralph prints `gate: <cmd>` before each gate, and the gate's
output passes straight through. In the full-screen view each gate shows up in
the Claude activity pane as `→ gate <cmd>` followed by its output.

A few things to know:

- There's no timeout. A gate that never exits hangs the run until you stop it.
- Write gates that check instead of fix: `test -z "$(gofmt -l .)"`, not
  `gofmt -w .`. Nothing commits what a gate changes.
- Resetting a gate-failed task to `pending` runs its whole session again, not
  just the gates. Setting it to `completed` by hand skips them.
- `--dry-run` checks that `gates` is well formed. It never runs a gate.
```

- [ ] **Step 3: README, the view and the considerations**

In "The full-screen view", change the Claude activity bullet's list `Claude's text, one `→ <tool> <target>` line per tool call, and anything on stderr.` to `Claude's text, one `→ <tool> <target>` line per tool call, anything on stderr, and then each gate and its output.`

In "Key Considerations":

- In the **Only run task files you trust** bullet, append the sentence: `Gate commands are run by gralph itself through `sh`, with no sandbox.`
- In the **Ctrl-C stops the run cleanly** bullet, change `stops the current Claude session and the run` to `stops the current Claude session or gate and the run`.

- [ ] **Step 4: CLAUDE.md**

In the `internal/looper` list, add this bullet after the bullet that begins `Outcome comes from the last non-blank line`:

```markdown
  - Gates (ADR-013): `runTask` runs the session and, only when it returned `completed`, `runGates` (`gates.go`). Each `task.Gates` command runs in file order as `sh -c <cmd>` (that call carries the one approved `// #nosec G204`; see Conventions), no stdin, in its own process group. The first non-zero exit makes the task `failed` with `gate "<cmd>" failed: <exit error>` and skips the rest. Plain mode prints `gate: <cmd>` and passes the gate's output through; the stream path reports `→ gate <first line of cmd>` and each output line as `Activity`. Gates are never sent to Claude: the wire contract above is unchanged.
```

In the `Cancelling `ctx`` bullet, change it to read: `Cancelling `ctx` leaves the task's state and the file untouched, whether a session or a gate was running.`

In the `internal/tasks` bullet, after `` `state`/`error` strings) before decoding it ``, add: ``; the optional `gates` must be a sequence of mappings whose only key is `cmd`, a nonblank string (errors read `tasks[<index>] (id <id>): gates[<j>]: ...`), stored unaltered and written back by `SaveTasks` ``. Keep the sentence grammatical.

- [ ] **Step 5: Deployment doc testing sections**

In `docs/architecture/05_deployment_architecture.md`:

- Set the metadata to `Version: v04`, `Date: 2026-09-30`, `Notes: Testing strategy now covers gates (ADR-013).`
- Under "E2E Tests", in the `Tests cover:` list, add: `  - Gates: passing gates, a failing gate (and the blocked next run), gates skipped after a failed session, dry-run gate validation`
- Under "Test Coverage", add: `- Gates: parse rules and errors, file order, first failure stops the rest, shell syntax and empty stdin, cancel during a gate, plain and stream output`

- [ ] **Step 6: Check the docs against the code**

Run: `grep -n 'gate' README.md CLAUDE.md docs/architecture/05_deployment_architecture.md | wc -l`
Expected: 15 or more.

Run: `grep -rn 'gate: \|→ gate \|nosec G204\|unknown key' internal/looper/gates.go internal/tasks/task.go`
Expected: the strings the docs quote (`gate: `, `→ gate `, `#nosec G204`, `unknown key; a gate has only cmd`) appear in the code as documented. Fix the docs, not the code, if they differ.

- [ ] **Step 7: Run the quality gates and commit**

Run: `make test && make analyze && make build`
Expected: all exit 0.

```bash
git add README.md CLAUDE.md docs/architecture/05_deployment_architecture.md
git commit -m "5: Document gates"
```

(Add any `docs/architecture/0[0-3]_*.md` file you had to correct in Step 6 to the same commit.)
