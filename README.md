# Gralph

> **Maturity Level**: Emerging - under active development; the CLI contract has already changed between minor versions
> **Version**: v0.7.0

> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

Gralph runs "Ralph loops" with Claude Code. You give it a list of tasks in a
YAML file and one shared prompt. It works through the list one task at a time,
starting a fresh `claude --print --dangerously-skip-permissions` session for
each, so every task gets a clean context instead of one long session that
drifts.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)
- [License](#license)

## Usage

```bash
gralph --prompt path/to/prompt.md --tasks path/to/tasks.yaml
```

| Flag               | Required                                                                | Description                                                             |
| ------------------ | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `--prompt` / `-p`  | Yes, unless `--dry-run`; asked for when missing in the full-screen view | Path to the shared prompt sent to Claude for every task                 |
| `--tasks` / `-t`   | Yes; asked for when missing in the full-screen view                     | Path to the YAML task list that drives the loop                         |
| `--dry-run`        | No                                                                      | Validate the task file and report on it without running anything        |
| `--no-tui`         | No                                                                      | Use plain output instead of the full-screen view                        |
| `--install-skill`  | No                                                                      | Install the bundled `gralph-docs-writer` skill for Claude Code and exit |
| `--version` / `-v` | No                                                                      | Print version information and exit                                      |

Run it in a terminal and you get a full-screen view of the run (see
[The full-screen view](#the-full-screen-view)). Pipe it, redirect it, run it in
CI, or pass `--no-tui`, and you get plain text output instead. `--dry-run` is
always plain.

### Checking a task file first

Want to know if a task file is good before you spend a run on it?

```bash
gralph -t tasks.yaml --dry-run
```

This uses the same checks as a real run, but never launches Claude or writes
anything. `--prompt` is ignored. If the file is invalid, you get the error on
stderr and a non-zero exit. If it's valid, you get a table of each task's id,
state, and name, followed by `<tasks path> is valid`, and exit zero.

If any task is `failed` (a real run would refuse to start), the table comes
after `Some tasks failed previous runs:` instead, and each failed row ends in
`← Needs review!`.

### The task file

A task file is a `tasks` list. Each task needs a unique positive integer `id`,
a `name` (unique, ignoring case and surrounding spaces), and a `prompt`. `state`
and `gates` are optional:

```yaml
tasks:
  - id: 1
    name: Add the widget repository
    prompt: |
      Objective:
      Add a Postgres-backed WidgetRepository with Create and GetByID.

      Verification:
      - Command: go test ./internal/widget/...
    gates:
      - cmd: go test ./internal/widget/...
  - id: 2
    name: Expose GET /widgets/{id}
    state: pending
    prompt: |
      Add the HTTP handler using the repository from task 1.
```

- `state` is `pending`, `completed`, or `failed`. Leave it out for new work;
  it's read as `pending`.
- Gralph adds an `error` field when a task fails. The session never writes it.
- `gates` is a list of commands gralph runs itself once the task's session says
  it's done (see [Gates](#gates)). Each entry has exactly one key, `cmd`.
- Tasks run in file order. The `id` just identifies a task; it doesn't set the
  order.

### The gralph-docs-writer skill

Writing a good task file by hand is tedious, so gralph ships a Claude Code
skill, [gralph-docs-writer](skills/gralph-docs-writer/SKILL.md), that turns a
plan into a task file and shared prompt. It's built into the binary:

```bash
gralph --install-skill
```

That replaces `~/.claude/skills/gralph-docs-writer/` with the copy that matches
your gralph binary and prints the path. A run or `--dry-run` won't start
(exit 1) while the skill is missing or came from a different gralph version;
it tells you to run `gralph --install-skill`. Working on the skill itself?
`scripts/install_skill.sh` installs the copy in your working tree instead.

## How it works

For each task, gralph glues the shared prompt and the task together and hands
the result to a new `claude --print` session on stdin. Claude sees:

```text
<shared prompt>

<id>: <name>

<task prompt>
```

Tasks already marked `completed` are skipped; in plain mode you'll see
`task <id>: <name> already completed, skipping`.

Gralph decides how a task went from the **last non-blank line** of Claude's
output (lines that are just code fences don't count). Your shared prompt should
ask Claude to end with a JSON line like `{"state": "completed", "error": ""}`.

- If that line is valid JSON with `state: "completed"` **and** the session
  exits zero, gralph runs the task's [gates](#gates), if it has any. When they
  all pass, or there are none, the task is `completed` and gralph saves the
  file.
- Anything else marks the task `failed`: a `failed` state, a missing or broken
  line, or a non-zero exit. Gralph saves the file, reports
  `task <id>: <name> failed: <error>` (the JSON `error`, or the exit status if
  there isn't one), exits non-zero, and doesn't run any later tasks.

When every task succeeds, gralph exits zero.

In plain mode, gralph also prints the combined prompt, and Claude's output
passes straight through to your terminal. The full-screen view runs
`claude --print --output-format stream-json --verbose
--dangerously-skip-permissions` instead, so it can show the session's activity
live. It uses the same success and failure rules and saves the file the same
way.

Gralph saves the task file after every task, atomically (it writes a temporary
file and renames it over the original), so a crash never leaves you with half a
file. The catch: comments and custom formatting don't survive, and every task's
`state` gets written out explicitly.

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

### The full-screen view

The view has three panes, each with a title bar, over a one-line legend:

- **Current task** (top left): the task that's running, as `<id>: <name>` plus
  its prompt.
- **Task progress** (bottom left): every task as `<icon> <id>: <name>: <state>`,
  colored by state. The running task shows `in progress`; that's display only
  and never written to the file.
- **Claude activity** (right): what the session is doing: Claude's text, one
  `→ <tool> <target>` line per tool call, anything on stderr, and then each
  gate and its output. It keeps up with new lines as long as you're scrolled to
  the bottom.

Keys:

| Key             | Action                                                                                                                                                                         |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `tab`           | Move focus to the next pane (Claude activity has it at start)                                                                                                                  |
| `↑` / `↓`       | Scroll the focused pane one line                                                                                                                                               |
| `PgUp` / `PgDn` | Scroll the focused pane one page                                                                                                                                               |
| `q` / `ctrl+c`  | Ask `Stop the run? The current task stays pending. [y/N]`; `y` stops the Claude session and closes the view, any other key carries on. Once the run has ended, closes the view |

When the run finishes, the view stays open so you can look around. A banner at
the top of Task progress (and the legend) tells you how it went, like
`✔ All tasks completed · press q to exit` (bold green) or
`✘ Task <id> failed: <error> · press q to exit` (bold red). Press `q` to close
it. Gralph then prints one summary line to stdout: `All tasks completed`,
`Task <id> failed: <error>`, `Run stopped: <error>`, or `Run stopped by user` /
`Run stopped by signal`. It exits zero only when every task completed.

Forgot `--tasks` or `--prompt`? A setup screen asks for each missing path,
tasks file first. `enter` checks the path with the same rules a run uses and
shows any problem right under the field. `esc` or `ctrl+c` quits with
`error: setup cancelled` and exit 1. As in plain mode, a task file with a
`failed` task prints the task table and exits 1 before the view ever opens.

Want the background? The idea behind gralph is in
[docs/gralph-concept.md](docs/gralph-concept.md), and the design starts at
[docs/architecture/00_overview.md](docs/architecture/00_overview.md).

## Key Considerations

- **Don't run gralph from inside a Claude session.** It starts Claude itself, as
  a subprocess, so run it from a normal terminal.
- **You need the `claude` CLI on your `PATH`.** Gralph calls it directly.
- **Only run task files you trust.** `--dangerously-skip-permissions` means
  Claude won't stop to ask before running commands or editing files. Treat the
  prompt and task files like code you're about to run. Gate commands are run by
  gralph itself through `sh`, with no sandbox.
- **Every session starts from scratch.** A session sees the shared prompt and
  its own task, nothing else. If a task depends on earlier work, say so in the
  prompt, or make sure the repository shows it.
- **A failed task stops everything until you deal with it.** There's no retry.
  Gralph won't start while any task is `failed`: it prints the same table as
  `--dry-run`, exits 1, and doesn't touch the file. Fix whatever went wrong,
  set that task's `state` back to `pending` (or `completed`) by hand, and run
  again.
- **Ctrl-C stops the run cleanly.** In plain mode, SIGINT (Ctrl-C) or SIGTERM
  stops the current Claude session or gate and the run. Gralph kills Claude's
  whole process group, so nothing Claude started is left running. The interrupted
  task and the file stay as they were, so the next run picks it up again. In
  the full-screen view, Ctrl-C is just a key (see `q` above). A SIGINT or
  SIGTERM from outside stops a running loop without asking, closes the view,
  prints `Run stopped by signal`, and exits 1. Once the run has ended, it just
  closes the view.
- **Unix only.** Gralph runs on Linux, macOS, and the BSDs. Windows isn't
  supported.

## Development Considerations

### Quick Start

Requires Go 1.27.1 or later.

```bash
git clone https://github.com/twistingmercury/gralph.git
cd gralph
make local install   # builds .bin/local/gralph, then copies it to ~/go/bin
gralph --version
```

Heads up: `make install` only copies the last local build, so always run it
with `make local` like above, or you'll install a stale binary. `make help`
lists every target.

`make build` is the release build, and it's exactly what CI runs
([build/build.sh](build/build.sh) is the only thing the CI workflow executes).
It needs Docker and the `ghcr.io/twistingmercury/golang-tooling:go1.27.1`
image. It runs the linters, security scanners, and unit tests inside the image,
cross-compiles into `.bin/<arch>/<os>/`, then runs the e2e suite in a
container. If it passes locally, CI should pass too.

### Testing

```bash
make test      # unit tests: go test -v ./cmd/... ./internal/...
make analyze   # goimports, golangci-lint, govulncheck, gosec (tools must be on PATH)
```

One gotcha: `tests/e2e` is its own Go module, so `go test ./...` from the
repository root never runs it. Use `make build` for that. The e2e suite uses a
fake `claude` on `PATH`, so you don't need the real Claude CLI or a network
connection.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags and embedded at build time:

```bash
git describe --tags --always
```

## License

[MIT](LICENSE)
