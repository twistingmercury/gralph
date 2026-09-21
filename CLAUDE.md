# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Gralph is a Go CLI that runs a "Ralph loop" over a Markdown PRD checklist: it finds the first open `- [ ]` item, invokes a user-configured noninteractive AI agent with an assembled prompt, and retries until the item changes or the attempt limit is hit. It is deliberately **agent agnostic** — core code must not name or special-case a provider.

## Commands

```bash
make local          # quick native build -> .bin/local/gralph (version ldflags from git)
make test           # unit tests: go test -v ./internal/...
make e2e            # builds local binary, runs black-box tests in tests/e2e against it
make e2e-race       # same with -race
make docs-check     # --help output vs docs/cli-help.txt, plus every local Markdown link in the repo
make analyze        # goimports -w (rewrites files in place), golangci-lint, govulncheck, gosec
make verify         # full release acceptance gate (everything above + Docker build + artifact checks)
make build          # Docker-first release build (build/build.sh); this is what CI runs
```

Single tests:

```bash
go test ./internal/looper -run TestName -v
cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -run TestAgentRetry -v .
```

`make verify` and `make analyze` need `golangci-lint`, `govulncheck`, `gosec`, `goimports`, `rg`, and Docker on PATH. There is no `.golangci.*` config, so linter defaults apply.

Things that are easy to get wrong:

- `tests/e2e` is a **separate Go module**. `go test ./...` from the root never runs e2e; use `make e2e` or `cd tests/e2e`. Without `GRALPH_BINARY`, the suite builds gralph from source itself.
- `make build` runs lint, vuln, gosec, and unit tests **inside** `build/Dockerfile`, cross-compiles five OS/arch binaries into `.bin/<arch>/<os>/`, then runs e2e in a container (`tests/docker-compose.yaml`) against the exported linux/amd64 binary. CI (`.github/workflows/ci.yml`) only calls `build/build.sh`, so a lint or gosec failure fails CI.
- Changing any flag or its help text requires regenerating `docs/cli-help.txt`, or `docs-check` fails:
  `make local && ./.bin/local/gralph --help 2>&1 | sed '1s|^Usage of .*:$|Usage of gralph:|' > docs/cli-help.txt`
- `docs-check` validates local links in **every** `.md` file under the repo (except `.git`, `.bin`) — including this one. Links must resolve to files, not directories.
- `make verify` greps `cmd`, `internal`, and `.github/workflows` for legacy provider-specific identifiers (`defaultClaudeRunner`, `invokeClaude`, `CLAUDE_CODE_OAUTH_TOKEN`) and fails if any reappear.
- gosec runs in the gate; existing file/exec calls carry `// #nosec Gxxx -- reason` justifications. New ones need the same treatment.

## Architecture

Three layers, one direction of dependency: `cmd/main` → `internal/looper` → `internal/agent`.

- **`cmd/main`** — pflag parsing, resolves an optional `--agent-profile` into an `agent.AgentCommand` (explicit `--agent-exec` / `--agent-arg` / `--prompt-mode` override profile values only when the flag was actually `Changed`), builds `looper.Config`, and installs a SIGINT/SIGTERM `signal.NotifyContext`. Cancellation flows purely through that context.
- **`internal/agent`** — the process boundary. `AgentCommand` (executable + literal argv + `PromptMode`) validates via `Validate()`: `stdin` mode forbids `{prompt}`, `arg` mode requires exactly one whole-argument `{prompt}`. `CommandRunner.Run` execs directly (never a shell), captures stdout and stderr to separate temp files, and **classifies the outcome with sentinel errors**: `ErrInvalidConfiguration`, `ErrSetup`, `ErrProcessStart`, `ErrCanceled`, `ErrNonZeroExit`. The caller owns `CommandOutput` and must `Cleanup()` on every path. `profiles` in `profile.go` holds verified convenience configs (`codex`, `claude-code`); profiles must never include permission-bypass arguments.
- **`internal/looper`** — the loop state machine in `runLoop`. The error classification drives it: only `ErrNonZeroExit` is retryable; every other error is fatal and leaves the PRD untouched. After each invocation it re-reads the PRD — if the first open item changed or disappeared, the item is complete (regardless of exit code); if unchanged at `MaxAttempts`, the item is rewritten to `- [~]` via temp-file + rename. Gralph itself never marks items done — the agent edits the PRD. Agent output is printed (stdout then stderr; interleaving is lost) only for completed and final-failed attempts; intermediate retries discard it.

Details worth knowing before editing:

- The prompt sent to the agent is the prompt file plus an appended `## Runtime paths` block with the PRD and progress paths. The e2e fake agent parses that block, so its format is a tested contract.
- Prompt and PRD must be regular files; symlinks are rejected because the atomic rename would replace the link rather than its target.
- PRD parsing is line-prefix based (`- [ ]` at column 0). An optional `- Agent:` line below an item (before the next checkbox) is display-only metadata for the console header; it does not select the executable.
- Process-tree termination is build-tagged: `process_tree_unix.go` (darwin/linux) uses `Setpgid` + `kill(-pid, SIGKILL)` via `cmd.Cancel`; `process_tree_other.go` only kills the direct child. E2E signal tests are split the same way (`signal_unix_test.go` / `signal_other_test.go`).
- The test seam in `looper` is the `promptRunner` function type passed into `runLoop`; unit tests inject fakes there rather than exec'ing processes.
- E2E tests are strictly black-box (no internal imports). They compile `tests/e2e/testdata/fakeagent`, a scriptable stand-in agent driven by flags (`--record`, `--exit-code`, `--block`, `--descendant-ready-file`, ...), and pass it via `--agent-exec`.
- `internal/version` values are injected with `-X` ldflags by both the Makefile and `build/Dockerfile`; keep the two in sync.

## Docs

- Current architecture contracts are the versioned `docs/architecture/*_v01.md` files (start at `00_overview_v01.md`). The hyphenated siblings (`00-overview.md`, `03-system-architecture.md`, ...) describe the older Claude-only design and are stale.
- `docs/plans/2026-08-10-agent-agnostic-runtime-tasks.md` records the task-by-task migration to the agent-agnostic runtime.
- `CHANGELOG.md` is maintained per tagged version (SemVer; version metadata comes from git tags).
- Default branch for PRs is `develop`.
