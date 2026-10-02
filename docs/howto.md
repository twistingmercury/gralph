# Gralph HOWTO

## Table of Contents

- [What gralph is](#what-gralph-is)
- [Before you start](#before-you-start)
- [Installing gralph](#installing-gralph)
- [Your first run](#your-first-run)
- [The task file](#the-task-file)
- [How a run works](#how-a-run-works)
- [Sandboxing sessions](#sandboxing-sessions)
- [Gates](#gates)
- [Committing tasks](#committing-tasks)
- [Logging a run](#logging-a-run)
- [The full-screen view](#the-full-screen-view)
- [When things go wrong](#when-things-go-wrong)
- [Flag reference](#flag-reference)

## What gralph is

Gralph runs "Ralph loops" with Claude Code. You give it a list of tasks in a
YAML file and one shared prompt. It works through the list one task at a time,
starting a fresh `claude --print` session for each, so every task gets a clean
context instead of one long session that drifts.

## Before you start

You need the `claude` CLI on your `PATH`. Gralph calls it directly.

Two more tools are needed only if you use the feature that calls them:

- `sh`, if your task file has [gates](#gates). Gralph runs each gate through
  it.
- `git`, if you pass `--commit` (see [Committing tasks](#committing-tasks)).

Don't run gralph from inside a Claude session. It starts Claude itself, as a
subprocess, so run it from a normal terminal.

Gralph runs on Linux, macOS, and the BSDs. Sandboxed runs need Linux or macOS;
on the BSDs only `--skip-permissions` works.

There's no Windows release. If you want one, you can build it yourself: in
gralph's source, `build/Dockerfile` has a commented-out `GOOS=windows` line;
uncomment it and run `make build`, and you get `.bin/amd64/windows/gralph.exe`.
But **the Windows version itself hasn't been directly exercised**. It compiles;
nobody has run it. Going by the code, these are the known issues:

- Only `--skip-permissions` works, since Claude Code's sandbox needs Linux or
  macOS.
- Gates run through `sh`, so `sh` has to be on your `PATH`.
- Stopping a run stops only the process gralph started, not anything that
  process started. A stopped Claude session, gate, or commit can leave
  processes running, and a stopped git can leave its `index.lock` file behind.

If you're on Windows, the better bet is the Linux binary inside WSL2, with
Claude Code installed inside WSL2 as well. WSL2 is a real Linux, and Claude
Code's sandbox supports it (it needs bubblewrap and socat there, like any
Linux), so none of the issues above should apply. Gralph hasn't been tried on
WSL2 either, though. WSL1 won't do for sandboxed runs: Claude Code's sandbox
doesn't support it.

If you plan to use the sandbox, your system needs the tools
Claude's sandbox uses. On Linux, that's bubblewrap and socat (two packages you
may need to install). On macOS, it's Seatbelt, which is built in. If the
sandbox can't start during a run, the session exits non-zero and the task
fails. Gralph never falls back to running unsandboxed. See
[Sandboxing sessions](#sandboxing-sessions) for details.

## Installing gralph

You don't need Go, or the source. Every release has an archive for each
platform at <https://github.com/twistingmercury/gralph/releases>: Linux and
macOS, each for `amd64` (Intel and AMD) and `arm64` (Apple silicon and other
ARM machines). An archive holds three files: `gralph`, this guide
(`howto.md`), and `LICENSE`.

Set the three variables on the first three lines, then run the rest as it is:

- `VERSION` is the release you want, from the releases page.
- `OS` is `linux`, or `darwin` for macOS.
- `ARCH` is `amd64` or `arm64`.

```bash
VERSION=v0.9.8
OS=linux
ARCH=amd64

BASE="https://github.com/twistingmercury/gralph/releases/download/${VERSION}"
curl -fsSLO "${BASE}/gralph_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "${BASE}/checksums.txt"

sha256sum -c --ignore-missing checksums.txt
```

Not sure which `ARCH` you have? `uname -m` prints `x86_64` for `amd64`, and
`arm64` or `aarch64` for `arm64`.

On macOS there's no `sha256sum`. Use this as the last line instead:

```bash
shasum -a 256 -c --ignore-missing checksums.txt
```

The check should print the archive's name followed by `OK`. If it doesn't,
stop here: the download is damaged, so delete it and download again.

Then, in the same terminal, put `gralph` in place:

```bash
mkdir -p ~/.local/bin
tar -xzf "gralph_${VERSION}_${OS}_${ARCH}.tar.gz" -C ~/.local/bin gralph

gralph --version
gralph --install-skill
```

That unpacks only `gralph`, straight into `~/.local/bin`. This guide and the
license are in the archive too. If you want a copy, unpack them into a folder
of their own:

```bash
mkdir -p ~/.local/share/gralph
tar -xzf "gralph_${VERSION}_${OS}_${ARCH}.tar.gz" -C ~/.local/share/gralph howto.md LICENSE
```

If `gralph --version` says "command not found", `~/.local/bin` isn't on your
`PATH`. Add it with the line for your shell, then open a new terminal.

For bash:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
```

For zsh (the macOS default):

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

The macOS archives haven't been tried on a Mac yet. One thing is known: if you
download an archive with a browser instead of `curl`, macOS quarantines it and
refuses to run `gralph`. Clear that with:

```bash
xattr -d com.apple.quarantine ~/.local/bin/gralph
```

To upgrade, do the same steps with the new version. Don't skip
`gralph --install-skill`: gralph won't start a run while the installed skill
comes from a different version. If `gralph --version` still prints the old
version, an older copy sits earlier on your `PATH` (`~/go/bin/gralph`, if you
once built from source). `which gralph` shows which one runs; remove it.

If you'd rather build gralph yourself, get the source and run
`make local install`. That needs Go and make.

## Your first run

Here's the quick path from zero to a working run:

1. **Install the skill.** Run `gralph --install-skill` once. It replaces
   `~/.claude/skills/gralph-docs-writer/` with the copy that matches your
   gralph binary. The skill helps you write task files and prompts. You can
   also write them by hand.

2. **Get a task file and shared prompt.** The skill generates both from a
   plan. Or write them yourself (see [The task file](#the-task-file) for the
   format).

3. **Write a sandbox settings file.** This tells Claude's sandbox what your
   project needs to access (see [Writing a settings file](#writing-a-settings-file)).

4. **Validate before running.** Check your task file and settings:

   ```bash
   gralph -t tasks.yaml --dry-run --sandbox-settings sandbox.json
   ```

   This validates the files without running anything. Fix any errors it finds.

5. **Run it.** Once validation passes:

   ```bash
   gralph -p prompt.md -t tasks.yaml --sandbox-settings sandbox.json
   ```

   You'll see a full-screen view of the run (see
   [The full-screen view](#the-full-screen-view)). Press `q` to close it when
   done.

## The task file

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
skill, `gralph-docs-writer`, that turns a plan into a task file and shared
prompt. It's built into the binary:

```bash
gralph --install-skill
```

That replaces `~/.claude/skills/gralph-docs-writer/` with the copy that matches
your gralph binary and prints the path. A run or `--dry-run` won't start
(exit 1) while the skill is missing or came from a different gralph version;
it tells you to run `gralph --install-skill`.

## How a run works

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
`--log-dir` is ignored: a dry run records nothing.
Last comes `<tasks path> is valid`, and exit zero.

If any task is `failed` (a real run would refuse to start), the table comes
after `Some tasks failed previous runs:` instead, and each failed row ends in
`← Needs review!`.

## Sandboxing sessions

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

### What `--sandbox-settings` does

The file is a normal Claude Code settings file, in JSON. Gralph reads it once at
startup and never writes to it. If it can't be read or isn't a JSON object,
gralph exits 1 with an error starting `--sandbox-settings:`. On top of
whatever the file says, gralph always sets three keys:

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

### Writing a settings file

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

### What the sandbox needs

Claude Code's sandbox uses bubblewrap and socat on Linux and Seatbelt on macOS
(two Linux packages you may need to install, and a tool built into macOS). If
the sandbox can't start, the session exits non-zero and the task fails like any
other. Gralph never falls back to running unsandboxed.

Sandboxed runs have been tested on Linux only. macOS should work the same way,
since gralph passes Claude the same flags and settings there, but it hasn't
been tried.

### What the sandbox doesn't cover

- **Gates.** Gralph runs those itself, with no sandbox.
- **Commits.** With `--commit`, gralph runs git, and so the repository's hooks,
  itself, with no sandbox.
- **Your other Claude settings.** `~/.claude/settings.json` and the project's
  `.claude/settings.json` still apply on top.
- **Anything the file itself opens up.** `excludedCommands`, wide `allowWrite`
  paths, and allow rules do what they say.
- **Network limits are by hostname.** The sandbox checks the name a command
  asks for, not what the host behind it does.

### Running without a sandbox: `--skip-permissions`

With `--skip-permissions`, sessions run as
`claude --print --dangerously-skip-permissions`, as gralph always did before
these flags existed. A session can then run any command, read, change, or
delete anything your user can, and reach anything on the network, with nobody
watching.

**If you pass this flag, you are on your own: you are responsible for whatever
the sessions do.** Use it only somewhere you wouldn't mind losing, such as a
container or a throwaway VM.

## Gates

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

## Committing tasks

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

```text
tasks.yaml
prompt.md
```

or ignore the directory you keep them in. A sandbox settings file kept in the
project needs the same treatment. So does a `--log-dir` folder inside the
repository; gralph checks that one for you (see [Logging a run](#logging-a-run)).

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

## Logging a run

Once you close the full-screen view, the Claude activity pane is gone. Pass
`--log-dir <path>` and gralph keeps a record of the run instead:

```bash
gralph -p prompt.md -t tasks.yaml --sandbox-settings sandbox.json --log-dir logs
```

Each run gets its own folder under that path, named after the local time it
started:

```text
logs/
  20261001T140211/
    run.jsonl      what ran and when
    task-1.log     everything task 1's activity pane showed
    task-2.log
```

Gralph writes these itself. Claude is told nothing about them.

**`run.jsonl`** is the ledger: one JSON object per line, written as things
happen. Every line has a `time` and an `event`, and every line about a task
has that task's id as `task`:

| `event`            | What else it holds                                                                                                                                                                        |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `run_started`      | Gralph's `version`, the `tasks_file` and `prompt_file` paths, `permissions` (`sandbox` or `skip`), `sandbox_settings` and `gate_timeout` if passed, and `commit` (`true` or `false`)      |
| `task_started`     | The task's `name`                                                                                                                                                                         |
| `session_finished` | What the session reported (`state`, and `error` if there is one) and how long it took (`duration`)                                                                                        |
| `gate_finished`    | The gate's command (`cmd`, first line only), its `result` (`passed`, `failed`, or `timed_out`), the `timeout` it ran under, and its `duration`                                            |
| `committed`        | The new commit's `hash`                                                                                                                                                                   |
| `task_finished`    | The task's final `state`, its `error` if it failed, and the `duration` of the whole task                                                                                                  |
| `run_finished`     | The `result` (`completed`, `failed`, or `stopped`), the `error` if any, and the `duration` of the run                                                                                     |

So this lists every gate in a run with its result and duration:

```bash
jq -r 'select(.event == "gate_finished") | [.task, .result, .duration, .cmd] | @tsv' logs/20261001T140211/run.jsonl
```

**`task-<id>.log`** is the detail: every line the Claude activity pane showed
for that task, each with the time it arrived. That's Claude's text, a
`→ <tool> <target>` line per tool call, anything on stderr, each gate and its
output, and the commit and git's output.

A few things to know:

- **Full-screen view only.** With `--no-tui`, or when gralph isn't on a
  terminal, a run with `--log-dir` exits 1 with
  `error: --log-dir only works with the full-screen view`. Plain mode already
  prints everything, so redirect it: `gralph ... --no-tui > run.log 2>&1`.
  `--dry-run` ignores the flag.
- **The files are private to you.** The folder is created with mode `0700` and
  the files with `0600`, because the record holds whatever Claude, a gate, or
  a git hook printed. That can include secrets. Look before you share one.
- **Nothing is cleaned up.** Every run adds a folder. Delete the old ones when
  you're done with them.
- **A skipped task leaves no trace.** Tasks already `completed` when the run
  starts aren't in the ledger and get no detail file.
- **A stopped run says `stopped`.** The task that was running has no
  `task_finished` line, which matches the task file: it's still pending. The
  ledger doesn't say whether you stopped it or a signal did.
- **If gralph can't write the record, it stops the run.** The view shows
  `Run stopped: log: <error>` and gralph exits 1. The ledger then just ends,
  with no `run_finished` line. If the folder can't be created in the first
  place, gralph exits 1 with an error starting `--log-dir:` before the view
  opens. That covers two runs started in the same second: the second one's
  folder already exists, so it refuses to start.
- **With `--commit`, keep the logs out of git.** A log folder inside the
  repository must be ignored, or gralph exits 1 with
  `error: --commit needs the log directory ignored by git or outside the repository: <path>`.
  Add it to `.gitignore` (for example `logs/`), or point `--log-dir` somewhere
  outside the repository.

## The full-screen view

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

To keep what the view showed after it closes, pass `--log-dir` (see
[Logging a run](#logging-a-run)).

Forgot `--tasks` or `--prompt`? A setup screen asks for each missing path,
tasks file first. `enter` checks the path with the same rules a run uses and
shows any problem right under the field. `esc` or `ctrl+c` quits with
`error: setup cancelled` and exit 1. As in plain mode, a task file with a
`failed` task prints the task table and exits 1 before the view ever opens.

## When things go wrong

- **A failed task stops everything until you deal with it.** There's no retry.
  Gralph won't start while any task is `failed`: it prints the same table as
  `--dry-run`, exits 1, and doesn't touch the file. Fix whatever went wrong,
  set that task's `state` back to `pending` (or `completed`) by hand, and run
  again.
- **Every session starts from scratch.** A session sees the shared prompt and
  its own task, nothing else. If a task depends on earlier work, say so in the
  prompt, or make sure the repository shows it.
- **Only run task files you trust.** The sandbox limits what a session can
  reach, but the prompt and task files are still code you're about to run. With
  `--skip-permissions` there's no limit at all. Gate commands and, with
  `--commit`, git and the repository's hooks are run by gralph itself, with no
  sandbox either way.
- **Ctrl-C stops the run cleanly.** In plain mode, SIGINT (Ctrl-C) or SIGTERM
  stops the current Claude session, gate, or commit step and the run. Gralph
  kills the process group. For git (when `--commit` is set), it asks the group
  to stop first with SIGTERM, and only kills it with SIGKILL if still alive
  after 2 seconds, so the repository is not left locked. The interrupted task
  and the file stay as they were, so the next run picks it up again. In
  the full-screen view, Ctrl-C is just a key (see
  [The full-screen view](#the-full-screen-view)). A SIGINT or SIGTERM from
  outside stops a running loop without asking, closes the view, prints
  `Run stopped by signal`, and exits 1. Once the run has ended, it just closes
  the view.

Still stuck, or got a question or an idea? Ask in
[Discussions](https://github.com/twistingmercury/gralph/discussions).

## Flag reference

```bash
gralph --prompt path/to/prompt.md --tasks path/to/tasks.yaml --sandbox-settings path/to/sandbox.json
```

| Flag                 | Required                                                                | Description                                                                                                                 |
| -------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `--prompt` / `-p`    | Yes, unless `--dry-run`; asked for when missing in the full-screen view | Path to the shared prompt sent to Claude for every task                                                                     |
| `--tasks` / `-t`     | Yes; asked for when missing in the full-screen view                     | Path to the YAML task list that drives the loop                                                                             |
| `--sandbox-settings` | One of these two for any real run; not both                             | Path to a Claude Code settings file; sessions run in Claude's sandbox with it                                               |
| `--skip-permissions` | One of these two for any real run; not both                             | Run sessions with no sandbox and no permission checks. You're on your own (see [Sandboxing sessions](#sandboxing-sessions)) |
| `--gate-timeout`     | No                                                                      | Time limit for every gate, like `90s`; overrides the task file's                                                            |
| `--commit`           | No                                                                      | Commit each completed task with git, after its gates pass                                                                   |
| `--log-dir`          | No                                                                      | Directory to keep a record of the run in; full-screen view only                                                             |
| `--dry-run`          | No                                                                      | Validate the task file and report on it without running anything                                                            |
| `--no-tui`           | No                                                                      | Use plain output instead of the full-screen view                                                                            |
| `--install-skill`    | No                                                                      | Install the bundled `gralph-docs-writer` skill for Claude Code and exit                                                     |
| `--version` / `-v`   | No                                                                      | Print version information and exit                                                                                          |

Run it in a terminal and you get a full-screen view of the run (see
[The full-screen view](#the-full-screen-view)). Pipe it, redirect it, run it in
CI, or pass `--no-tui`, and you get plain text output instead. `--dry-run` is
always plain.

Every real run needs either `--sandbox-settings` or `--skip-permissions`. There
is no default: see [Sandboxing sessions](#sandboxing-sessions).
