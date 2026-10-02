# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Read these first

This file does not repeat what the project's own documents say. Read the one that covers the work before starting:

- `README.md` — what gralph is, how a run works, the key considerations, and the build, test, and release commands ("Development Considerations").
- `docs/howto.md` — every flag, message, and user-visible behaviour.
- `docs/architecture/03_system_architecture.md` — how each package works, function by function.
- `docs/architecture/02_architectural_decisions.md` — why; the ADR numbers below point there.
- `docs/dependency_graph.md` — every package and import edge.

What follows is only what those leave out: the rules for changing the code, and the traps.

## Commands

The `make` targets and `bats tests/bats` are in the README. Not there:

```bash
cd src && go test ./internal/tasks -run TestParseTasks_Valid -count=1
# debugging shortcut only — the suite is meant to run in the container
make local && cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -run TestName -count=1 .
```

Easy to get wrong:

- `make analyze` runs `goimports -w`, which rewrites files.
- A native e2e run can print a **cached pass** after the binary changed (Go's test cache does not track the exec'd binary); always pass `-count=1`. A failed signal test run natively can leave a sleeping fake `claude` behind — harmless in the container, where the suite belongs.
- `make build` runs lint, govulncheck, gosec, and unit tests **inside** `build/Dockerfile`, so any of them fails CI. Reproduce with `docker build --target builder -f build/Dockerfile .`. Its `-X` version ldflags must stay in sync with the Makefile's.
- Both fake `claude` executables print a completed result line by default on exit 0; `FAKE_CLAUDE_OUTPUT` overrides stdout in both. Their other env vars differ: the unit fake uses `FAKE_CLAUDE_*` (`EXIT`, `RECORD`, `BLOCK`, `BLOCK_ON`, `READY`, `ARGS`, `STDERR`, `BIG_EVENT`), the e2e fake `FAKECLAUDE_*` (`EXIT_CODE`, `RECORD_FILE`, `MODE`, …). Only the unit fake speaks stream-json, and only when its argv has `--output-format stream-json`; `FAKE_CLAUDE_OUTPUT` is then the result event's text. Read the fake's header comment before driving it.
- Every e2e test that starts a real run passes `--skip-permissions` or `--sandbox-settings`; without one gralph exits 1 before claude starts. Neither fake `claude` enforces anything: they only record the argv.
- The e2e suite has no terminal, so it always runs plain mode and cannot exercise `--log-dir` logging; it only pins that plain mode refuses the flag and a dry run ignores it. The record is covered by the unit tests in `internal/runlog`, `internal/looper`, and `internal/tui`.
- E2E tests are black-box: no internal imports. They run gralph with a `HOME` where `TestMain` already ran `--install-skill` (`skillHome`), because a run refuses a stale skill; a test that needs a different `HOME` passes its own.
- `build/package.sh` needs `BUILD_VER` (`vMAJOR.MINOR.PATCH`). `tests/bats/test_helper/` is vendored `bats-support` and `bats-assert`, copied unchanged — do not edit or lint those files.

## Architecture

Package paths are relative to `src/`. Doc 03 has a section per package; this is the map and the rules.

Imports: `cmd/main` → `internal/tui` → `internal/looper` → `internal/tasks` (`cmd/main` also calls `looper` directly), and `cmd/main` → `internal/runlog` → `internal/looper`. `looper` never imports `tui`, `runlog`, or any Bubble Tea module; `tui` never imports `runlog`; `tasks` imports none of them. Bubble Tea is v2 only (`charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`); never import the `github.com/charmbracelet` v1 modules.

- **`cmd/main`** — flag checks and mode selection (`looper.Start`/`looper.DryRun` for plain mode, `runTUI` otherwise). Startup checks run in a fixed order, so keep it: `--gate-timeout`, plain mode's required flags, `--log-dir`, the session flags, the skill check.
- **`internal/looper`** — the loop, session flags (ADR-014), gates (ADR-013), commit (ADR-015), process groups, and the `Event`s it reports. The `report func(Event)` hook picks the path: `nil` is plain, non-nil is the stream path.
- **`internal/tui`** — the setup screen and the run view (ADR-011, ADR-012).
- **`internal/runlog`** — the `--log-dir` record (ADR-016). Standard library only.
- **`internal/tasks`** — the YAML task model (ADR-002, ADR-004).
- **`internal/skillinstall`**, **`skills/`** — the embedded skill and the stale-skill check (ADR-009, ADR-010).

Invariants that are easy to break:

- **Wire contract.** Claude gets `fmt.Sprintf("%s\n\n%s\n", prompt, task.String())` on stdin, the same on both paths; both test suites golden-assert it. Gates, the `error` field, and anything about gralph are never sent to Claude. A task's `name` and `prompt` are stored and sent verbatim.
- **One place for the command and the outcome.** Both runners build the command with `claudeCmd` and resolve the result with `finishTask`. Do not fork either, and never add an exit-code fallback to the outcome rule.
- **Plain mode's output is a contract (ADR-011).** The stream path writes nothing to gralph's stdout or stderr; the plain path reports no events.
- **A cancel changes nothing on disk.** Cancelling `ctx` leaves the task's state and the file untouched, whether a session, a gate, or the commit was running. `in progress` is display only and never saved. The `--gate-timeout` value is never written to the task file.
- **`report` is called from more than one goroutine.** Anything that listens must be safe for that.
- **Children run in their own process group.** Because of `Setpgid`, Ctrl-C reaches only gralph: nothing may rely on claude receiving SIGINT. Git gets SIGTERM first so it can remove `index.lock`.
- **Git commands are built by `(*Repo).git`**, which appends arguments to a constant `git` command; keep it that way so gosec stays quiet without a suppression. Gralph never pushes, resets, stashes, or passes `--no-verify`.
- **Windows.** Leave the `GOOS=windows` line in `build/Dockerfile` commented out. Keep the Unix/other split by build constraint (`process_tree_other.go`): no `runtime.GOOS == "windows"` branches, Windows features, or Windows tests without the owner asking. Nothing in CI compiles for Windows, so after changing `internal/looper` process handling run `GOOS=windows go build ./cmd/main` from `src/`.
- **No injectable runner.** `runLoop` execs inline; tests put a fake `claude` first on `PATH` for the gralph process only.

## Conventions

Do not reintroduce, by decision:

- **An agent abstraction.** Gralph is Claude Code only; a provider-agnostic runtime was built and rolled back (tags `v0.4.0`–`v0.5.2`). No profiles, no `--agent-*` flags (ADR-001).
- **Retry.** `--iterations` and attempt counting were removed (last in `v0.6.0`); an e2e test pins that the flag is rejected.
- **A default for the two permission flags**, or a fallback to an unsandboxed run (ADR-014).
- **Git without `--commit`**: no default, no task-file key, no cleaning, resetting, or stashing a work tree (ADR-015).
- **Logging without `--log-dir`, or in plain mode**: no default location, task-file key, or rotation, and a session never writes or reads the record (ADR-016).
- **Automatic releases.** The release workflow is started by hand and leaves a draft (ADR-017). No tag-push trigger, publishing without a draft, Windows archive, install script, or package-manager formula without the owner asking.

Code:

- **Go tests use `github.com/stretchr/testify`** (`require` for preconditions, `assert` for checks); `internal/tasks/task_test.go` is the house style. `tests/e2e` has its own `go.mod` with its own testify requirement.
- **Never add `// #nosec` or `//nolint`**; fix the code. Older ones exist — don't add more, including when moving lines. Only the owner can approve a specific suppression. One exists: `// #nosec G204` on the `sh -c` call in `internal/looper/gates.go`, because running task-file commands is what a gate is (ADR-013). Keep it, with its reason; do not hide the command from the scanner, and do not treat it as a license for others.
- **Code style** — applies to every line written or moved, not just the lines a task names:
  - Never-nester: invert conditions to return early; move non-trivial loop and `case` bodies into their own functions.
  - A blank line after every `if` statement (after its closing brace), except where the next line is the enclosing block's `}` or an `else`.
  - Callbacks longer than a line or two are named functions, not inline func literals.
  - Never pass a function call as an argument: call it, assign the result to a named variable, and pass the variable. That includes getters in format calls (`repo.Root()`, `task.String()`). Exempt: type conversions and built-ins (`string(x)`, `len(x)`, `max(a, b)`, `lipgloss.Color("12")`), `context.Background()` and `os.Environ()`, functions that return a callback (`fs.WalkDir(fsys, root, copyTo(dest))`), and chained builders (`lipgloss.NewStyle().Bold(true)`).
  - Comments explain *why* code exists or is shaped the way it is, not what it does.
- YAML files use `.yaml`, never `.yml`.

Keeping things in step:

- `src/skills/gralph-docs-writer/` field rules must match `internal/tasks`. Its `templates/` are the only task-file and prompt templates; do not add templates elsewhere. `scripts/install_skill.sh` is a development shortcut that copies the working tree's skill into `~/.claude/skills`.
- A change to user-visible behaviour (a flag, an error message, output) updates `docs/howto.md` in the same change; the README keeps only a summary that links there.
- A change to how a package works updates `03_system_architecture.md` in the same change, and a design change updates the ADRs. Do not grow this file with that detail.
- A new or removed package or import edge updates `docs/dependency_graph.md` in the same change.
- The release archive's names (`gralph_<version>_<os>_<arch>.tar.gz`) and contents (`gralph`, `howto.md`, `LICENSE`) are what the HOWTO tells users to download; change the three together.

Docs:

- `docs/architecture/` files are named `NN_name.md` with no version suffix; the version lives only in each file's `Version`/`Date`/`Notes` metadata. Edit in place and bump the metadata; do not create `_vNN` copies (the global `/arch-docs` skill does, so do not follow its file naming here).
- The HOWTO ships inside the release archives, so it must not link to any file in the repository.
- Command blocks a reader is meant to paste (the HOWTO, the README's Install section) carry no trailing `#` comments: an interactive zsh runs `#` as a command, so `VAR=value   # note` never sets the variable. Put the note in the text above the block.
- PRs target `develop`. Versions are SemVer git tags; there is no CHANGELOG. The `VERSION=` examples in `README.md` and `docs/howto.md` name a real release; set them in the release's "bump version" commit.
