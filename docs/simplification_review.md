# Simplification review

Date: 2026-10-01, against `develop` at `02ef8a1` (v0.9.1).

A review-only pass by the code-simplifier agent over every production Go file
under `src/` (16 files; no tests, no `testdata/`, nothing under `tests/`).
Nothing was changed. These are suggestions to pick from.

Every suggestion is a pure refactor: output text, the claude argv, the stdin
wire contract, and how git commands are built all stay exactly as they are.
Line numbers are as of the commit above. Items are ordered most valuable first.

## Summary

| #   | Where                                             | What                                                     | Risk |
| --- | ------------------------------------------------- | -------------------------------------------------------- | ---- |
| 1   | `internal/tui/model.go:89-192`                    | `Update` is ~100 lines with switches nested three deep   | Low  |
| 2   | `cmd/main/main.go` (six places)                   | Print-error-then-exit pair repeated                      | None |
| 3   | `cmd/main/main.go:83-145`                         | `runTUI` does four jobs                                  | Low  |
| 4   | `internal/looper/stream.go`, `gates.go`           | Same pipe-draining block in both                         | None |
| 5   | `internal/tui/run.go:23-33`                       | Inline goroutine literals                                | Low  |
| 6   | `internal/looper/looper.go:130, 164`              | Local named `bytes` shadows the `bytes` package          | None |
| 7   | `internal/looper/stream.go:85`                    | `firstLine` written out by hand                          | None |
| 8   | `internal/looper/gates.go:36-41`                  | `firstLine` is described as gate-only but used widely    | None |
| 9   | `internal/looper/process_tree_unix.go:28-30`      | `terminateProcessTree` sends SIGKILL, name suggests TERM | None |
| 10  | `internal/tui/setup.go:90-104`                    | `submit` mixes loading a file with advancing focus       | Low  |
| 11  | `internal/skillinstall/skillinstall.go`           | Skill path built twice; nested `VERSION` parse           | Low  |
| 12  | `internal/tasks/task.go`                          | Four small cleanups                                      | None |

## Read against "simplify means readable"

The owner's definition of simplify: easy to read, intent explicit, and
duplication is fine when it reads better and the copies can diverge safely.
Fewer lines is not the goal. Sorted by that:

- **Clearly fit** (they make the code easier to read or its intent plainer):
  1, 3 (the `loadGiven` split only), 5, 6, 8, 9, 10.
- **De-duplication, worth doing because the copies must stay in step:**
  2 (one way to fail), 7 (uses the helper that names the intent), 11's
  `skillDir` (`Install` and `Check` must agree on the path).
- **De-duplication, leave alone or decide case by case:**
  - 4: the session and gate drains look the same today but belong to two
    different things, and a logging feature could well treat them differently.
  - 3's cross-package `startError`: couples `main` and `looper` to save three
    lines.
  - 12's `resolveAlias` and `slices.Contains`: shorter, not clearer. The chained
    state check reads fine as it is. The `taskFields` rename is the part of 12
    that helps intent.
- **Optional struct for the run parameters:** it would shorten signatures, but
  explicit parameters show what each function needs. Not a readability win by
  itself.

## Findings

### 1. `tui.Model.Update` is too big

`src/internal/tui/model.go:89-192`

`Update` is about 100 lines with switches nested three deep (message type, then
key, then event kind). That breaks the "move non-trivial case bodies into named
functions" rule. The `RunDone` body alone has its own loop, switch, and early
return.

Split it into value-receiver methods, the same shape `SetupModel.submit` already
uses:

```go
case tea.KeyPressMsg:
    return m.handleKey(msg.String())
case looper.Event:
    return m.handleEvent(msg)
```

Inside `handleEvent`, `RunDone` calls `m.runDone(msg.Err)`. The `TaskStarted`
and `Activity` bodies can become their own small methods too.

Risk: low. `model_test.go` drives `Model` directly; run it.

### 2. Print-and-exit repeated in `main.go`

`src/cmd/main/main.go:47-48, 53-54, 68-69, 174-175, 190-191, 209-210`

The same two lines (`fmt.Fprintf(os.Stderr, "error: %v\n", err)` then
`os.Exit(1)`) appear six times.

Add `func fatal(err error)` that prints and exits, and call it from all six
places. Output is byte-identical. The `defer stop()` path doesn't change, since
`os.Exit` already skips defers today.

Risk: none.

### 3. `runTUI` does four jobs

`src/cmd/main/main.go:83-145`

`runTUI` is about 60 lines: load the given paths, run setup for missing ones,
open the repo, run and print. It declares `var err error` twice in nested
blocks.

Pull out `loadGiven(tasksPath, promptPath string) (string, *tasks.TaskList, error)`,
which returns the error already wrapped: `ErrFailedTasks` passes through bare
and everything else gets `failed to start loop runner: %w`. `runTUI` then prints
`error: %v` once. Optionally pull the setup block out as `askMissing`.

That wrap-unless-`ErrFailedTasks` rule also appears in `looper.Start`
(`looper.go:27-39`). A tiny `startError(err) error` would write it once, but
crossing packages may not be worth it.

Risk: low. Check the exact stderr strings against the e2e and TUI tests.

### 4. Pipe draining duplicated

`src/internal/looper/stream.go:115-119` and `src/internal/looper/gates.go:136-140`

Both have the same "start stderr reader, drain stdout, wait for stderr" block.
Only the stdout callback differs (`stdoutLine` vs `rawLine`).

Add one method to `streamTask`:

```go
// drain reads both pipes to EOF so cmd.Wait never runs with unread output.
func (st *streamTask) drain(stdout, stderr io.Reader, onStdout func([]byte)) {
    stderrDone := make(chan struct{})
    go st.readStderr(stderr, stderrDone)
    readLines(stdout, onStdout)
    <-stderrDone
}
```

Leave the pipe setup alone: the error wrapping differs between the two
(`runTaskStream` prefixes the task and sends `Start` failures through
`finishTask`).

Risk: none.

### 5. Inline goroutine literals in `tui.Run`

`src/internal/tui/run.go:23-33`

Two inline `go func(){...}()` literals of 3-5 lines each, which breaks the
named-callback rule.

Turn the signal forwarder into
`forwardSignal(ctx context.Context, p *tea.Program, progDone <-chan struct{})`.
The loop goroutine captures seven values: either give it a named function too,
or accept it as is, since it's mostly the call.

Keep `func(e looper.Event) { p.Send(e) }` as it is: `p.Send` takes `tea.Msg`,
so it can't be passed directly.

Risk: low.

### 6. Local `bytes` shadows the package

`src/internal/looper/looper.go:130, 135, 164, 169, 173`

`LoadTasks` and `LoadPrompt` name a local `bytes`, which shadows the `bytes`
package this file imports (used at line 279).

Rename the local to `data`, the name `SandboxArgs` already uses.

Risk: none.

### 7. `firstLine` written out by hand

`src/internal/looper/stream.go:85`

`target, _, _ = strings.Cut(target, "\n")` is `firstLine`. Use
`target = firstLine(target)`. The comment above `toolActivity` already gives the
reason.

Risk: none.

### 8. `firstLine` is described too narrowly

`src/internal/looper/gates.go:36-41`

Its comment and parameter name (`cmd`) say it's about gate commands. It's also
used on task names (`commit.go:256`), git stderr (`commit.go:86`), and the
dry-run gate list.

Rename the parameter to `s` and make the comment general, for example "messages
quote only the first line so a multi-line value cannot bury the reason in a
wrapped banner". Optionally move it next to other shared helpers, such as
`looper.go`.

Risk: none.

### 9. `terminateProcessTree` sends SIGKILL

`src/internal/looper/process_tree_unix.go:28-30`

"Terminate" reads like SIGTERM, which is confusing right next to
`stopProcessTree`, which really does send SIGTERM first.

Rename it to `killProcessTree`, or inline
`signalProcessTree(process, syscall.SIGKILL)` at its two call sites.

Also, `configureProcessTree`'s comment (line 11) says it's for "the claude
child", but gates use it too. Say "a claude or gate child".

Risk: none.

### 10. `SetupModel.submit` mixes two jobs

`src/internal/tui/setup.go:90-104`

`submit` mixes "load the right kind of file" with "advance focus", using
`var err error` plus `var` declarations inside if/else branches.

Extract:

```go
func (s *SetupModel) load(isTasks bool, path string) error {
    if !isTasks {
        prompt, err := looper.LoadPrompt(path)
        if err != nil {
            return err
        }

        s.promptPath, s.prompt = path, prompt
        return nil
    }

    tl, err := looper.LoadTasks(path)
    if err != nil {
        return err
    }

    s.tasksPath, s.taskList = path, tl
    return nil
}
```

`submit` then starts with
`if err := s.load(f.isTasks, f.input.Value()); err != nil { f.err = err.Error(); return s, nil }`.
An `ErrFailedTasks` load still stores nothing, same as now.

Risk: low. `setup_test.go` covers it.

### 11. `skillinstall` cleanups

`src/internal/skillinstall/skillinstall.go:25-30, 116-121, 134-144`

- `Install` and `Check` both build `~/.claude/skills/gralph-docs-writer` from
  `os.UserHomeDir`. Pull that into `skillDir() (string, error)`.
- The `VERSION` parse in `Check` is a nested
  `if data, err := ...; err == nil { ... }`. Extract
  `readStamp(dest) (installedBy, hash string)` so `Check` stays flat. Keep
  `fs.ReadFile(os.DirFS(...))` as is, since it probably keeps gosec quiet.
- Minor: line 36 `err = fs.WalkDir(...)` could use the
  `if err := ...; err != nil` form like the rest of the file.

Risk: low.

### 12. `tasks/task.go` small cleanups

`src/internal/tasks/task.go`

- Lines 146-148, 187-189, 268-270: `if n.Kind == yaml.AliasNode { n = n.Alias }`
  appears three times. Make it a `resolveAlias(n *yaml.Node) *yaml.Node` helper.
- Line 179: `taskFields` also maps gate fields (line 276). Rename it to
  `mappingFields`.
- Lines 235-236: the chained `!=` state check could be
  `!slices.Contains([]string{PendingState, CompletedState, FailedState}, node.Value)`.
- Line 200: move `var id int16` below the nil check.

Risk: none. Error text and check order stay the same; the unknown-key loop in
`checkGate` should keep walking `el.Content` so the reported key stays
deterministic.

## Optional

`src/internal/looper/looper.go:189-268` passes the same six values (prompt,
tasksFile, gateTimeout, sessionArgs, repo, report) through `Run` → `runLoop` →
`runTask`. A plain unexported struct, like the existing `streamTask`, would
shorten those signatures and let the `runLoop` loop body become its own
function. It's a bundle of values, not an interface or a layer, but it sits on
the edge of the "no new abstractions" rule. Skip it to keep the loop path flat.

## Small notes

Not worth a change on their own:

- `textActivity` (`stream.go:64`) could range over `strings.SplitSeq`.
- `SetupModel.PromptPath()` (`setup.go:142`) is only called from tests.

## Looked at and fine as is

- `result.go`: `outcome`, `parseResult`, `lastResultLine`.
- `sandbox.go`'s `forceSandbox`, `event.go`, `version.go`, `skills/embed.go`.
- `claudeCmd`, `finishTask`, `runTaskPlain`, and `runTask`'s early-return chain.
- `gateLimit`, `gateFailure`, and `runGate`'s deadline check.
- `commit.go`: `OpenRepo`, `findRoot`, `relative`/`resolveExisting`,
  `ignored`/`staged`, `commit`/`commitChanges`, `announceCommit`, `repoFor`.
- `stopProcessTree`'s poll loop and `WaitDelay`; `readLines`.
- `ParseTasks`, `tasksSeq`, `checkDuplicate`, `checkTags`, `ParseTimeout`,
  `SaveTasks`.
- `PrintTasks`, `DryRun`, `LoadTasksReport`.
- `main.go`'s `sessionArgs`, `isPlain`, `openRepo`, `validateRequiredFlags`.
- `model.go`'s `layout`, `renderTasks`, `banner`, `View`.
- Blank line after every `if`: no violations found in any production file.
