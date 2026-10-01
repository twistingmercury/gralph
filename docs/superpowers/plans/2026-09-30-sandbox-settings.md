# Sandboxed Sessions (ADR-014) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A gralph run must be given either `--sandbox-settings <path>` (sessions run in Claude Code's sandbox) or `--skip-permissions` (today's `--dangerously-skip-permissions`, on the user's head); there is no default.

**Architecture:** `internal/looper` gains one small file that turns a Claude Code settings file into the claude flags for a sandboxed session, forcing three sandbox keys. Those flags (or the bypass flag) are picked once in `cmd/main` and threaded as a `sessionArgs []string` through `Start`/`Run`/`tui.Run` down to `claudeCmd`, the one place the argv is built. Nothing else about a session changes: stdin, outcome rule, process handling, and gates stay as they are.

**Tech Stack:** Go (module in `src/`), `encoding/json` from the standard library, `github.com/spf13/pflag`, `github.com/stretchr/testify`. No new dependencies.

**Spec:** `docs/architecture/02_architectural_decisions.md`, ADR-014. Read it before starting; this plan argues from it.

## Global Constraints

- The Go module is in `src/`; run go commands from `src/` or use the root `Makefile`. `tests/e2e` is a separate module.
- Tests use `github.com/stretchr/testify`: `require` for preconditions, `assert` for checks.
- Never add `// #nosec` or `//nolint`, with one exception the owner approved on 2026-09-30: if `gosec` reports G204 on the claude command in `claudeCmd`, put a single `// #nosec G204` with its reason on the offending line (Task 2, Step 5). That approval covers that finding on that line only. For any other finding from `gosec` or `golangci-lint`, stop and report it; do not suppress, and do not restructure code just to hide it from the scanner.
- Code style, for every line written or moved: return early and keep the happy path at the left margin; a blank line after every `if` block (except before a closing `}` or an `else`); callbacks longer than a line or two are named functions; comments say why, not what.
- No new dependencies, no new packages, no abstraction beyond what this plan names.
- Unix only. No Windows branches.
- `looper` must not import `tui` or Bubble Tea; `tasks` imports neither.
- Exact strings from the spec, copied verbatim:
  - Sandboxed argv tail: `--permission-mode acceptEdits --settings <merged JSON>`
  - Bypass argv tail: `--dangerously-skip-permissions`
  - Forced keys: `sandbox.enabled: true`, `sandbox.allowUnsandboxedCommands: false`, `sandbox.failIfUnavailable: true`
  - `error: pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox`
  - `error: --sandbox-settings and --skip-permissions cannot be used together`
  - Settings file errors start `--sandbox-settings: `
  - Dry-run line: `sandbox settings: <path>`
- The stdin wire contract (`fmt.Sprintf("%s\n\n%s\n", prompt, task.String())`) must not change.
- Docs under `docs/architecture/` are edited in place with `Version`/`Date`/`Notes` bumped; never create `_vNN` copies.
- Commit after each task. Nobody but the owner pushes or tags.
- Who does what: Tasks 1–3 go to the `go software engineer`; Task 4 to the `technical writer` (README via the `/readme-writer` skill conventions, architecture docs in place).

## Review Focus

1. **A settings file that is valid JSON but not an object** (`null`, `[]`, `"x"`, or `"sandbox": null` / `"sandbox": []`): gralph must reject it with a `--sandbox-settings:` error, not crash or pass `null` to Claude. Pinned in Task 1.
2. **A settings file that sets the forced keys the other way** (`"enabled": false`, `"allowUnsandboxedCommands": true`, `"failIfUnavailable": false`): gralph's values must win. Pinned in Task 1.
3. **Everything gralph does not force must survive untouched**, including nested objects, other top-level keys, and numbers too big for a float64. Pinned in Task 1.
4. **A path that is missing or is a directory**: exit 1 with the path in the message, and claude never starts. Pinned in Tasks 1 and 3.
5. **The settings file on disk must be byte-for-byte unchanged after a run**, and a `--dry-run` with both flags must still be rejected. Pinned in Task 3.

---

### Task 1: Build the session flags from a settings file

**Files:**
- Create: `src/internal/looper/sandbox.go`
- Test: `src/internal/looper/sandbox_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func BypassArgs() []string` returns `[]string{"--dangerously-skip-permissions"}`.
  - `func SandboxArgs(path string) ([]string, error)` returns `[]string{"--permission-mode", "acceptEdits", "--settings", <merged JSON>}`; every error starts `--sandbox-settings: `.

- [ ] **Step 1: Write the failing tests**

Create `src/internal/looper/sandbox_test.go`:

```go
package looper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSettings(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestBypassArgs(t *testing.T) {
	assert.Equal(t, []string{"--dangerously-skip-permissions"}, BypassArgs())
}

func TestSandboxArgs_ForcesSandboxKeys(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty object",
			in:   `{}`,
			want: `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`,
		},
		{
			name: "forced keys set the other way",
			in:   `{"sandbox":{"enabled":false,"allowUnsandboxedCommands":true,"failIfUnavailable":false}}`,
			want: `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`,
		},
		{
			name: "everything else passes through",
			in: `{
  "permissions": {"deny": ["WebFetch"]},
  "sandbox": {
    "filesystem": {"denyRead": ["~/"], "allowWrite": ["/tmp/build"]},
    "network": {"allowedDomains": ["proxy.golang.org"], "httpProxyPort": 12345678901234567890}
  }
}`,
			want: `{"permissions":{"deny":["WebFetch"]},"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/"],"allowWrite":["/tmp/build"]},"network":{"allowedDomains":["proxy.golang.org"],"httpProxyPort":12345678901234567890}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSettings(t, tt.in)

			args, err := SandboxArgs(path)
			require.NoError(t, err)
			assert.Equal(t, []string{"--permission-mode", "acceptEdits", "--settings", tt.want}, args)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.in, string(after), "the settings file must not be written")
		})
	}
}

func TestSandboxArgs_RejectsBadFiles(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "empty file", in: ``, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "not JSON", in: `sandbox: true`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "null", in: `null`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "array", in: `[]`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "string", in: `"x"`, wantErr: "--sandbox-settings: must be a JSON object"},
		{name: "sandbox null", in: `{"sandbox":null}`, wantErr: "--sandbox-settings: sandbox: must be a JSON object"},
		{name: "sandbox array", in: `{"sandbox":[]}`, wantErr: "--sandbox-settings: sandbox: must be a JSON object"},
		{name: "sandbox bool", in: `{"sandbox":true}`, wantErr: "--sandbox-settings: sandbox: must be a JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := SandboxArgs(writeSettings(t, tt.in))
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, args)
		})
	}
}

func TestSandboxArgs_UnreadablePath(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "missing.json"), dir} {
		args, err := SandboxArgs(path)
		require.ErrorContains(t, err, "--sandbox-settings: file \""+path+"\" is not accessible")
		assert.Nil(t, args)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd src && go test ./internal/looper -run 'TestBypassArgs|TestSandboxArgs' -count=1`
Expected: build failure, `undefined: BypassArgs` and `undefined: SandboxArgs`.

- [ ] **Step 3: Write the implementation**

Create `src/internal/looper/sandbox.go`:

```go
package looper

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BypassArgs returns the claude flags for a session with no sandbox and no
// permission checks: what every session ran with before ADR-014.
func BypassArgs() []string {
	return []string{"--dangerously-skip-permissions"}
}

// SandboxArgs reads the Claude Code settings file at path and returns the
// claude flags for a sandboxed session. The settings travel inline, so there
// is no temporary file to clean up, and the file at path is never written.
func SandboxArgs(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("--sandbox-settings: file %q is not accessible: %w", path, err)
	}

	settings, err := forceSandbox(data)
	if err != nil {
		return nil, fmt.Errorf("--sandbox-settings: %w", err)
	}

	return []string{"--permission-mode", "acceptEdits", "--settings", settings}, nil
}

// forceSandbox returns the settings in data with the three sandbox keys a
// file must not be able to weaken set: the sandbox on, no way for the session
// to ask its way out, and no carrying on unsandboxed when it cannot start.
// Values are kept as raw JSON so everything else reaches Claude as written.
func forceSandbox(data []byte) (string, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("must be a JSON object: %w", err)
	}

	if settings == nil {
		return "", errors.New("must be a JSON object")
	}

	sandbox := map[string]json.RawMessage{}
	if raw, ok := settings["sandbox"]; ok {
		if err := json.Unmarshal(raw, &sandbox); err != nil || sandbox == nil {
			return "", errors.New("sandbox: must be a JSON object")
		}
	}

	sandbox["enabled"] = json.RawMessage("true")
	sandbox["allowUnsandboxedCommands"] = json.RawMessage("false")
	sandbox["failIfUnavailable"] = json.RawMessage("true")

	forced, err := json.Marshal(sandbox)
	if err != nil {
		return "", err
	}

	settings["sandbox"] = forced
	merged, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}

	return string(merged), nil
}
```

Why the nil checks: `json.Unmarshal` of `null` into a map succeeds and leaves the map nil, so `null` and `"sandbox": null` need their own rejection.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd src && go test ./internal/looper -run 'TestBypassArgs|TestSandboxArgs' -count=1`
Expected: PASS. If the "everything else passes through" case fails only on key order inside `filesystem` or `network`, the implementation is decoding nested values instead of keeping them raw; fix the implementation, not the expected string.

- [ ] **Step 5: Commit**

```bash
git add src/internal/looper/sandbox.go src/internal/looper/sandbox_test.go
git commit -m "Build claude's session flags from a sandbox settings file"
```

---

### Task 2: Thread the session flags down to `claudeCmd`

After this task the claude argv comes from the caller. `cmd/main` passes `looper.BypassArgs()` for now, so the binary behaves exactly as before; Task 3 adds the flags.

**Files:**
- Modify: `src/internal/looper/looper.go` (`Start`, `DryRun`, `Run`, `runLoop`, `runTask`, `runTaskPlain`, `claudeCmd`)
- Modify: `src/internal/looper/stream.go` (`runTaskStream`, line ~98)
- Modify: `src/internal/tui/run.go` (`Run`, lines 15 and 24)
- Modify: `src/cmd/main/main.go` (the three call sites only)
- Test: `src/internal/looper/looper_test.go`, `src/internal/looper/stream_run_test.go`, and every `_test.go` in `src/internal/looper` and `src/internal/tui` that calls the changed functions

**Interfaces:**
- Consumes: `BypassArgs() []string` from Task 1.
- Produces (exact signatures later tasks call):
  - `func Start(ctx context.Context, promptFile, tasksFile, gateTimeout string, sessionArgs []string) error`
  - `func DryRun(w io.Writer, tasksFile, gateTimeout, sandboxFile string) error`
  - `func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, report func(Event)) error`
  - `tui.Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, opts ...tea.ProgramOption) (exitCode int, summary string, err error)`

- [ ] **Step 1: Write the failing tests**

In `src/internal/looper/looper_test.go`, add next to `useFakeClaude`:

```go
// bypass is the session flags most tests run with; the fake claude ignores
// them, so only the argv tests care which flags they are.
var bypass = BypassArgs()
```

Add to `src/internal/looper/looper_test.go`:

```go
func TestRunTaskPlain_ArgvIsPrintThenSessionArgs(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	t.Setenv("FAKE_CLAUDE_ARGS", argsPath)

	sessionArgs := []string{"--permission-mode", "acceptEdits", "--settings", `{"sandbox":{"enabled":true}}`}
	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "Do the first thing."}}}
	require.NoError(t, runLoop(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), "", sessionArgs, nil))

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
		"sandbox settings: sandbox.json\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, out.String())
}
```

Add to `src/internal/looper/stream_run_test.go`, after `TestRunTaskStream_ArgvAndStdin`:

```go
func TestRunTaskStream_ArgvWithSandboxArgs(t *testing.T) {
	useFakeClaude(t)
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	t.Setenv("FAKE_CLAUDE_ARGS", argsPath)

	sessionArgs := []string{"--permission-mode", "acceptEdits", "--settings", `{"sandbox":{"enabled":true}}`}
	tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: "First", Prompt: "Do the first thing."}}}
	var rec recorder
	require.NoError(t, Run(context.Background(), "prompt", tl, filepath.Join(dir, "tasks.yaml"), "", sessionArgs, rec.report))

	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, "--print --output-format stream-json --verbose --permission-mode acceptEdits --settings {\"sandbox\":{\"enabled\":true}}\n", string(args))
}
```

Then update every existing call so the package compiles against the new signatures. The rule, applied at each site the compiler reports:

- `Start(ctx, promptPath, tasksPath, X)` becomes `Start(ctx, promptPath, tasksPath, X, bypass)`
- `runLoop(ctx, p, tl, tasksPath, X, R)` becomes `runLoop(ctx, p, tl, tasksPath, X, bypass, R)`
- `Run(ctx, p, tl, tasksPath, X, R)` becomes `Run(ctx, p, tl, tasksPath, X, bypass, R)`
- `DryRun(w, tasksPath, X)` becomes `DryRun(w, tasksPath, X, "")`
- In `src/internal/tui/run_test.go` (`startRun`), `Run(ctx, "prompt", &tl, tasksPath, "",` becomes `Run(ctx, "prompt", &tl, tasksPath, "", looper.BypassArgs(),` (add the `looper` import if the file does not have it).

where `X` is the existing gate-timeout argument and `R` the existing report argument. The existing assertion in `TestRunTaskStream_ArgvAndStdin` (`"--print --output-format stream-json --verbose --dangerously-skip-permissions\n"`) stays as it is: with `bypass` it must still hold.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd src && go vet ./internal/... ./cmd/...`
Expected: compile errors such as `too many arguments in call to runLoop` and `too many arguments in call to DryRun`.

- [ ] **Step 3: Write the implementation**

In `src/internal/looper/looper.go`:

`Start`: new signature and doc line, and pass the flags on.

```go
// Start runs the loop in plain mode. gateTimeout is the --gate-timeout value as
// given, or "" when the flag was not passed. sessionArgs is the claude flags
// that set what a session may do: SandboxArgs or BypassArgs.
func Start(ctx context.Context, promptFile, tasksFile, gateTimeout string, sessionArgs []string) error {
```

and in its body:

```go
	if err := Run(ctx, prompt, tasklist, tasksFile, gateTimeout, sessionArgs, nil); err != nil {
```

`DryRun`: new parameter, and one line before the final one.

```go
// DryRun validates tasksFile with the same checks Start uses and reports on
// it to w without launching claude or writing any file. It also lists the
// timeout each gate would run under, given gateTimeout as for Start, and
// names sandboxFile, when not empty, as the settings a run would use.
func DryRun(w io.Writer, tasksFile, gateTimeout, sandboxFile string) error {
	tasklist, err := LoadTasksReport(w, tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		return nil
	}

	if err != nil {
		return err
	}

	PrintTasks(w, tasklist)
	printGateLimits(w, tasklist, gateTimeout)
	if sandboxFile != "" {
		_, _ = fmt.Fprintf(w, "sandbox settings: %s\n", sandboxFile)
	}

	_, _ = fmt.Fprintf(w, "%s is valid\n", tasksFile)
	return nil
}
```

`Run`: add to the end of its doc comment `sessionArgs is as for Start.` and change the signature and the call:

```go
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, report func(Event)) error {
	err := runLoop(ctx, prompt, tl, tasksFile, gateTimeout, sessionArgs, report)
```

`runLoop`: signature and the `runTask` call only.

```go
func runLoop(ctx context.Context, p string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, report func(Event)) error {
```

```go
		state, errMsg, err := runTask(ctx, p, *task, gateTimeout, sessionArgs, report)
```

`runTask`:

```go
func runTask(ctx context.Context, p string, task tasks.Task, gateTimeout string, sessionArgs []string, report func(Event)) (state, errMsg string, err error) {
	if report == nil {
		state, errMsg, err = runTaskPlain(ctx, p, task, sessionArgs)
	} else {
		state, errMsg, err = runTaskStream(ctx, p, task, sessionArgs, report)
	}
```

`runTaskPlain`:

```go
func runTaskPlain(ctx context.Context, p string, task tasks.Task, sessionArgs []string) (state, errMsg string, err error) {
	cmd, prompt := claudeCmd(ctx, p, task, sessionArgs, false)
```

`claudeCmd`, replaced whole. Remove the `slices` import from `looper.go` if nothing else in the file uses it.

```go
// claudeCmd builds the claude command for task, in its own process group
// with the combined prompt on stdin, and also returns that prompt text. The
// argv is --print, then the stream-json flags when stream is set, then
// sessionArgs.
func claudeCmd(ctx context.Context, p string, task tasks.Task, sessionArgs []string, stream bool) (*exec.Cmd, string) {
	prompt := fmt.Sprintf("%s\n\n%s\n", p, task.String())
	cmd := exec.CommandContext(ctx, "claude", "--print")
	if stream {
		cmd.Args = append(cmd.Args, "--output-format", "stream-json", "--verbose")
	}

	cmd.Args = append(cmd.Args, sessionArgs...)
	configureProcessTree(cmd)
	cmd.Stdin = strings.NewReader(prompt)
	return cmd, prompt
}
```

In `src/internal/looper/stream.go`:

```go
func runTaskStream(ctx context.Context, p string, task tasks.Task, sessionArgs []string, report func(Event)) (state, errMsg string, err error) {
	cmd, _ := claudeCmd(ctx, p, task, sessionArgs, true)
```

In `src/internal/tui/run.go`, line 15 and line 24:

```go
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, opts ...tea.ProgramOption) (exitCode int, summary string, err error) {
```

```go
		_ = looper.Run(runCtx, prompt, tl, tasksFile, gateTimeout, sessionArgs, func(e looper.Event) { p.Send(e) })
```

If `Run` in `run.go` has a doc comment that lists its parameters, add `sessionArgs is as for looper.Start.` to it.

In `src/cmd/main/main.go`, only so the binary builds and behaves as before (Task 3 replaces these):

```go
		if err := looper.DryRun(os.Stdout, *tasksFlag, *gateTimeoutFlag, ""); err != nil {
```

```go
	if err := looper.Start(ctx, *promptFlag, *tasksFlag, *gateTimeoutFlag, looper.BypassArgs()); err != nil {
```

```go
	code, summary, err := tui.Run(ctx, prompt, tasklist, tasksPath, *gateTimeoutFlag, looper.BypassArgs())
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make test`
Expected: PASS for `./cmd/...` and `./internal/...`, including the three new tests and the unchanged `TestRunTaskStream_ArgvAndStdin`.

- [ ] **Step 5: Run the linters**

Run: `make analyze`
Expected: clean.

If, and only if, `gosec` reports G204 on a line of `claudeCmd`, add the owner-approved suppression to that one line, in the same form as the gate call in `gates.go`, with this reason:

```go
// #nosec G204 -- the binary is the fixed name "claude"; the arguments are gralph's own flags plus the user's sandbox settings as one inline value (ADR-014), and there is no shell
```

Then rerun `make analyze` and confirm it is clean. Say in the task report whether the suppression was needed and which line carries it; Task 4 records it in `CLAUDE.md`. If `gosec` does not flag the code, add nothing. Any other finding: stop and report it.

- [ ] **Step 6: Commit**

```bash
git add src/internal/looper src/internal/tui src/cmd/main/main.go
git commit -m "Pass claude's session flags in from the caller"
```

---

### Task 3: The two flags, and the e2e suite

**Files:**
- Modify: `src/cmd/main/main.go`
- Test: `src/cmd/main/main_test.go`
- Create: `tests/e2e/sandbox_test.go`
- Modify: every `tests/e2e/*_test.go` that starts a real run (listed in Step 5)
- Modify: `tests/e2e/testdata/fakeclaude/main.go` (header comment only)

**Interfaces:**
- Consumes: `looper.SandboxArgs(path string) ([]string, error)`, `looper.BypassArgs() []string`, and the Task 2 signatures of `looper.Start`, `looper.DryRun`, `tui.Run`.
- Produces: the CLI contract in ADR-014. Nothing later depends on Go names from this task.

- [ ] **Step 1: Write the failing unit test**

Add to `src/cmd/main/main_test.go` (add `os`, `path/filepath`, and `require` imports as needed):

```go
func TestSessionArgs(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0o600))
	sandboxed := []string{"--permission-mode", "acceptEdits", "--settings", `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`}

	tests := []struct {
		name    string
		file    string
		skip    bool
		dryRun  bool
		want    []string
		wantErr string
	}{
		{name: "sandbox file", file: settings, want: sandboxed},
		{name: "skip", skip: true, want: []string{"--dangerously-skip-permissions"}},
		{name: "both", file: settings, skip: true, wantErr: "--sandbox-settings and --skip-permissions cannot be used together"},
		{name: "neither", wantErr: "pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox"},
		{name: "dry run, neither", dryRun: true},
		{name: "dry run, both", file: settings, skip: true, dryRun: true, wantErr: "--sandbox-settings and --skip-permissions cannot be used together"},
		{name: "dry run, sandbox file", file: settings, dryRun: true, want: sandboxed},
		{name: "bad file", file: filepath.Join(t.TempDir(), "missing.json"), wantErr: "--sandbox-settings: file"},
		{name: "dry run, bad file", file: filepath.Join(t.TempDir(), "missing.json"), dryRun: true, wantErr: "--sandbox-settings: file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sessionArgs(tt.file, tt.skip, tt.dryRun)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd src && go test ./cmd/main -run TestSessionArgs -count=1`
Expected: build failure, `undefined: sessionArgs`.

- [ ] **Step 3: Write the implementation**

In `src/cmd/main/main.go`, add two flags to the `var` block:

```go
	sandboxFlag   = pflag.String("sandbox-settings", "", "Path to a Claude Code settings JSON file; sessions run in Claude's sandbox with it. A run needs this or --skip-permissions")
	skipPermsFlag = pflag.Bool("skip-permissions", false, "Run sessions with no sandbox and no permission checks (claude --dangerously-skip-permissions); what they do is on you")
```

Replace `main` (the three call sites from Task 2 change again, and one check is added after the required-flags check):

```go
func main() {
	pflag.Parse()
	checkVersion()
	checkInstallSkill()
	validateGateTimeout()
	plain := isPlain(*dryRunFlag, *noTUIFlag, term.IsTerminal(os.Stdin.Fd()), term.IsTerminal(os.Stdout.Fd()))
	if plain {
		validateRequiredFlags()
	}

	session := validateSessionFlags()

	if err := skillinstall.Check(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dryRunFlag {
		if err := looper.DryRun(os.Stdout, *tasksFlag, *gateTimeoutFlag, *sandboxFlag); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !plain {
		os.Exit(runTUI(ctx, session))
	}

	if err := looper.Start(ctx, *promptFlag, *tasksFlag, *gateTimeoutFlag, session); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

Change `runTUI` to take the flags: its signature becomes `func runTUI(ctx context.Context, session []string) int`, its doc comment gains `session is the claude flags from validateSessionFlags.`, and its `tui.Run` call becomes:

```go
	code, summary, err := tui.Run(ctx, prompt, tasklist, tasksPath, *gateTimeoutFlag, session)
```

Add after `checkGateTimeout`:

```go
// validateSessionFlags exits unless the run was told how far to trust its
// sessions; see sessionArgs.
func validateSessionFlags() []string {
	args, err := sessionArgs(*sandboxFlag, *skipPermsFlag, *dryRunFlag)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	return args
}

// sessionArgs returns the claude flags that set what a session may do. A run
// must choose a sandbox or ask by name to go without one (ADR-014): there is
// no default, so nobody runs unsandboxed by accident. A dry run starts no
// session and needs neither, but its sandbox file is still checked.
func sessionArgs(sandboxFile string, skip, dryRun bool) ([]string, error) {
	if sandboxFile != "" && skip {
		return nil, errors.New("--sandbox-settings and --skip-permissions cannot be used together")
	}

	if sandboxFile != "" {
		return looper.SandboxArgs(sandboxFile)
	}

	if skip {
		return looper.BypassArgs(), nil
	}

	if dryRun {
		return nil, nil
	}

	return nil, errors.New("pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox")
}
```

- [ ] **Step 4: Run the unit tests**

Run: `make test`
Expected: PASS, including `TestSessionArgs`.

- [ ] **Step 5: Update the existing e2e invocations**

Every existing e2e test that starts a real run must now say how; they all get `--skip-permissions`, which keeps their argv assertions (`--print --dangerously-skip-permissions`) true.

Run from the repository root:

```bash
sed -i 's/\[\]string{"--prompt=" + promptPath, "--tasks=" + tasksPath/[]string{"--skip-permissions", "--prompt=" + promptPath, "--tasks=" + tasksPath/' tests/e2e/*_test.go
```

Then make these four edits by hand:

- `tests/e2e/no_tui_test.go`: `[]string{"-p", promptPath, "-t", tasksPath}` becomes `[]string{"--skip-permissions", "-p", promptPath, "-t", tasksPath}`.
- `tests/e2e/skill_check_test.go`: in the `for _, args := range [][]string{dryRun, {"-t", tasksPath, "-p", promptPath}}` line, the second element becomes `{"--skip-permissions", "-t", tasksPath, "-p", promptPath}`.
- `tests/e2e/gralph_test.go`, `TestNonexistentFiles`: add `"--skip-permissions",` as the first argument to `runCLI`.
- `tests/e2e/gralph_test.go`, `TestHelpFlag`: add `"--sandbox-settings", "--skip-permissions"` to the list of flags it checks.

Leave `TestMissingPrompt`, `TestMissingTasks`, `TestMissingBothRequiredFlags`, and every `--dry-run` invocation alone: the required-flags check runs first, and a dry run needs neither flag.

In `tests/e2e/testdata/fakeclaude/main.go`, change the header's second sentence so it no longer claims one fixed argv: replace `gralph always invokes claude with a fixed argv (`--print --dangerously-skip-permissions`) and the assembled prompt on stdin` with `gralph always invokes claude with an argv it builds itself (`--print` plus the session flags) and the assembled prompt on stdin`.

- [ ] **Step 6: Write the new e2e tests**

Create `tests/e2e/sandbox_test.go`:

```go
package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const neitherFlagError = "error: pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox"

const bothFlagsError = "error: --sandbox-settings and --skip-permissions cannot be used together"

// writeSandboxSettings writes a settings fixture and returns its path.
func writeSandboxSettings(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "sandbox.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// sandboxRun is one gralph run over a single valid task, with the fake
// claude recording its argv.
type sandboxRun struct {
	res        gralphResult
	recordFile string
	attemptLog string
	tasksPath  string
}

func runWithFlags(t *testing.T, flags ...string) sandboxRun {
	t.Helper()
	dir := t.TempDir()
	run := sandboxRun{
		recordFile: filepath.Join(dir, "record.ndjson"),
		attemptLog: filepath.Join(dir, "attempts.log"),
		tasksPath:  writeTasksYAML(t, dir, validTasksYAML()),
	}
	promptPath := writePrompt(t, dir, "Follow the runbook.\n")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE":      run.recordFile,
		"FAKECLAUDE_ATTEMPT_LOG_FILE": run.attemptLog,
	})

	args := append([]string{"--prompt=" + promptPath, "--tasks=" + run.tasksPath}, flags...)
	run.res = runGralph(t, 15*time.Second, args, env)
	return run
}

func TestSandbox_ArgvCarriesForcedSettings(t *testing.T) {
	t.Parallel()
	content := `{"sandbox":{"enabled":false,"filesystem":{"denyRead":["~/"]}}}`
	settings := writeSandboxSettings(t, t.TempDir(), content)

	run := runWithFlags(t, "--sandbox-settings="+settings)
	require.Equal(t, 0, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)

	records := readFakeClaudeRecords(t, run.recordFile)
	require.Len(t, records, 1)
	want := []string{
		"--print", "--permission-mode", "acceptEdits", "--settings",
		`{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true,"filesystem":{"denyRead":["~/"]}}}`,
	}
	assert.Equal(t, want, records[0].Argv)
	assert.Equal(t, expectedStdin("Follow the runbook.", 1, "First task", "Do the thing."), records[0].Stdin, "the stdin contract must not change")

	after, err := os.ReadFile(settings)
	require.NoError(t, err)
	assert.Equal(t, content, string(after), "the settings file must not be written")
	assert.Equal(t, "completed", taskStates(readTasksYAML(t, run.tasksPath))[1])
}

func TestRun_NeedsOnePermissionFlag(t *testing.T) {
	t.Parallel()
	settings := writeSandboxSettings(t, t.TempDir(), `{}`)

	tests := []struct {
		name    string
		flags   []string
		wantErr string
	}{
		{name: "neither", wantErr: neitherFlagError},
		{name: "empty sandbox path", flags: []string{"--sandbox-settings="}, wantErr: neitherFlagError},
		{name: "both", flags: []string{"--sandbox-settings=" + settings, "--skip-permissions"}, wantErr: bothFlagsError},
		{name: "both on a dry run", flags: []string{"--sandbox-settings=" + settings, "--skip-permissions", "--dry-run"}, wantErr: bothFlagsError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := runWithFlags(t, tt.flags...)

			require.Equal(t, 1, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
			assert.Contains(t, run.res.stderr, tt.wantErr)
			assert.Equal(t, 0, countAttempts(t, run.attemptLog), "expected claude never invoked")
			assert.Equal(t, "pending", taskStates(readTasksYAML(t, run.tasksPath))[1])
		})
	}
}

func TestSandbox_BadSettingsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "missing", path: filepath.Join(dir, "missing.json"), wantErr: "is not accessible"},
		{name: "directory", path: dir, wantErr: "is not accessible"},
		{name: "not an object", path: writeSandboxSettings(t, t.TempDir(), `null`), wantErr: "must be a JSON object"},
		{name: "sandbox not an object", path: writeSandboxSettings(t, t.TempDir(), `{"sandbox":true}`), wantErr: "sandbox: must be a JSON object"},
	}
	for _, tt := range tests {
		for _, extra := range [][]string{nil, {"--dry-run"}} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				run := runWithFlags(t, append([]string{"--sandbox-settings=" + tt.path}, extra...)...)

				require.Equal(t, 1, run.res.exitCode, "stdout:\n%s\nstderr:\n%s", run.res.stdout, run.res.stderr)
				assert.Contains(t, run.res.stderr, "error: --sandbox-settings: ")
				assert.Contains(t, run.res.stderr, tt.wantErr)
				assert.Equal(t, 0, countAttempts(t, run.attemptLog), "expected claude never invoked")
			})
		}
	}
}

func TestDryRun_NamesSandboxSettings(t *testing.T) {
	t.Parallel()
	settings := writeSandboxSettings(t, t.TempDir(), `{}`)

	res, tasksPath := runDryRun(t, validTasksYAML(), func(p string) []string {
		return []string{"-t", p, "--dry-run", "--sandbox-settings=" + settings}
	})

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	want := "   ID  STATE      NAME\n" +
		"   --  ---------  ----\n" +
		"    1  PENDING    First task\n" +
		"sandbox settings: " + settings + "\n" +
		tasksPath + " is valid\n"
	assert.Equal(t, want, res.stdout)
}
```

Before running, check two things in the existing helpers and adjust the new file to match rather than changing the helpers: that `taskStates` returns a `map[int]string` keyed by task id with a file's omitted state read back as written by gralph (for the "pending" assertions, if an untouched file has no `state` key the map value is `""`; assert on that value instead), and that `expectedStdin`'s first argument is the trimmed shared prompt.

- [ ] **Step 7: Run the e2e suite**

Quick native check (debugging shortcut; `-count=1` is required because Go's test cache does not track the binary):

```bash
make local && cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -count=1 .
```

Expected: PASS.

Then the supported run, in the container:

```bash
make build
```

Expected: lint, govulncheck, gosec, unit tests, cross-compile, and the e2e suite all pass.

- [ ] **Step 8: Commit**

```bash
git add src/cmd/main tests/e2e
git commit -m "Require --sandbox-settings or --skip-permissions for a run"
```

---

### Task 4: Documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/architecture/00_overview.md`, `docs/architecture/01_requirements.md`, `docs/architecture/03_system_architecture.md` (in place; bump `Version`, set `Date` to the day of the edit, replace `Notes` with a one-line summary of this change)
- Modify: `docs/gralph-concept.md` (line 15)
- Check only: `src/skills/gralph-docs-writer/SKILL.md` (its `gralph -t <tasks.yaml> --dry-run` line stays correct; a dry run needs neither flag. Do not edit the skill: changing it changes the skill hash every installed copy is checked against.)

**Interfaces:**
- Consumes: the behavior built in Tasks 1–3 and ADR-014's wording.
- Produces: nothing code depends on.

Write for a reader who has never seen the project: plain, casual, no unexplained terms.

- [ ] **Step 1: README.md**

1. Intro paragraph (line ~14): replace "starting a fresh `claude --print --dangerously-skip-permissions` session for each" with "starting a fresh `claude --print` session for each".
2. Usage example becomes:

   ```bash
   gralph --prompt path/to/prompt.md --tasks path/to/tasks.yaml --sandbox-settings path/to/sandbox.json
   ```

3. Flag table: add two rows after `--tasks`, and realign the table.

   | Flag                 | Required                                          | Description                                                                              |
   | -------------------- | ------------------------------------------------- | ---------------------------------------------------------------------------------------- |
   | `--sandbox-settings` | One of these two for any real run; not both       | Path to a Claude Code settings file; sessions run in Claude's sandbox with it            |
   | `--skip-permissions` | One of these two for any real run; not both       | Run sessions with no sandbox and no permission checks. You're on your own (see below)    |

4. "Checking a task file first": add that `--dry-run` needs neither flag, and that with `--sandbox-settings` it checks the file and prints `sandbox settings: <path>` before the final line.
5. New section `### Sandboxing sessions` under "How it works", before "Gates", covering, in this order:
   - A run needs exactly one of the two flags. With neither: `error: pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox`. With both: `error: --sandbox-settings and --skip-permissions cannot be used together`. Both exit 1 before anything runs.
   - What `--sandbox-settings` does: the file is a normal Claude Code settings file. Gralph reads it once at startup, never writes it, and always sets `sandbox.enabled: true`, `sandbox.allowUnsandboxedCommands: false`, and `sandbox.failIfUnavailable: true` on top, whatever the file says. Sessions run as `claude --print --permission-mode acceptEdits --settings <your settings>`: edits inside the project are allowed, shell commands run inside the sandbox, and anything that would normally stop and ask is refused.
   - A subsection `#### Writing a settings file`, a how-to a first-time user can follow start to finish:
     1. **Start from this file** (a Go project; save it anywhere, for example next to the task file as `sandbox.json`). Say that the paths are from one machine and must be replaced, and that absolute paths are what was tested: write the project and toolchain paths in full.

        ```json
        {
          "sandbox": {
            "filesystem": {
              "denyRead": ["~/"],
              "allowRead": ["/home/you/dev/your-project", "/usr/local/go", "/home/you/go", "/home/you/.cache/go-build"],
              "allowWrite": ["/home/you/.cache/go-build"]
            },
            "network": {
              "allowedDomains": ["proxy.golang.org", "sum.golang.org"],
              "strictAllowlist": true
            }
          }
        }
        ```

     2. **What each part does**, one line each: `denyRead: ["~/"]` hides your home directory (SSH keys, cloud credentials, other projects) from shell commands; `allowRead` lets back in the project and whatever the build tools need to read; `allowWrite` adds places outside the project that commands may write to (the project directory itself is always writable); `allowedDomains` is the list of hosts shell commands may reach, and `strictAllowlist: true` refuses everything else outright. Leave `enabled`, `allowUnsandboxedCommands`, and `failIfUnavailable` out: gralph sets them.
     3. **Find your toolchain's paths.** For Go: `go env GOROOT GOPATH GOCACHE` prints the three paths; all three go in `allowRead` and `GOCACHE` also in `allowWrite`. For any other toolchain give the method, not a made-up list: ask the tool where its install directory and caches are (its `env` or `config` command), allow reading both and writing the cache. Only the Go example was tested; do not print path lists for other toolchains as if they were.
     4. **Try it before a real run.** `gralph -t tasks.yaml --dry-run --sandbox-settings sandbox.json` checks the file is readable JSON (it does not check that the paths are right). Then run a one-task file whose prompt only asks Claude to build and test the project, and read the output.
     5. **Read the failures.** A table of what a too-tight file looks like and what to add, from the spike: `read-only file system` on a path → add that path to `allowWrite`; `No such file or directory` for a file you know exists under your home → add its directory to `allowRead`; `CONNECT tunnel failed, response 403` or a download that hangs up → add the host to `allowedDomains`. Add the narrowest path or host that fixes it and run again.
     6. **Keep it tight.** Do not allow all of `~/` to make an error go away; do not add `excludedCommands` unless a tool truly cannot run sandboxed, since those commands run with no sandbox at all.
     7. **Where the full reference is.** Link Claude Code's sandboxing page, `https://code.claude.com/docs/en/sandboxing`, for every other key; say that the file may hold any Claude Code settings, not only `sandbox`.

   - What it needs: Claude Code's sandbox, which uses bubblewrap and socat on Linux and Seatbelt on macOS. If the sandbox can't start, the session exits non-zero and the task fails like any other; gralph never falls back to running unsandboxed.
   - What it does not cover: gates (gralph runs those itself, unsandboxed); your own Claude settings in `~/.claude/settings.json` and the project's `.claude/settings.json` still apply on top; anything the file itself opens up (`excludedCommands`, wide `allowWrite` paths, allow rules) is opened; network limits are by hostname.
   - `--skip-permissions`, stated plainly: sessions run as `claude --print --dangerously-skip-permissions`, as gralph always did before. A session can then run any command and read, change, or delete anything your user can, and reach anything on the network, with nobody watching. If you pass this flag you are on your own: you are responsible for whatever the sessions do. Use it only somewhere you would not mind losing, such as a container or a throwaway VM.
6. "How it works": in the paragraph about the full-screen view's argv, replace the literal with "adds `--output-format stream-json --verbose` right after `--print`".
7. Key Considerations, "Only run task files you trust": rewrite to say the sandbox limits what a session can reach but the prompt and task files are still code you are about to run; with `--skip-permissions` there is no limit at all; gate commands run through `sh` with no sandbox either way.
8. Key Considerations, "Unix only": add that sandboxed runs need Linux or macOS; on the BSDs only `--skip-permissions` works.
9. Do not change the `Version` line; the owner bumps it when tagging.

- [ ] **Step 2: CLAUDE.md**

- "What this is": the invocation is now `claude --print` plus session flags; a run needs `--sandbox-settings` or `--skip-permissions`.
- Architecture, `cmd/main`: add `validateSessionFlags`/`sessionArgs` (checked after `--gate-timeout` and plain mode's required flags, before the skill check; a dry run needs neither flag; both flags or, on a real run, neither exits 1), and that the result is passed to `looper.Start` and `tui.Run`.
- Architecture, `internal/looper`: add `sandbox.go` (`SandboxArgs` reads the settings file, forces the three keys, returns `--permission-mode acceptEdits --settings <merged JSON>`; `BypassArgs` returns `--dangerously-skip-permissions`); update the `Start`/`Run` signatures quoted there (`sessionArgs` after `gateTimeout`); `claudeCmd` builds `--print`, then the stream flags, then `sessionArgs`; `DryRun` prints `sandbox settings: <path>` when given one.
- "Easy to get wrong": add that every e2e test that starts a real run passes `--skip-permissions` or `--sandbox-settings`, and that neither fake claude enforces anything; they only record the argv.
- Conventions: add "Do not add a default for the two permission flags, and do not make gralph fall back to an unsandboxed run (ADR-014)."
- Conventions, only if Task 2 added the `// #nosec G204` to `claudeCmd`: extend the "Never add `// #nosec`" bullet to name it as the second owner-approved exception (the claude command in `internal/looper/looper.go`, approved 2026-09-30, ADR-014), next to the gate one, and add the same sentence to ADR-014's negative consequences. If it was not needed, change nothing there.

- [ ] **Step 3: Architecture docs and the concept doc**

- `00_overview.md`: the Core Concept step that quotes the claude command now says `claude --print` with the session flags from `--sandbox-settings` or `--skip-permissions`; add one Key Principles line: sessions are sandboxed unless the user asks otherwise by name.
- `01_requirements.md`: add sandboxed sessions to the goals; in Non-Goals keep "sandboxing what a gate command does" and add gralph-managed containers and toolchain detection; update the "Prompt passed via stdin" constraint to the new argv.
- `03_system_architecture.md`: update every quoted argv (mode list, the mermaid diagram's two claude nodes, the Looper component section, the boundary table row "Gralph → Claude (subprocess)"); add the settings file as an input in the trust-boundary section (read once, never written, three keys forced); mention `sandbox.go` in the Looper component breakdown.
- `docs/gralph-concept.md` line 15: `claude --print` session, sandboxed by default.
- Leave ADR-014 itself as written, and leave older ADRs' text alone: they record what was decided at the time.

- [ ] **Step 4: Check the docs against the code**

Run: `grep -rn "dangerously-skip-permissions" README.md CLAUDE.md docs/architecture/00_overview.md docs/architecture/01_requirements.md docs/architecture/03_system_architecture.md docs/gralph-concept.md`
Expected: every remaining hit describes `--skip-permissions` or `BypassArgs`; none says it is what gralph always runs.

Run: `.bin/local/gralph --help 2>&1 | grep -E "sandbox-settings|skip-permissions"`
Expected: both flags listed, matching the README table.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md docs
git commit -m "Document sandboxed sessions and --skip-permissions"
```

---

## Final verification

- [ ] `make analyze` is clean. `git diff develop -- src tests | grep -nE "nosec|nolint"` prints nothing, or exactly the one approved `// #nosec G204` line in `claudeCmd`.
- [ ] `make test` passes.
- [ ] `make build` passes (this is what CI runs, e2e included).
- [ ] By hand, in a real terminal, outside any Claude session, with a real `claude` on `PATH`: run a one-task file with `--sandbox-settings` in both plain (`--no-tui`) and full-screen mode, with a task that tries `cat ~/.ssh/config` and a write outside the project, and confirm both are refused while an edit inside the project works. The spike covered plain `--print` on Linux only; the stream-json argv and macOS were not tested.
- [ ] Release note for the owner: this is a breaking change to the CLI contract, so the next tag is a minor bump (`v0.9.0`) on `develop`'s squash-merge commit after the PR merges.
