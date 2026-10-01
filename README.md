# Gralph

> **Maturity Level**: Emerging - under active development; the CLI contract has already changed between minor versions  
> **Version**: v0.9.1 
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

Gralph runs "Ralph loops" with Claude Code. You give it a list of tasks in a
YAML file and one shared prompt. It works through the list one task at a time,
starting a fresh `claude --print` session for each, so every task gets a clean
context instead of one long session that drifts.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)
- [License](#license)

## Usage

```bash
gralph --prompt path/to/prompt.md --tasks path/to/tasks.yaml --sandbox-settings path/to/sandbox.json
```

| Flag                 | Required                                                                | Description                                                                           |
| -------------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `--prompt` / `-p`    | Yes, unless `--dry-run`; asked for when missing in the full-screen view | Path to the shared prompt sent to Claude for every task                               |
| `--tasks` / `-t`     | Yes; asked for when missing in the full-screen view                     | Path to the YAML task list that drives the loop                                       |
| `--sandbox-settings` | One of these two for any real run; not both                             | Path to a Claude Code settings file; sessions run in Claude's sandbox with it         |
| `--skip-permissions` | One of these two for any real run; not both                             | Run sessions with no sandbox and no permission checks. You're on your own (see below) |
| `--gate-timeout`     | No                                                                      | Time limit for every gate, like `90s`; overrides the task file's                      |
| `--commit`           | No                                                                      | Commit each completed task with git, after its gates pass                             |
| `--dry-run`          | No                                                                      | Validate the task file and report on it without running anything                      |
| `--no-tui`           | No                                                                      | Use plain output instead of the full-screen view                                      |
| `--install-skill`    | No                                                                      | Install the bundled `gralph-docs-writer` skill for Claude Code and exit               |
| `--version` / `-v`   | No                                                                      | Print version information and exit                                                    |

Run it in a terminal and you get a full-screen view of the run (see
[The full-screen view](#the-full-screen-view)). Pipe it, redirect it, run it in
CI, or pass `--no-tui`, and you get plain text output instead. `--dry-run` is
always plain.

Every real run needs either `--sandbox-settings` or `--skip-permissions`. There
is no default: see [Sandboxing sessions](#sandboxing-sessions).

### Checking a task file first

Want to know if a task file is good before you spend a run on it?

```bash
gralph -t tasks.yaml --dry-run
```

This uses the same checks as a real run, but never launches Claude or writes
anything. `--prompt` is ignored, and since nothing runs, you need neither
`--sandbox-settings` nor `--skip-permissions`. If the file is invalid, you get the error on
stderr and a non-zero exit. If it's valid, you get a table of each task's id,
state, and name. If the file has gates, you then get one line per gate showing
the timeout it would run under and where that came from (`flag`, `gate`, or
`default`), like `task 1 gate: go test ./...: 10m (default)`. Pass
`--gate-timeout` along with `--dry-run` to see what it would change. If you
pass `--sandbox-settings` too, gralph checks that file the same way a real run
would and prints `sandbox settings: <path>`. With `--commit`, it also checks
the work tree the way a real run would and prints `commit: <work tree root>`.
Last comes `<tasks path> is valid`, and exit zero.

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
        timeout: 10m
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
  it's done (see [Gates](#gates)). Each entry has `cmd` (required) and
  `timeout` (optional, a duration like `90s` or `10m`; when omitted, the gate
  uses the built-in default 10m, or `--gate-timeout` if that flag is set).
- Tasks run in file order. The `id` just identifies a task; it doesn't set the
  order.

### The gralph-docs-writer skill

Writing a good task file by hand is tedious, so gralph ships a Claude Code
skill, [gralph-docs-writer](src/skills/gralph-docs-writer/SKILL.md), that turns
a plan into a task file and shared prompt. It's built into the binary:

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
passes straight through to your terminal. The full-screen view adds
`--output-format stream-json --verbose` right after `--print` instead, so it
can show the session's activity live. It uses the same success and failure rules and saves the file the same
way.

Gralph saves the task file after every task, atomically (it writes a temporary
file and renames it over the original), so a crash never leaves you with half a
file. The catch: comments and custom formatting don't survive, and every task's
`state` gets written out explicitly.

### Sandboxing sessions

A sandbox is a fence the operating system puts around a program: it limits
which files the program can read and write and which hosts it can reach on the
network. Gralph runs unattended, so it makes you choose up front whether
sessions get one.

A run needs exactly one of these two flags:

- `--sandbox-settings <path>`: sessions run inside Claude Code's sandbox, set up
  by the file at that path.
- `--skip-permissions`: sessions run with no sandbox at all.

With neither, gralph stops with
`error: pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox`.
With both, it stops with
`error: --sandbox-settings and --skip-permissions cannot be used together`.
Either way it exits 1 before anything runs.

**What `--sandbox-settings` does.** The file is a normal Claude Code settings
file, in JSON. Gralph reads it once at startup and never writes to it. If it
can't be read or isn't a JSON object, gralph exits 1 with an error starting
`--sandbox-settings:`. On top of whatever the file says, gralph always sets
three keys:

| Key                                | Forced to | Why                                                                   |
| ---------------------------------- | --------- | --------------------------------------------------------------------- |
| `sandbox.enabled`                  | `true`    | Turns the sandbox on                                                  |
| `sandbox.allowUnsandboxedCommands` | `false`   | A session can't ask to run a command outside the sandbox              |
| `sandbox.failIfUnavailable`        | `true`    | If the sandbox can't start, the session fails instead of running bare |

Everything else in the file reaches Claude as you wrote it. Sessions then run as
`claude --print --permission-mode acceptEdits --settings <your settings>`:

- Claude may edit files inside the project.
- Shell commands run inside the sandbox.
- Anything that would normally stop and ask you first is refused, because
  nobody is there to answer.

#### Writing a settings file

1. **Start from this file.** It's for a Go project. Save it anywhere, for
   example next to the task file as `sandbox.json`.

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

   The paths are from one machine, so replace them with yours. Write the
   project and toolchain paths in full (absolute paths, starting with `/`):
   that's what was tested.

2. **Know what each part does.**

   - `denyRead: ["~/"]` hides your home directory (SSH keys, cloud
     credentials, other projects) from shell commands.
   - `allowRead` lets back in the project and whatever the build tools need to
     read.
   - `allowWrite` adds places outside the project that commands may write to.
     The project directory itself is always writable.
   - `allowedDomains` is the list of hosts shell commands may reach.
   - `strictAllowlist: true` refuses every other host outright.

   Leave `enabled`, `allowUnsandboxedCommands`, and `failIfUnavailable` out.
   Gralph sets them.

3. **Find your toolchain's paths.** For Go, run:

   ```bash
   go env GOROOT GOPATH GOCACHE
   ```

   It prints three paths. All three go in `allowRead`, and the `GOCACHE` one
   also goes in `allowWrite`. Only this Go setup was tested. For any other
   toolchain, use the same method: ask the tool where its install directory
   and its caches are (most have an `env` or `config` command for that), then
   allow reading both and writing the cache.

4. **Try it before a real run.**

   ```bash
   gralph -t tasks.yaml --dry-run --sandbox-settings sandbox.json
   ```

   This checks that the file is readable JSON. It doesn't check that the paths
   are right. For that, run a one-task file whose prompt only asks Claude to
   build and test the project, and read the output.

5. **Read the failures.** A file that's too tight shows up as errors in the
   session's output:

   | What you see                                                        | What to add                   |
   | ------------------------------------------------------------------- | ----------------------------- |
   | `read-only file system` on a path                                   | That path, to `allowWrite`    |
   | `No such file or directory` for a file you know exists in your home | Its directory, to `allowRead` |
   | `CONNECT tunnel failed, response 403`, or a download that hangs up  | The host, to `allowedDomains` |

   Add the narrowest path or host that fixes it, and run again.

6. **Keep it tight.** Don't allow all of `~/` to make an error go away. Don't
   add `excludedCommands` unless a tool truly can't run sandboxed: those
   commands run with no sandbox at all.

7. **Look up the rest.** Every other key is in Claude Code's
   [sandboxing reference](https://code.claude.com/docs/en/sandboxing). The
   file can hold any Claude Code settings, not only `sandbox`.

#### What the sandbox needs

Claude Code's sandbox uses bubblewrap and socat on Linux and Seatbelt on macOS
(two Linux packages you may need to install, and a tool built into macOS). If
the sandbox can't start, the session exits non-zero and the task fails like any
other. Gralph never falls back to running unsandboxed.

Sandboxed runs have been tested on Linux only. macOS should work the same way,
since gralph passes Claude the same flags and settings there, but it hasn't
been tried.

#### What the sandbox doesn't cover

- **Gates.** Gralph runs those itself, with no sandbox.
- **Commits.** With `--commit`, gralph runs git, and so the repository's hooks,
  itself, with no sandbox.
- **Your other Claude settings.** `~/.claude/settings.json` and the project's
  `.claude/settings.json` still apply on top.
- **Anything the file itself opens up.** `excludedCommands`, wide `allowWrite`
  paths, and allow rules do what they say.
- **Network limits are by hostname.** The sandbox checks the name a command
  asks for, not what the host behind it does.

#### Running without a sandbox: `--skip-permissions`

With `--skip-permissions`, sessions run as
`claude --print --dangerously-skip-permissions`, as gralph always did before
these flags existed. A session can then run any command, read, change, or
delete anything your user can, and reach anything on the network, with nobody
watching.

**If you pass this flag, you are on your own: you are responsible for whatever
the sessions do.** Use it only somewhere you wouldn't mind losing, such as a
container or a throwaway VM.

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

- Every gate runs under a timeout. The limit is determined by (in order): the
  `--gate-timeout` flag if set, the gate's `timeout` value if present, or the
  built-in default 10m. When a gate exceeds its timeout, gralph kills it,
  marks the task `failed` with `gate "<cmd>" timed out after <timeout>`, skips
  any remaining gates, and stops the run. If your task has slow but legitimate
  gates, set `timeout` on them (or use `--gate-timeout` for the whole run).
- If you have existing task files with no `timeout` set, they now get a 10m
  limit per gate (previously they had no limit). Gates that legitimately run
  longer need their own `timeout`.
- For a multi-line gate, error messages quote only the command's first line, so
  the reason stays readable.
- Write gates that check instead of fix: `test -z "$(gofmt -l .)"`, not
  `gofmt -w .`. Without `--commit`, nothing commits what a gate changes; with
  it, the task's whole change, including what a gate does, goes into one commit.
- Resetting a gate-failed task to `pending` runs its whole session again, not
  just the gates. Setting it to `completed` by hand skips them.
- `--dry-run` checks that `gates` is well formed. It never runs a gate.

### Committing tasks

Pass `--commit` and gralph commits each task for you. The order for a task is
session, gates, commit: the commit happens only after the session reported
`completed` and every gate passed. That's why the session shouldn't commit
its own work: it would be committing before the gates ran.

- With `--commit`, gralph stages what a plain `git add -A` would stage and
  commits it with the task's `name` as the message. Ignored files stay out.
- The commit holds everything the task changed, anywhere in the repository.
  A task that changed nothing gets no commit and is still `completed`.
- If git refuses the commit (a pre-commit hook fails, say), the task is
  `failed` with an error like `commit failed: exit status 1` and the run stops.
  Hooks always run.
- A failed task is never committed. Its changes stay in the work tree for you
  to look at.
- Gralph never pushes.

**The task file must be ignored by git or kept outside the repository.** Inside
a repository, gralph exits 1 with
`error: --commit needs the task file ignored by git or outside the repository: <path>`
if the file is tracked or not ignored. **The work tree must also be clean when
the run starts.** Otherwise gralph exits 1 with
`error: --commit needs a clean work tree; commit, stash, or remove:` and the
files in the way. The simplest setup is to ignore the run's files. In the
project's `.gitignore`:

```
tasks.yaml
prompt.md
```

or ignore the directory you keep them in. A sandbox settings file kept in the
project needs the same treatment.

After a failed task, the tree is dirty with that task's leftovers, so the next
`--commit` run is refused too. If it was the commit itself that failed, the
leftovers are also staged. Either throw them away
(`git restore --staged --worktree .` plus `git clean -f` for new files, or
`git stash -u`) and set the task back to `pending`, or finish the work yourself,
commit it, and set the task to `completed`.

Outside a git repository `--commit` does nothing: the run goes ahead, and plain
mode prints `commit: not a git repository, nothing will be committed`. The
full-screen view doesn't mention it. A repository git can't read is different:
if git fails for any other reason (a broken `.git/config`, say), gralph exits 1
with `error: --commit: git rev-parse: <git's message>` instead of running
without commits.

In plain mode gralph prints `commit: <name>` once something is staged, and git's
output passes through. The full-screen view shows `→ commit <name>` in the Claude
activity pane. `--dry-run --commit` applies the same startup checks: it validates
the task file is ignored or outside the repository, checks that the work tree is
clean, and prints `commit: <work tree root>` (or the not-a-repository line).
It never commits anything.

A few things to know:

- Changes made only inside a git submodule are not committed by gralph (the task
  is still `completed`, and the next `--commit` run is refused because the
  submodule shows as modified).
- A new git repository created inside the work tree by a task is committed as git
  would commit it, as an embedded repository link, not as files.

Tell Claude not to commit in your shared prompt when you use `--commit`. The
`gralph-docs-writer` skill does that for you when you say the run will use it.

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
- **Only run task files you trust.** The sandbox limits what a session can
  reach, but the prompt and task files are still code you're about to run. With
  `--skip-permissions` there's no limit at all. Gate commands and, with
  `--commit`, git and the repository's hooks are run by gralph itself, with no
  sandbox either way.
- **Every session starts from scratch.** A session sees the shared prompt and
  its own task, nothing else. If a task depends on earlier work, say so in the
  prompt, or make sure the repository shows it.
- **A failed task stops everything until you deal with it.** There's no retry.
  Gralph won't start while any task is `failed`: it prints the same table as
  `--dry-run`, exits 1, and doesn't touch the file. Fix whatever went wrong,
  set that task's `state` back to `pending` (or `completed`) by hand, and run
  again.
- **Ctrl-C stops the run cleanly.** In plain mode, SIGINT (Ctrl-C) or SIGTERM
  stops the current Claude session, gate, or commit step and the run. Gralph
  kills the process group. For git (when `--commit` is set), it asks the group
  to stop first with SIGTERM, and only kills it with SIGKILL if still alive
  after 2 seconds, so the repository is not left locked. The interrupted task
  and the file stay as they were, so the next run picks it up again. In
  the full-screen view, Ctrl-C is just a key (see `q` above). A SIGINT or
  SIGTERM from outside stops a running loop without asking, closes the view,
  prints `Run stopped by signal`, and exits 1. Once the run has ended, it just
  closes the view.
- **Unix only.** Gralph runs on Linux, macOS, and the BSDs. Windows isn't
  supported. Sandboxed runs need Linux or macOS; on the BSDs only
  `--skip-permissions` works.

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
make test      # unit tests: go test ./cmd/... ./internal/... (run from src/)
make analyze   # goimports, golangci-lint, govulncheck, gosec (tools must be on PATH)
```

One gotcha: the Go module is in `src/`. `go test ./...` from the repository root
finds no module; use `make test` instead, or run go commands from inside `src/`.
`tests/e2e` is its own Go module at the repository root, so `go test ./...` from
`src/` never includes it. Use `make build` to run the full e2e suite. The e2e
suite uses a fake `claude` on `PATH`, so you don't need the real Claude CLI or a
network connection.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags and embedded at build time:

```bash
git describe --tags --always
```

## License

[MIT](LICENSE)
