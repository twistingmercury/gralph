# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Gralph is a Go CLI that runs a "Ralph loop": for each open task it invokes `claude --print --dangerously-skip-permissions` once, with the prompt on stdin. It is **Claude Code only** by decision — an agent-agnostic runtime was built and rolled back (tags `v0.4.0`–`v0.5.2` hold it). Do not reintroduce provider abstraction, profiles, or `--agent-*` flags.

## Commands

```bash
make local      # native build -> .bin/local/gralph (version ldflags from git tags)
make test       # unit tests: go test -v ./internal/...
make e2e        # builds local binary, runs black-box tests in tests/e2e against it
make analyze    # goimports -w (rewrites files), golangci-lint, govulncheck, gosec
make build      # Docker release build (build/build.sh) — the only thing CI runs
```

Single tests:

```bash
go test ./internal/tasks -run TestNormalizeState -count=1
cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -run TestName -count=1 .
```

Easy to get wrong:

- `tests/e2e` is a **separate Go module**; `go test ./...` from the root never runs it.
- `make e2e` can print a **cached pass** after the binary changed (Go's test cache does not track the exec'd binary). Use `-count=1` when the result matters.
- `make install` only copies `.bin/local/gralph`. Run `make local install`, or the installed binary and its `--version` are stale.
- `build/build.sh` runs `docker compose up` **without `--build`**, so a local `make build` can pass e2e against a stale image. Force it: `docker compose -f tests/docker-compose.yaml up --build --exit-code-from tests`.
- `make build` runs lint, govulncheck, gosec, and unit tests **inside** `build/Dockerfile`, so any of them fails CI. Reproduce with `docker build --target builder -f build/Dockerfile .`. Its `-X` version ldflags must stay in sync with the Makefile's.

## Architecture

`cmd/main` → `internal/looper`. `internal/tasks` exists but is **not wired in yet**.

- **`internal/looper`** — `runLoop` still parses the task file as a **Markdown checklist** (`- [ ]` at column 0, first open item wins). After one `claude` invocation it re-reads the file: item changed or gone → next item, even if claude exited non-zero; unchanged → `ErrCycleFailed`, non-zero exit, no retry, file untouched. The prompt sent is the prompt file plus an appended `## Runtime paths` block, which the e2e fake parses — its format is a tested contract.
- **`internal/tasks`** — the YAML task model the looper is moving to. `ParseTasks` validates in a fixed order that tests pin. `normalizeState` must run **before** state validation (empty → `pending`; otherwise lowercased and trimmed). Stored `Name`/`Prompt` are never altered: the prompt goes to Claude verbatim.
- Cancellation flows only through the SIGINT/SIGTERM context from `cmd/main`. On darwin/linux the child runs in its own process group and the whole group is killed (`process_tree_unix.go`); elsewhere only the direct child. Because of `Setpgid`, Ctrl-C reaches only gralph — nothing may rely on claude receiving SIGINT itself.
- Test seams: unit tests inject a fake `commandRunner` into `runLoop`. E2E tests are black-box; they build `tests/e2e/testdata/fakeclaude` as a binary named `claude`, prepend it to `PATH` for the gralph process only, and drive it with `FAKECLAUDE_*` env vars because gralph passes fixed argv.

Direction (decided, not built): the looper will load `tasks.yaml` via `ParseTasks`, combine each pending task's prompt with the shared prompt, and start a new claude session per task, with **Gralph owning `state`**. How success is detected and written back is still open — ask rather than assume.

## Conventions

- **There is no retry.** The `--iterations` flag and the retry/abandon code were removed outright (last present in tag `v0.6.0`); an e2e test pins that `--iterations` is rejected. Do not reintroduce attempt counting.
- **Go unit tests use `github.com/stretchr/testify`** (`require` for preconditions, `assert` for checks); `internal/tasks/task_test.go` is the house style. E2E tests stay stdlib.
- **Never add `// #nosec` or `//nolint`**; fix the code. Older ones exist — don't add more, including when moving lines.
- YAML files use `.yaml`, never `.yml`.
- `skills/ralph-loop-docs-writer/` generates the `tasks.yaml` + `prompt.md` pair; its field rules must match `internal/tasks`. `scripts/install_skill.sh` installs it by `rm -rf` + copy into `~/.claude/skills` (override with `SKILLS_DIR`).
- `docs/architecture/` is **stale** (retry/abandon, the removed progress file, old flags) and will be realigned in one pass after the YAML work. Do not treat it as the contract or patch it piecemeal.
- PRs target `develop`. Versions are SemVer git tags; there is no CHANGELOG.
