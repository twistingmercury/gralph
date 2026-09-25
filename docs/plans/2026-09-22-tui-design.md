# Plan: opt-in `--tui` for gralph

## Context

Gralph today pipes `claude --print` straight to the terminal: it echoes the combined
prompt, then claude's stdout/stderr pass through unchanged. Watching a long run it is hard
to tell which task is active, what the agent is doing right now, and which tasks already
finished. The user wants a three-pane terminal UI: **left** the prompt being run,
**right** the agent's output live as it works, **bottom ~1/4** the history of tasks
finished in this run.

Decisions settled with the user during brainstorming (do not revisit):

| Topic          | Decision                                                                                                                                                                                                                                                                                         |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Activation     | Opt-in `--tui` flag. Plain mode stays byte-for-byte as today (argv, stdout echo, inherited stdout/stderr). `--tui` with a non-TTY stdin or stdout fails fast, exit 1, before anything is read or run.                                                                                            |
| Live output    | Under `--tui` only, claude runs with `--output-format stream-json --verbose` and gralph parses NDJSON. No `--include-partial-messages` in v1.                                                                                                                                                    |
| History pane   | This run only: id, name, PASS/FAIL, duration. No reading or writing of `tasks.yaml` state.                                                                                                                                                                                                       |
| Interaction    | `q`/ctrl+c cancels the run through the existing looper ctx. Arrow/PgUp/PgDn/Home/End/wheel scroll the focused pane; `tab` moves focus between prompt and output. On finish (all done, one failed, or user cancel) the screen holds until any key, then gralph exits with plain mode's exit code. |
| Prompt pane    | Shows `<id>: <name>` plus the task prompt. `p` toggles the shared prompt on top. Pane scrolls when focused.                                                                                                                                                                                      |
| Tool rendering | Assistant text as-is. Each `tool_use` is one dim line `> <Name>: <most useful input field, truncated>`. `tool_result` shown only when it is an error. Final `result` highlighted.                                                                                                                |
| Library        | Bubble Tea v2 + Lip Gloss v2 + Bubbles v2 (viewport), Go proxy verified: `charm.land/bubbletea/v2 v2.0.9`, `charm.land/bubbles/v2 v2.2.1`, `charm.land/lipgloss/v2 v2.0.6`, `github.com/charmbracelet/x/ansi v0.11.8`, `golang.org/x/term v0.46.0`.                                              |

**Stated assumption:** an _external_ SIGINT/SIGTERM under `--tui` (nobody at the keyboard:
in raw mode a typed ctrl+c is a key, not a signal) quits immediately instead of holding
the screen, otherwise a non-interactive kill could never exit. Holding applies to normal
finish, failure, and `q`/ctrl+c.

**Deliberate scope change:** `docs/architecture/01-requirements.md:38` and the "fixed
argv" sentences in `CLAUDE.md` describe plain mode. TUI mode extends the argv; plain mode
does not. The e2e argv golden (`tests/e2e/claude_loop_test.go:57,68`) stays as-is.

## Verified facts that shape the design

- `internal/looper/looper.go:74-97` `runLoop(ctx, prompt, *tasks.TaskList)`: echo via
  `fmt.Println`, child inherits `os.Stdout`/`os.Stderr`, no pipe. Seven unit-test call
  sites call `runLoop` directly, so its signature stays.
- `process_tree_unix.go`: `Setpgid` + `cmd.Cancel` SIGKILLs the group. **No `WaitDelay`**
  in production. With a pipe on stdout a killed group can leave `Wait` blocked.
  `StdoutPipe` + `WaitDelay` deadlocks by os/exec's docs, so use `cmd.Stdout = io.Writer`
  (os/exec owns the copy goroutine, `WaitDelay` bounds it).
- Nothing in either suite asserts claude's output passes through. Both fakes write once
  at exit and never stream.
- Every test gives gralph a pipe stdout and no stdin. No pty dependency exists.
- Claude docs: `stream-json` requires `--verbose`; combines fine with
  `--dangerously-skip-permissions`; one JSON object per line; plain `--print` prints only
  the final result. Types: `system` (`init`, `api_retry`, `plugin_install`,
  `permission_denied`), `assistant` (`text` / `tool_use{name,input}` blocks), `user`
  (`tool_result`, `is_error`), `result` (`subtype`, `is_error`, `result`, `duration_ms`,
  `total_cost_usd`, `num_turns`). stderr still carries load errors/warnings.
- gosec G204 (runs in `build/Dockerfile`; `#nosec` banned): argv to `exec.CommandContext`
  must be literals/constants. Two literal call sites, never a runtime `[]string`.
- Bubble Tea v2: `QuitMsg` ends `Run` nil; its own SIGINT handler returns `ErrInterrupted`
  without `Update`; `WithContext` cancel returns `ErrProgramKilled`. Raw-mode ctrl+c is a
  `KeyPressMsg` with `String()=="ctrl+c"`. `View()` returns `tea.View{Content, AltScreen,
MouseMode, WindowTitle}`. Headless options: `WithInput(nil)`, `WithOutput`,
  `WithoutRenderer`, `WithWindowSize`, `WithFilter`. `Program.Send` is a no-op after exit.
  Requires go >= 1.25 (we are 1.27.1). v1 snippets online will not compile.

## Design

### A. Looper seam (`internal/looper/looper.go`, new `linewriter.go`)

```go
type Stream int
const ( Stdout Stream = iota; Stderr )

// Reporter receives loop progress. TaskStarted/TaskFinished run on the loop goroutine,
// Output on os/exec's pipe-copy goroutines: implementations must be goroutine-safe.
type Reporter interface {
    TaskStarted(task tasks.Task, combinedPrompt string)
    Output(stream Stream, line string)           // newline stripped; streaming mode only
    TaskFinished(task tasks.Task, err error, elapsed time.Duration)
}

func Load(promptFile, tasksFile string) (prompt string, tl *tasks.TaskList, err error) // extracted from Start; same "failed to start loop runner: %s" wrapping (%s, pinned by TestStart_LoadFailuresBreakTheErrorChain)
func Start(ctx context.Context, promptFile, tasksFile string) error                      // unchanged behavior: Load + plain runLoop, "loop error: %w"
func RunStreaming(ctx context.Context, prompt string, tl *tasks.TaskList, r Reporter) error // TUI entry; nothing echoed; "loop error: %w"
```

Internals: `plainReporter{}` whose `TaskStarted` is today's `fmt.Println(prompt)` and other
methods no-op. `runLoop` keeps its signature and calls
`runLoopWith(ctx, p, tl, plainReporter{}, false)`. `runLoopWith` per task: build
`fmt.Sprintf("%s\n\n%s\n", p, task.String())` (wire contract unchanged), `TaskStarted`,
`newClaudeCmd(ctx, stream)`, `configureProcessTree`, stdin reader, time it, `runChild`,
wrap error `task %d: %s failed: %w`, `TaskFinished`, stop on error.

`newClaudeCmd` is the only place argv lives, two literal call sites of package constants
(`claude`, `--print`, `--dangerously-skip-permissions`, `--output-format`, `stream-json`,
`--verbose`).

`runChild`: plain sets `cmd.Stdout = os.Stdout; cmd.Stderr = os.Stderr; cmd.Run()` (no
pipe, no WaitDelay). Stream sets each of stdout/stderr to a `lineWriter` that calls
`r.Output(stream, line)`, `cmd.WaitDelay = 2s`, `cmd.Run()`, then `Flush()` both. If
`errors.Is(err, exec.ErrWaitDelay)` after a zero exit, treat as success and emit
`r.Output(Stderr, "gralph: claude exited 0 but left its output pipe open; continuing")`.
No goroutines or channels added to the looper; cancel path is unchanged (SIGKILL group,
pipes close, `Wait` returns; `WaitDelay` bounds the escaped-grandchild case).

`lineWriter`: `sync.Mutex` + `bytes.Buffer` (not `bufio.Scanner`, so a multi-MB
`tool_result` line is not truncated); `Write` emits each complete line with `\n` and
trailing `\r` stripped; `Flush` emits a trailing partial line.

### B. Stream-json parser (`internal/claudestream/`, stdlib only)

```go
type Kind int // KindRaw, KindInit, KindText, KindToolUse, KindToolResult, KindResult
type Event struct { Kind Kind; Raw, Text, Tool string; IsError bool; Subtype string; Duration time.Duration; CostUSD float64 }
func Parse(line string) []Event    // never fails; one event per content block; empty/whitespace -> none; non-JSON/unknown type -> KindRaw
func (e Event) Display() string    // plain text, no ANSI; TUI styles by Kind
```

Display: Init `-- session started (model: X) --`; Text verbatim; ToolUse `> Bash: go test
./...` (input summary prefers `command`, `file_path`, `path`, `pattern`, `query`,
`description`, else compact JSON; 200 runes max); ToolResult `< error: ...` (only rendered
by the TUI when `IsError`); Result `-- done (success, 1.2s, $0.0123) --` or
`-- failed (error_max_turns): <result> --`; Raw the line itself.

### C. TUI package (`internal/tui/`: `model.go`, `layout.go`, `keys.go`, `reporter.go`, `run.go`)

Messages: `taskStartedMsg{Task, Prompt, At}`, `outputMsg{Stream, Line}`,
`taskFinishedMsg{Task, Err, Elapsed}`, `runFinishedMsg{Err}`, `tickMsg`.

`reporter{send func(tea.Msg)}` implements `looper.Reporter` by forwarding each call as a
message (`send = program.Send`).

`Model` fields: `total, index`, `current *tasks.Task`, `sharedPrompt string`,
`showShared bool`, `focus` (prompt|output), `prompt, output viewport.Model`
(`SoftWrap`, `MouseWheelEnabled`), `lines []string` capped at 5000 (trim from top),
`follow bool`, `history []historyEntry{ID, Name, Passed, Elapsed}`, `width, height`,
`startedAt, now, taskStartedAt`, `phase` (running|cancelling|finished), `userCancelled`,
`runErr`, `cancel context.CancelFunc`, `keys`.

Update rules:

- `WindowSizeMsg`: store, `computeLayout`, resize viewports, re-set content.
- `taskStartedMsg`: `index++`, reset `lines`, prompt content = `task.String()` (or shared +
  blank + task when `showShared`), output cleared, `follow = true`.
- `outputMsg`: Stdout -> `claudestream.Parse` -> append styled `Display()` per event,
  skipping non-error ToolResult; Stderr -> append dim `stderr: ` + `ansi.Strip(line)`.
  Expand tabs, drop `\r`. `SetContent`, `GotoBottom()` if `follow`.
- `taskFinishedMsg`: append history entry.
- `runFinishedMsg`: `runErr`, `phase = finished`; if the error wraps `context.Canceled` and
  `!userCancelled`, return `tea.Quit` (external signal); else hold.
- `tickMsg`: update `now`; re-arm only while not finished.
- `KeyPressMsg`: finished -> `tea.Quit` on any key. `q`/`ctrl+c` -> `userCancelled = true`,
  `phase = cancelling`, `m.cancel()`. `tab` -> toggle focus. `p` -> toggle `showShared`
  and re-set prompt content. Scroll keys (`up`, `down`, `pgup`, `pgdown`, `home`, `end`)
  -> forward to the focused viewport; for output, `follow = output.AtBottom()`.
- `MouseWheelMsg`: forward to the output viewport, update `follow`.

Key map (`charm.land/bubbles/v2/key`) replaces the viewport defaults so `j/k/d/u/f/b/space`
do nothing.

Layout: `historyH = max(4, height/4)`, `topH = height - historyH - 1` (status bar),
`promptW = width*2/5`, `outputW = width - promptW`; rounded borders, inner = outer-2;
`minWidth, minHeight = 40, 12`, below which View is one line `terminal too small: need at
least 40x12`. Focused pane title is highlighted. View: `AltScreen = true`,
`MouseMode = tea.MouseModeCellMotion`, `WindowTitle = "gralph"`. History shows the last
rows that fit: `#1  First task  PASS  12.3s` (green/red). Status bar:
`task 2 of 5 | task 00:12 | run 01:03 | q cancel  tab focus  p shared prompt  arrows/wheel scroll`;
`cancelling...` while cancelling; `done: 5/5 passed | press any key to exit` or
`failed: <err> | press any key to exit` when finished.

`run.go`:

```go
type LoopFunc func(ctx context.Context, prompt string, tl *tasks.TaskList, r looper.Reporter) error
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, loop LoopFunc) error
```

`Run` derives `runCtx, cancel := context.WithCancel(ctx)`, builds the model with `cancel`,
`tea.NewProgram(m, tea.WithoutSignalHandler(), opts...)`, runs `loop` in a goroutine with
`&reporter{send: p.Send}` and sends `runFinishedMsg{err}` when it returns, then `p.Run()`,
`cancel()`, joins the loop goroutine (so the child group kill completes before exit),
returns `fmt.Errorf("tui: %w", runErr)` on a program failure or the loop error otherwise.
Do not use `tea.WithContext` (would kill the hold-on-finish screen). `WithoutSignalHandler`
keeps `cmd/main`'s `signal.NotifyContext` as the single cancellation path; SIGWINCH resize
still works.

### D. `cmd/main/main.go`

`tuiFlag = pflag.Bool("tui", false, "Show a live terminal UI for the run (requires an
interactive terminal)")`. After `validateRequiredFlags`, if `--tui`: `requireTerminal()`
checks `term.IsTerminal` on stdin and stdout, else prints
`error: --tui requires an interactive terminal (stdin and stdout must be a TTY)` and exits

1. Then `NotifyContext` as today, `looper.Load` (fails before any screen is drawn),
   `tui.Run(ctx, prompt, tl, looper.RunStreaming)`. Errors go through today's `error: %v`

- exit 1, printed after the terminal is restored. Plain path is untouched.

### E. Fake claude (`internal/looper/testdata/fakeclaude/main.go`)

New env vars: `FAKE_CLAUDE_STREAM=1` emits, after recording stdin, six NDJSON lines to
stdout one per `FAKE_CLAUDE_STREAM_DELAY_MS` (default 20), plus one stderr line
`fakeclaude: stderr line` between lines 2 and 3, then exits with `FAKE_CLAUDE_EXIT`:
init, assistant text `Working on it.`, assistant tool_use `Bash` `go test ./...`, user
tool_result `ok`, `this line is not json`, result success 1234ms $0.0123.
Argv validation in both modes: expected `--print --dangerously-skip-permissions`
(+ `--output-format stream-json --verbose` when streaming); mismatch prints
`fakeclaude: unexpected argv %q` and exits 64. `FAKE_CLAUDE_BLOCK` keeps precedence.
The e2e fake is unchanged (no e2e test can reach `--tui` without a TTY).

### F. Tests

Unit (testify, `require` for preconditions, `assert` for checks):

- `claudestream`: table test per event type, tool_use summary keys and truncation,
  tool_result string/array/`is_error`, result with/without fields, unknown type, non-object
  JSON, malformed, empty/whitespace, trailing `\r`, 1 MiB line, no newlines in
  ToolUse/ToolResult/Result display.
- `looper`: `TestNewClaudeCmd_PlainArgv/StreamArgv`; `TestLoad_*` mirroring
  `TestStart_Missing*`; `TestRunLoopWith_PlainReporterMatchesRunLoop` (stdout capture);
  `TestRunStreaming_RecordsParsedLinesInOrder` (recording reporter, stdin record still
  exact); `TestRunStreaming_DeliversLinesLive` (delay 60ms, `finished - firstOutput >=
150ms`); `TestRunStreaming_NonZeroExitStopsAndReportsError`;
  `TestRunStreaming_NothingEchoedToStdout`; `TestRunStreaming_CancelMidTaskStopsQuickly`
  in `runloop_unix_test.go`; `TestLineWriter_*` (partial writes, multi-line write, `\r\n`,
  empty lines kept, Flush trailing partial, Flush empty).
- `tui`: `layout_test` (sums equal width/height for 40x12, 80x24, 120x40, 200x60; too
  small; no negative inner sizes); `reporter_test` (each call -> one message);
  `model_test` headless (strip ANSI with `ansi.Strip` before `Contains`): prompt and
  `task 1 of 3` shown; stdout parsed (`Working on it.`, `> Bash: go test`); stderr shown
  raw with prefix; raw line verbatim; follow until user scrolls, `end` resumes; history
  PASS/FAIL rows; `q` and `ctrl+c` call cancel and set `cancelling` without quitting;
  finished holds until any key which yields `tea.QuitMsg`; error shown and `Err()`
  returns it; external cancel quits immediately; `tab` focus and `p` toggle; too small;
  5000-line cap; scroll keys before first task do not panic.
  `run_test` headless (`WithInput(nil)`, `WithOutput(io.Discard)`, `WithoutRenderer()`,
  `WithWindowSize(80,24)`, `WithFilter` to inject keys): `Run` returns the loop error and
  reporter calls reach the model; `q` cancels the loop ctx within 5s. If these prove flaky
  headless, keep the model-level tests and note it.

E2E (`tests/e2e/tui_test.go`, new): `TestTUI_RequiresTerminal` (`--tui` with piped stdio:
exit 1, stderr contains `--tui requires an interactive terminal`, zero fake invocations,
empty stdout); `TestTUI_FlagIsKnown` (`--tui` alone complains about `--prompt`, not an
unknown flag). Plain-mode argv is already pinned at `claude_loop_test.go:57`; add a comment
that `--tui` is the only mode that changes argv. **Real-TTY e2e is out of scope:** it needs
a pty dependency, asserts terminal-dependent ANSI, and must script a key press to release
the hold. Covered by headless model tests plus the manual checklist below.

### G. Docs and config

- `README.md`: flag row for `--tui`; "Terminal UI" subsection under "How it works" (panes,
  keys, the TUI argv, hold-until-key, same exit codes); the "claude must be on PATH"
  bullet mentions both argv shapes.
- `CLAUDE.md`: argv is fixed _per mode_; add `internal/claudestream` and `internal/tui`
  (Bubble Tea v2, `charm.land` imports) to Architecture; describe `Reporter`, `Load`,
  `RunStreaming`, `newClaudeCmd`'s two literal call sites and the G204 reason; replace
  "there is no injectable runner" with the `Reporter` + `stream` seam and `tui.Run`'s
  `LoopFunc`; list `FAKE_CLAUDE_STREAM*`; note `WithoutSignalHandler` and no
  `WithContext` on purpose; cancellation bullet gains `q`/ctrl+c under `--tui`.
- `go.mod`/`go.sum`: add the five modules above; `go mod tidy`; commit `go.sum` because
  `build/Dockerfile` runs `go mod verify` first. `tests/e2e/go.mod` untouched (its testify
  is older than root's; leave it).
- New packages must pass goimports, golangci-lint defaults, govulncheck, gosec inside
  `build/Dockerfile` (`make analyze` locally covers the same).

## Steps (TDD, each independently green)

1. `internal/claudestream`: tests, then `Parse`/`Display`.
   `go test ./internal/claudestream -count=1`
2. `internal/looper/linewriter.go`: tests, then implementation.
   `go test ./internal/looper -run TestLineWriter -count=1`
3. Fake claude: argv validation + stream mode.
   `go test ./internal/looper -count=1` (existing tests stay green)
4. Looper seam: `newClaudeCmd` tests -> impl; `Load` tests -> extract; `RunStreaming`
   tests with a recording reporter (order, live delivery, non-zero exit, no echo, cancel)
   -> `Reporter`, `plainReporter`, `runLoopWith`, `runChild`.
   `go test ./internal/... -count=1 && go vet ./...`
5. `go get` the charm/term/ansi deps, `go mod tidy`.
   `go build ./... && go mod verify`
6. `internal/tui`: `layout` (tests first), `keys`, `reporter` (tests first), `model`
   (Update/View tests first), `run` (headless tests).
   `go test ./internal/tui -count=1`
7. `cmd/main` wiring.
   `make local`; `.bin/local/gralph --tui -p prompt.md -t tasks.yaml | cat` exits 1 with
   the TTY message; plain invocation output unchanged against the fake.
8. Manual real-claude checklist after `make local install`, in a real terminal: prompt
   left; events stream right within a second of claude starting; wheel/arrow scrolling
   stops auto-follow and `end` resumes; `tab` moves focus, `p` shows the shared prompt;
   resize redraws; `q` kills claude (`pgrep -f claude` afterwards is empty); history rows
   with durations; failed task prints `error: loop error: task N: ... failed` after
   teardown with exit 1; success exits 0; `kill -TERM <gralph pid>` from another shell
   exits promptly.
9. E2E tests.
   `make local && cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -count=1 .`
   then `docker compose -f tests/docker-compose.yaml up --build --exit-code-from tests`
10. Docs, then `make analyze` and `docker build --target builder -f build/Dockerfile .`
    (lint, govulncheck, gosec, unit tests exactly as CI).

## Risks

- Bubble Tea's SIGINT handler would return `ErrInterrupted` and skip the hold:
  `WithoutSignalHandler()`. `WithContext` would kill the final screen: derived
  `context.WithCancel` instead.
- `Wait` hanging after SIGKILL with an open pipe: `io.Writer` pipes + `WaitDelay`, never
  `StdoutPipe` + `WaitDelay`. `ErrWaitDelay` after exit 0 is a visible note, not a failure.
- ANSI/control chars in claude stderr or `result` text corrupt the layout: `ansi.Strip`,
  expand tabs, drop `\r`.
- Very long single lines and long runs: soft-wrap on, 200-rune tool summaries, 5000-line
  cap. `program.Send` applies backpressure to claude's pipe under bursts; no loss.
- Lip Gloss emits ANSI in `View().Content` regardless of tty: view assertions strip first.
- gosec G204: two literal call sites; never a runtime `[]string` argv.
- Headless `tea.Program` tests are an assumption from v1; fall back to model-level tests.
