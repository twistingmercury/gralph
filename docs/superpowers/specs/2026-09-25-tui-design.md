# TUI and live session activity — design

**Date:** 2026-09-25
**Status:** Approved
**Mockup:** [docs/tui_mock_up.txt](../../tui_mock_up.txt)

## Goal

Make a gralph run easy to follow while it happens:

- Make a full-screen view the default in a terminal, with the current task's
  prompt, the task list and statuses, the current session's live activity,
  and key help.
- Show what the current Claude session is doing, live, instead of nothing
  until the task ends.
- Leave plain mode (`--no-tui`, or no terminal) exactly as it is today.

## Decisions

| Topic | Decision |
|---|---|
| Mode | TUI by default when stdin and stdout are both terminals. Plain mode with `--no-tui`, or automatically when either is not a terminal (CI, pipes, e2e). |
| Plain mode | Byte-for-byte as today: same claude argv, the combined-prompt echo, claude's stdout/stderr inherited. |
| Live output | TUI only: run claude with `--output-format stream-json --verbose` and show its activity as it happens. |
| TUI start failure | Print the error, suggest `--no-tui`, exit 1. No automatic fallback. |
| Missing `-t`/`-p` | The TUI asks for them on a setup screen; plain mode still errors, as today. |
| `--dry-run` | Always plain, as today. Never launches the TUI and never prompts; `-t` stays required. |
| End of run | TUI stays open on the final state until `q`; exit code is the run's. |
| Quit mid-run | `q` or Ctrl-C asks to confirm; `y` cancels like Ctrl-C today. |
| External signal | SIGINT/SIGTERM from outside the TUI cancels at once, no confirm; exit 1. |
| Prompt pane | The current task's `<id>: <name>` and prompt; the shared prompt is not shown. |
| Output pane | Cleared when the next task starts; shows the current task only. |
| "in progress" | Display only; never written to `tasks.yaml`. |
| Library | Bubble Tea v2: `charm.land/bubbletea/v2` v2.0.10, `charm.land/bubbles/v2` v2.2.1 (viewport, textinput), `charm.land/lipgloss/v2` v2.0.6. No v1 `github.com/charmbracelet/*` imports. |
| Architecture | One loop. Plain mode runs each task exactly as today (`report` is nil); the TUI runs each task with stream-json and receives events through `report`. |

## Section 1 — Runner

### Shared loop, two task paths

`runLoop` keeps one copy of the loop logic (skip completed, save after every
task, stop on failure, cancel leaves the task untouched). It takes a
`report func(Event)`:

- `report == nil` — plain mode. Each task runs exactly as today: argv
  `claude --print --dangerously-skip-permissions`, the combined prompt echoed
  to stdout, claude's stdout teed to the terminal and captured for
  `lastResultLine`, stderr inherited. No output or behavior changes.
- `report != nil` — TUI mode. Each task runs claude with stream-json (below),
  nothing is written to gralph's stdout/stderr, and progress goes through
  `report`.

The loop body calls one of two unexported helpers per task, `runTaskPlain`
or `runTaskStream`, so the loop rules stay in one place, apart from the code
that talks to claude.

The combined-prompt wire contract
(`fmt.Sprintf("%s\n\n%s\n", prompt, task.String())`) is the same in both.

```go
type Event struct {
    Kind EventKind // TaskStarted, Activity, TaskFinished, RunDone
    Task tasks.Task
    Line string    // Activity
    Err  error     // TaskFinished (failed), RunDone
}
```

- `TaskStarted` and `TaskFinished` carry a copy of the task; `TaskFinished`'s
  `Task.State` is the saved state (`completed` or `failed`) and `Err` is set
  when it failed.
- Completed tasks that are skipped send no event; the TUI shows them from the
  task list it was given at start.
- A cancelled task sends no `TaskFinished`; `RunDone` follows with the
  cancellation error, and the TUI shows that task as `pending`.
- `report` receives only copies. The TUI never reads the `TaskList` that
  `runLoop` is mutating.

### Package boundary

The TUI lives in `internal/tui` and depends on `internal/looper`, never the
reverse. `looper` never imports Bubble Tea; only `internal/tui` and
`cmd/main` do. `looper` exports what the TUI and `cmd/main` need:

- `LoadPrompt(path string) (string, error)`: today's prompt checks (trimmed,
  non-empty). Prints nothing.
- `LoadTasks(path string) (*tasks.TaskList, error)`: today's tasks-file
  checks, plus the failed-task check. Prints nothing. When the file parses
  but holds a `failed` task it returns the list **and** `ErrFailedTasks`, so
  the caller can print the table: `Start` then returns an error (exit 1)
  and `DryRun` returns nil (exit 0), both byte for byte as today.
- `Run(ctx, prompt, tl, tasksFile, report)`: the exported entry to `runLoop`.
  `Start` is `LoadPrompt` + `LoadTasks` + the table on refusal +
  `Run(..., nil)`.

Two loaders rather than one `Load`, because the setup screen validates one
path at a time.

### TUI task path: claude invocation

`claude --print --output-format stream-json --verbose --dangerously-skip-permissions`,
prompt on stdin.

### Stream parsing

Claude writes one JSON event per line on stdout. Observed event types:

| `type` | Use |
|---|---|
| `system` (`init`, `hook_*`) | ignored |
| `assistant` | `message.content[]`: `text` blocks and `tool_use` blocks (`name`, `input`) become activity lines |
| `user` | `tool_result` blocks; ignored |
| `rate_limit_event` | ignored |
| `result` | `result` holds the final message text; used for the outcome |

Gralph reads stdout line by line and decodes only the fields it uses. Unknown
event types and lines that fail to decode are ignored. Claude's stderr is
captured and reported as `Activity` lines.

Lines have no length limit: a `user` event can carry a whole file. Read with
`bufio.Reader.ReadBytes('\n')` (or equivalent), not a default
`bufio.Scanner`, whose 64 KiB token limit would stop reading. Stdout and
stderr are always drained to EOF, even after a decode error, before
`cmd.Wait`; a pipe left unread blocks claude and hangs the run.

Activity lines:

- `text` block: the text, one line per non-blank line.
- `tool_use` block: `→ <Name> <target>`, where target is `input.command` (Bash),
  `input.file_path` (Read, Edit, Write), `input.pattern` (Grep, Glob), else
  empty; truncated to one line.

### Outcome

`outcome(runErr, lastResultLine(resultText))`, where `resultText` is the
`result` event's `result` field. The rules are the same as plain mode: only
exit 0 with a `completed` result line succeeds; no `result` event means an
empty text, which is "no valid result line" and therefore `failed`.

## Section 2 — TUI

### Startup order

1. Parse flags. `--version` and `--install-skill` exit early, as today.
2. Choose the mode: plain if `--dry-run` or `--no-tui` is set, or stdin or
   stdout is not a terminal; otherwise the TUI.
3. Plain: exactly today's order — required-flag check, skill check, then
   dry-run or `Start`. Errors and their order are unchanged.
4. TUI: skill check (plain stderr on failure). Then, for each path given on
   the command line, `LoadTasks`/`LoadPrompt` check it before the TUI opens; a bad file is a
   plain stderr error and exit 1, as today (a `failed` task prints the same
   table and refusal as `Start`). Paths that are missing go to the setup
   screen; then the run view.

### Setup screen

One text field per missing path. Enter validates it with `LoadTasks` or
`LoadPrompt` (tasks file parses and has no `failed` task; prompt file non-empty
after trimming) so the rules cannot drift. Errors show under the field.
`Esc` quits.

### Run view

Alt screen, sized to the terminal and relaid out on resize, following the
mockup:

- **Prompt pane** (top left): the current task's `<id>: <name>` and prompt
  (`task.String()`), read-only, scrollable; replaced on each `TaskStarted`.
  Before the first task and after the run it shows the last task that ran.
  The shared prompt is not shown.
- **Tasks pane** (bottom left): one row per task — `✅`/`❌`/`▶`, name, and
  `pending | in progress | completed | failed`.
- **Output pane** (right): the current task's activity lines under a
  `task <id>: <name>` header. Cleared on each `TaskStarted`, so it holds one
  task at a time; after the run it keeps the last task's output. Follows the
  newest line unless scrolled up.
- **Key legend** (bottom): `tab` switch focused pane · `↑/↓/PgUp/PgDn` scroll ·
  `q` quit.

`runLoop` runs in a goroutine with a cancellable context; its `report` hook
calls `program.Send(event)`. The model only reacts to messages.

### Quitting

- During a run, `q` or Ctrl-C shows `Stop the run? The current task stays
  pending. [y/N]`. `y` cancels the context (kills the claude process group,
  leaves the task `pending`, file untouched), waits for `runLoop` to return,
  and exits 1.
- SIGINT/SIGTERM from outside (a keyboard Ctrl-C is a key in raw mode, not a
  signal): cancel the context at once with no confirm, leave the task
  `pending`, wait for `runLoop`, close the TUI, exit 1. The run context
  derives from `cmd/main`'s `signal.NotifyContext`; Bubble Tea's own signal
  handling is disabled (`tea.WithoutSignalHandler()`) so the signal has one
  owner.
- After the run ends, the view freezes with a status line (`All tasks
  completed` or `Task <id> failed: <error>`); `q` exits 0 or 1.
- After the TUI closes, gralph prints a one-line summary to the terminal.

## Section 3 — Errors and testing

### Errors

- Before the TUI opens (flags, skill check, `--dry-run`): plain stderr and
  exit code, as today.
- In the TUI: a failed task shows `❌` and its error on the status line; a
  `tasks.yaml` write failure stops the run and is shown the same way.
- If the TUI fails to start despite the terminal check, gralph prints the
  error, suggests `--no-tui`, and exits 1.

### Testing

- Plain mode: no test changes. Existing unit and e2e tests pin today's
  behavior and must keep passing unchanged.
- TUI task path: unit tests for stream parsing (events → activity lines;
  `result` text → outcome; a line over 64 KiB is read, not a hang) and for
  `runLoop` with a non-nil `report` (event order; cancel sends no
  `TaskFinished`).
- `LoadPrompt`/`LoadTasks`: unit tests that they match today's checks, print
  nothing, and return the list with `ErrFailedTasks` for a failed task.
- The unit fake claude keeps today's text output by default and emits
  stream-json only when their argv contains `--output-format stream-json`. Then the
  default is `system` init, one `assistant` text event, and a `result` event
  whose text is the completed result line; `FAKE_CLAUDE_OUTPUT` supplies the
  `result` text. The e2e fake is unchanged: e2e has no terminal, so gralph
  never asks it for stream-json.
- e2e: gralph without a terminal (as e2e always runs) uses plain mode with
  no flag, so every existing test keeps passing unchanged; add a test that
  `--no-tui` is accepted and output is identical to running without it. (e2e
  has no terminal, so it cannot drive the TUI itself.)
- TUI: unit tests on the Bubble Tea model without a terminal — events update
  statuses, `TaskStarted` replaces the prompt pane and clears the output
  pane, `q` mid-run asks to confirm, `y` cancels, a cancelled context closes
  without confirm, the final state stays until `q`, resize relayouts, the
  setup screen rejects invalid paths and accepts valid ones.
- A manual run of the TUI against a small task file with the fake claude.

### Docs

README (the TUI as default, `--no-tui`, the no-terminal fallback, keys), CLAUDE.md (the `report` hook and
its two task paths, stream-json in the unit fake, the TUI package), and ADRs in
`docs/architecture/02_architectural_decisions.md` for the default TUI with
stream-json live activity (plain mode unchanged) and for Bubble Tea.

## Delivery order

Each step leaves gralph working and passing the quality gates. Plain mode is
unchanged throughout.

1. `LoadPrompt`, `LoadTasks`, and `Run` exported from `looper`, and the `report` parameter on
   `runLoop` (nil everywhere) — a refactor, no visible change.
2. The stream-json task path behind a non-nil `report`, with stream parsing;
   the unit fake claude emits stream-json when asked.
3. The TUI as default: mode selection (terminal check, `--no-tui`), the run
   view, and quitting.
4. The setup screen for missing `-t`/`-p`.
5. Docs and ADRs.

## Out of scope

- Writing "in progress" to `tasks.yaml`.
- A TUI for `--dry-run`.
- The interviewer (`gralph plan`) — a separate change.
