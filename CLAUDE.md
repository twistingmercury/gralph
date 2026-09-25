# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Gralph is a Go CLI that runs a "Ralph loop": for each open task it invokes `claude --print --dangerously-skip-permissions` once, with the prompt on stdin. It is **Claude Code only** by decision — an agent-agnostic runtime was built and rolled back (tags `v0.4.0`–`v0.5.2` hold it). Do not reintroduce provider abstraction, profiles, or `--agent-*` flags.

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
- Both fake `claude` executables print a completed result line by default on exit 0; `FAKE_CLAUDE_OUTPUT` overrides stdout in both. Their other env vars differ: the unit fake uses `FAKE_CLAUDE_*` (`EXIT`, `RECORD`, `BLOCK`), the e2e fake `FAKECLAUDE_*` (`EXIT_CODE`, `RECORD_FILE`, `MODE`, …). Read the fake's header comment before driving it.
- Validate a task file without running anything: `.bin/local/gralph -t <tasks.yaml> --dry-run`.

## Architecture

`cmd/main` → `internal/looper` → `internal/tasks`.

- **`internal/looper`**
  - `Start`: loads the prompt (trimmed; empty rejected) and `tasks.yaml`. If any task is `failed` it prints the `printTasks` table and returns an error, launching and writing nothing. Otherwise `runLoop` runs tasks in file order, skipping `completed`.
  - Wire contract: Claude gets `fmt.Sprintf("%s\n\n%s\n", prompt, task.String())` on stdin via `claude --print --dangerously-skip-permissions`; both test suites golden-assert it.
  - Outcome comes from the last non-blank stdout line (fence lines skipped) as JSON `{state, error}`. Only exit 0 + `completed` is success; JSON `failed`, a missing/invalid line, or a non-zero exit is `failed` (never an exit-code fallback). State and `error` are saved via `tasks.SaveTasks` after every task; a failure stops the run.
  - SIGINT/SIGTERM leaves the task's state and the file untouched.
  - `DryRun` (`--dry-run`, `--prompt` ignored) loads and prints the same table and exits 0; it never execs claude or writes a file.
- **`internal/tasks`** — the YAML task model. `ParseTasks` unmarshals into a `yaml.Node`, rejects non-core tags anywhere, then checks each `tasks` element in file order (must be a mapping; `id` a positive int16 YAML integer, `name`/`prompt` nonblank strings, `state`/`error` strings) before decoding it; any invalid element rejects the whole file. Errors read `tasks[<index>] (id <id>): <field>: <problem>` (id omitted when missing or invalid); file-level errors name no task. `state` omitted or `""` becomes `pending`; otherwise it must be exactly `pending`, `completed`, or `failed` — no trimming or case folding. Stored `Name`/`Prompt` are never altered: the prompt goes to Claude verbatim. The optional `Error` field is written by the looper on failure (omitempty), never by the session and never sent to Claude. `SaveTasks` re-marshals the task tree to YAML with 2-space indent and writes it atomically to a temporary file, then renames over the original.
- Cancellation flows only through the SIGINT/SIGTERM context from `cmd/main`. The child runs in its own process group and the whole group is killed (`process_tree_unix.go`). Because of `Setpgid`, Ctrl-C reaches only gralph — nothing may rely on claude receiving SIGINT itself.
- **Unix only.** Windows support was removed deliberately (no build target, no `!unix` fallback); do not add `runtime.GOOS == "windows"` branches or `.exe` handling back.
- Test seam: there is no injectable runner; `runLoop` execs inline. Both suites build a fake `claude` (`internal/looper/testdata/fakeclaude`, `tests/e2e/testdata/fakeclaude`) into a temp dir, put it first on `PATH` for the gralph process only, and drive it with env vars because gralph passes fixed argv. E2E tests are black-box (no internal imports).

## Conventions

- **There is no retry.** The `--iterations` flag and the retry/abandon code were removed outright (last present in tag `v0.6.0`); an e2e test pins that `--iterations` is rejected. A failed task blocks every later run until a person fixes the cause and resets its `state` to `pending` (or `completed`) by hand. Do not reintroduce attempt counting.
- **Go tests use `github.com/stretchr/testify`** (`require` for preconditions, `assert` for checks); `internal/tasks/task_test.go` is the house style. `tests/e2e` has its own `go.mod` with its own testify requirement.
- **Never add `// #nosec` or `//nolint`**; fix the code. Older ones exist — don't add more, including when moving lines.
- YAML files use `.yaml`, never `.yml`.
- `skills/gralph-docs-writer/` generates the `tasks.yaml` + `prompt.md` pair; its field rules must match `internal/tasks`. Its `templates/` are the only authoritative task-file and prompt templates; do not add templates elsewhere. `skills/embed.go` embeds the folder in the binary; `gralph --install-skill` (`internal/skillinstall`, runs before the `--prompt`/`--tasks` checks) removes `~/.claude/skills/gralph-docs-writer` and writes the embedded copy, so the installed skill matches the binary. Runs and dry-runs call `skillinstall.Check` after the flag checks and exit 1 unless `~/.claude/skills/gralph-docs-writer/VERSION` carries this binary's skill hash, so the e2e suite runs gralph with a `HOME` where `TestMain` has already run `--install-skill` (`skillHome`); a test that needs a different `HOME` passes its own. `scripts/install_skill.sh` stays for development: it installs whatever is in the working tree by `rm -rf` + copy into `~/.claude/skills` (override with `SKILLS_DIR`).
- `docs/architecture/` files are named `NN_name.md` with no version suffix; the version lives only in each file's `Version`/`Date`/`Notes` metadata. Edit a doc in place and bump that metadata; do not create `_vNN` copies (the global `/arch-docs` skill does, so do not follow its file naming here). Keep the ADRs in `02_architectural_decisions.md` in step with design changes.
- PRs target `develop`. Versions are SemVer git tags; there is no CHANGELOG.
