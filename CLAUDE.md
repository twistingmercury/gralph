# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Gralph is a Go CLI that runs a "Ralph loop": for each open task it invokes `claude --print --dangerously-skip-permissions` once, with the prompt on stdin. On a terminal it runs in a full-screen Bubble Tea view (`internal/tui`) by default; otherwise, or with `--no-tui`, it runs in plain mode. It is **Claude Code only** by decision — an agent-agnostic runtime was built and rolled back (tags `v0.4.0`–`v0.5.2` hold it). Do not reintroduce provider abstraction, profiles, or `--agent-*` flags.

## Commands

```bash
make local      # native build -> .bin/local/gralph (version ldflags from git tags)
make test       # unit tests: go test -v ./internal/...
make analyze    # goimports -w (rewrites files), golangci-lint, govulncheck, gosec
make build      # Docker release build (build/build.sh) — the only thing CI runs; also the only
                # supported way to run the e2e suite (in a container, tests/docker-compose.yaml)
```

Single tests:

```bash
go test ./internal/tasks -run TestParseTasks_Valid -count=1
# debugging shortcut only — the suite is meant to run in the container
make local && cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -run TestName -count=1 .
```

Easy to get wrong:

- `tests/e2e` is a **separate Go module**; `go test ./...` from the root never runs it.
- A native e2e run can print a **cached pass** after the binary changed (Go's test cache does not track the exec'd binary); always pass `-count=1`. A failed signal test run natively can leave a sleeping fake `claude` behind — harmless in the container, where the suite belongs.
- `make install` only copies `.bin/local/gralph`. Run `make local install`, or the installed binary and its `--version` are stale.
- `make build` runs lint, govulncheck, gosec, and unit tests **inside** `build/Dockerfile`, so any of them fails CI. Reproduce with `docker build --target builder -f build/Dockerfile .`. Its `-X` version ldflags must stay in sync with the Makefile's.
- Both fake `claude` executables print a completed result line by default on exit 0; `FAKE_CLAUDE_OUTPUT` overrides stdout in both. Their other env vars differ: the unit fake uses `FAKE_CLAUDE_*` (`EXIT`, `RECORD`, `BLOCK`, `READY`, `ARGS`, `STDERR`, `BIG_EVENT`), the e2e fake `FAKECLAUDE_*` (`EXIT_CODE`, `RECORD_FILE`, `MODE`, …). Only the unit fake speaks stream-json, and only when its argv has `--output-format stream-json`; `FAKE_CLAUDE_OUTPUT` is then the result event's text. Read the fake's header comment before driving it.
- Validate a task file without running anything: `.bin/local/gralph -t <tasks.yaml> --dry-run`.

## Architecture

`cmd/main` → `internal/tui` → `internal/looper` → `internal/tasks` (`cmd/main` also calls `looper` directly). `looper` never imports `tui` or any Bubble Tea module; `tasks` imports neither. Bubble Tea is v2 only (`charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`); never import the `github.com/charmbracelet` v1 modules.

- **`cmd/main`** — mode selection: plain when `--dry-run`, `--no-tui`, or stdin or stdout is not a terminal; plain mode checks required flags as before and calls `looper.Start`/`looper.DryRun`. Otherwise `runTUI` loads the given paths with `LoadPrompt`/`LoadTasks` (a failed task prints the `PrintTasks` table and exits 1, as in plain mode), runs `tui.Setup` to ask for any missing `--prompt`/`--tasks` path, then `tui.Run`, and prints the one-line summary after the view closes. A Bubble Tea error exits 1 with a hint to rerun with `--no-tui`.
- **`internal/looper`**
  - `LoadPrompt` (trimmed; empty rejected) and `LoadTasks` (returns the list plus `ErrFailedTasks` when any task is `failed`) are shared by plain mode, the TUI, and the setup screen.
  - `Start` (plain): on `ErrFailedTasks` it prints the `PrintTasks` table and returns the error, launching and writing nothing. Otherwise it calls `Run(ctx, prompt, tl, tasksFile, nil)`.
  - `Run` → `runLoop` runs tasks in file order, skipping `completed`. The `report func(Event)` hook picks the path: `nil` → `runTaskPlain` (prompt echoed, claude stdout teed, stderr inherited — plain mode's exact output); non-nil → `runTaskStream`, which adds `--output-format stream-json --verbose` to the argv, writes nothing to gralph's stdout/stderr, and reports `TaskStarted`/`Activity`/`TaskFinished` events (stdout events parsed by `parseStreamLine`, stderr lines as-is), then a final `RunDone` from `Run`. `report` may be called from more than one goroutine.
  - Wire contract, identical on both paths: Claude gets `fmt.Sprintf("%s\n\n%s\n", prompt, task.String())` on stdin; both test suites golden-assert it.
  - Outcome comes from the last non-blank line (fence lines skipped) of stdout (plain) or of the stream's `result` event text, as JSON `{state, error}`. Only exit 0 + `completed` is success; JSON `failed`, a missing/invalid line, or a non-zero exit is `failed` (never an exit-code fallback). State and `error` are saved via `tasks.SaveTasks` after every task; a failure stops the run.
  - Cancelling `ctx` leaves the task's state and the file untouched.
  - `DryRun` (`--dry-run`, `--prompt` ignored, always plain) loads and prints the same table and exits 0; it never execs claude or writes a file.
- **`internal/tui`** — `Setup`/`SetupModel` is the path-entry screen (validates each entry with `LoadTasks`/`LoadPrompt`; Esc or ctrl+c cancels, exit 1). `Run` runs `looper.Run` in a goroutine with `report` forwarding events to the program (`Model`: prompt, tasks, and output panes plus a legend); `in progress` is display only and never saved. It exits 0 only when every task completed; a failed task keeps the view open with its error until `q`.
- Cancellation: the SIGINT/SIGTERM context from `cmd/main` is the only outside stop. In plain mode it goes straight to `Start`. In the TUI, `tui.Run` derives its own cancellable context from it and uses `tea.WithoutSignalHandler`: an outside signal cancels the loop at once, with no confirm, and the view quits with "Run stopped by signal". Inside the view the terminal is raw, so ctrl+c and `q` are keys: they open a `[y/N]` confirm, and only `y` cancels the run ("Run stopped by user"). Either stop resets the running task to pending on screen and exits 1. The child runs in its own process group and the whole group is killed (`process_tree_unix.go`). Because of `Setpgid`, Ctrl-C reaches only gralph — nothing may rely on claude receiving SIGINT itself.
- **`internal/tasks`** — the YAML task model. `ParseTasks` unmarshals into a `yaml.Node`, rejects non-core tags anywhere, then checks each `tasks` element in file order (must be a mapping; `id` a positive int16 YAML integer, `name`/`prompt` nonblank strings, `state`/`error` strings) before decoding it; any invalid element rejects the whole file. Errors read `tasks[<index>] (id <id>): <field>: <problem>` (id omitted when missing or invalid); file-level errors name no task. `state` omitted or `""` becomes `pending`; otherwise it must be exactly `pending`, `completed`, or `failed` — no trimming or case folding. Stored `Name`/`Prompt` are never altered: the prompt goes to Claude verbatim. The optional `Error` field is written by the looper on failure (omitempty), never by the session and never sent to Claude. `SaveTasks` re-marshals the task tree to YAML with 2-space indent and writes it atomically to a temporary file, then renames over the original.
- **Unix only.** Windows support was removed deliberately (no build target, no `!unix` fallback); do not add `runtime.GOOS == "windows"` branches or `.exe` handling back.
- Test seam: there is no injectable runner; `runLoop` execs inline. Both suites build a fake `claude` (`internal/looper/testdata/fakeclaude`, `tests/e2e/testdata/fakeclaude`) into a temp dir, put it first on `PATH` for the gralph process only, and drive it with env vars because gralph passes fixed argv (the unit fake also reads argv to choose stream-json). `internal/tui` tests drive `Model`/`SetupModel` directly or `tui.Run`/`tui.Setup` with test program options. E2E tests are black-box (no internal imports) and have no terminal, so they always run plain mode.

## Conventions

- **There is no retry.** The `--iterations` flag and the retry/abandon code were removed outright (last present in tag `v0.6.0`); an e2e test pins that `--iterations` is rejected. A failed task blocks every later run until a person fixes the cause and resets its `state` to `pending` (or `completed`) by hand. Do not reintroduce attempt counting.
- **Go tests use `github.com/stretchr/testify`** (`require` for preconditions, `assert` for checks); `internal/tasks/task_test.go` is the house style. `tests/e2e` has its own `go.mod` with its own testify requirement.
- **Never add `// #nosec` or `//nolint`**; fix the code. Older ones exist — don't add more, including when moving lines.
- YAML files use `.yaml`, never `.yml`.
- `skills/gralph-docs-writer/` generates the `tasks.yaml` + `prompt.md` pair; its field rules must match `internal/tasks`. Its `templates/` are the only authoritative task-file and prompt templates; do not add templates elsewhere. `skills/embed.go` embeds the folder in the binary; `gralph --install-skill` (`internal/skillinstall`, runs before the `--prompt`/`--tasks` checks) removes `~/.claude/skills/gralph-docs-writer` and writes the embedded copy, so the installed skill matches the binary. Runs and dry-runs call `skillinstall.Check` after the flag checks and exit 1 unless `~/.claude/skills/gralph-docs-writer/VERSION` carries this binary's skill hash, so the e2e suite runs gralph with a `HOME` where `TestMain` has already run `--install-skill` (`skillHome`); a test that needs a different `HOME` passes its own. `scripts/install_skill.sh` stays for development: it installs whatever is in the working tree by `rm -rf` + copy into `~/.claude/skills` (override with `SKILLS_DIR`).
- `docs/architecture/` files are named `NN_name.md` with no version suffix; the version lives only in each file's `Version`/`Date`/`Notes` metadata. Edit a doc in place and bump that metadata; do not create `_vNN` copies (the global `/arch-docs` skill does, so do not follow its file naming here). Keep the ADRs in `02_architectural_decisions.md` in step with design changes.
- PRs target `develop`. Versions are SemVer git tags; there is no CHANGELOG.
