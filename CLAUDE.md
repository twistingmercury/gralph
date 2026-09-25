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
go test ./internal/tasks -run TestNormalizeState -count=1
# debugging shortcut only — the suite is meant to run in the container
make local && cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -run TestName -count=1 .
```

Easy to get wrong:

- `tests/e2e` is a **separate Go module**; `go test ./...` from the root never runs it.
- A native e2e run can print a **cached pass** after the binary changed (Go's test cache does not track the exec'd binary); always pass `-count=1`. A failed signal test run natively can leave a sleeping fake `claude` behind — harmless in the container, where the suite belongs.
- `make install` only copies `.bin/local/gralph`. Run `make local install`, or the installed binary and its `--version` are stale.
- `build/build.sh` runs `docker compose up` **without `--build`**, so a local `make build` can pass e2e against a stale image. Force it: `docker compose -f tests/docker-compose.yaml up --build --exit-code-from tests`.
- `make build` runs lint, govulncheck, gosec, and unit tests **inside** `build/Dockerfile`, so any of them fails CI. Reproduce with `docker build --target builder -f build/Dockerfile .`. Its `-X` version ldflags must stay in sync with the Makefile's.
- Both fake `claude` executables print a completed result line by default on exit 0; override with `FAKE_CLAUDE_OUTPUT` to customize stdout.

## Architecture

`cmd/main` → `internal/looper` → `internal/tasks`.

- **`internal/looper`** — `Start` loads the prompt file (trimmed; empty/whitespace rejected) and `tasks.yaml` via `tasks.ParseTasks`, then `runLoop` walks tasks in file order skipping `completed` ones, builds `fmt.Sprintf("%s\n\n%s\n", prompt, task.String())`, echoes it, and execs `claude --print --dangerously-skip-permissions`. The looper reads the last non-blank line (skipping code fences) as JSON to determine outcome. Exit code 0 with `state: "completed"` marks the task `completed`, calls `tasks.SaveTasks` atomically, and continues. Any other outcome (JSON `failed`, missing/invalid result line, non-zero exit) marks `failed` and saves with `error` (the JSON `error`, else a fixed message or the exit error), reports `task <id>: <name> failed: <error>`, and stops; later tasks never run. That combined-prompt string is the wire contract — both test suites golden-assert it. SIGINT/SIGTERM leaves the task's state unchanged and does not rewrite the file.
- **`internal/tasks`** — the YAML task model. `ParseTasks` unmarshals into a `yaml.Node`, rejects non-core tags anywhere, then checks each `tasks` element in file order (must be a mapping; `id` a positive int16 YAML integer, `name`/`prompt` nonblank strings, `state`/`error` strings) before decoding it; any invalid element rejects the whole file. Errors read `tasks[<index>] (id <id>): <field>: <problem>` (id omitted when missing or invalid); file-level errors name no task. `state` omitted or `""` becomes `pending`; otherwise it must be exactly `pending`, `completed`, or `failed` — no trimming or case folding. Stored `Name`/`Prompt` are never altered: the prompt goes to Claude verbatim. The optional `Error` field is written by the looper on failure (omitempty), never by the session and never sent to Claude. `SaveTasks` re-marshals the task tree to YAML with 2-space indent and writes it atomically to a temporary file, then renames over the original.
- Cancellation flows only through the SIGINT/SIGTERM context from `cmd/main`. The child runs in its own process group and the whole group is killed (`process_tree_unix.go`). Because of `Setpgid`, Ctrl-C reaches only gralph — nothing may rely on claude receiving SIGINT itself.
- **Unix only.** Windows support was removed deliberately (no build target, no `!unix` fallback); do not add `runtime.GOOS == "windows"` branches or `.exe` handling back.
- Test seam: there is no injectable runner; `runLoop` execs inline. Both suites build a fake `claude` (`internal/looper/testdata/fakeclaude`, `tests/e2e/testdata/fakeclaude`) into a temp dir, put it first on `PATH` for the gralph process only, and drive it with env vars because gralph passes fixed argv. E2E tests are black-box (no internal imports).

The looper loads `tasks.yaml` via `ParseTasks`, combines pending or failed tasks with the shared prompt, starts a session per task, and owns writing `state` and `error` back. Outcome comes from the final non-blank output line (JSON with `state` and `error`); a missing or invalid line is `failed`, never a fallback to the exit code, and a non-zero exit is always `failed`.

## Conventions

- **There is no retry.** The `--iterations` flag and the retry/abandon code were removed outright (last present in tag `v0.6.0`); an e2e test pins that `--iterations` is rejected. Running gralph again re-runs failed tasks and any pending ones, but this is state recovery, not attempt counting. Do not reintroduce attempt counting.
- **Go tests use `github.com/stretchr/testify`** (`require` for preconditions, `assert` for checks); `internal/tasks/task_test.go` is the house style. `tests/e2e` has its own `go.mod` with its own testify requirement.
- **Never add `// #nosec` or `//nolint`**; fix the code. Older ones exist — don't add more, including when moving lines.
- YAML files use `.yaml`, never `.yml`.
- `skills/ralph-loop-docs-writer/` generates the `tasks.yaml` + `prompt.md` pair; its field rules must match `internal/tasks`. `scripts/install_skill.sh` installs it by `rm -rf` + copy into `~/.claude/skills` (override with `SKILLS_DIR`).
- `docs/architecture/` is **stale** (retry/abandon, the removed progress file, old flags) and will be realigned in one pass after the YAML work. Do not treat it as the contract or patch it piecemeal.
- PRs target `develop`. Versions are SemVer git tags; there is no CHANGELOG.
