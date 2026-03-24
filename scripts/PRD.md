# Product Requirements Document: Build `gralph` from `scripts/ralph.sh`

## Objective

Implement `gralph` as a Go CLI that reproduces the loop behavior in `scripts/ralph.sh`: pick the first open PRD item, run Claude once, detect completion, retry with limits, and abandon items at the configured cap.

## Problem Statement

The repository has a working shell script loop, but the Go implementation is incomplete. The CLI does not execute the loop, core looper functions are stubs, and current tests target an older command shape. This blocks using `gralph` as the portable, installable replacement for `ralph.sh`.

## Success Criteria

- `gralph` runs a full PRD loop from CLI flags without panics.
- Loop behavior matches `scripts/ralph.sh` semantics for completion, retries, and abandon.
- Non-zero Claude exits count as failed attempts and do not crash the loop.
- `go test ./...` passes.
- README usage matches the implemented CLI.

## Scope

In scope:

- CLI flags and validation
- Looper control flow and item state transitions
- Claude invocation wiring
- PRD mutation for abandon behavior
- Unit and integration-style loop tests
- README updates

Out of scope:

- New workflow features beyond script parity
- Replacing Claude CLI flags used by the script
- Multi-item parallel execution

## Constraints and Decisions

- Use `scripts/ralph.sh` as behavior source of truth.
- Keep one-cycle changes small and verifiable.
- Prefer standard library for file and process operations.
- Keep PRD mutation limited to first matching `- [ ]` when abandoning.
- Maintain cross-platform behavior for macOS and Linux.

## Implementation Plan

- [x] **Cycle 1 - Fix compile blockers in CLI entrypoint**: Make `cmd/main/main.go` build-clean and remove obvious typos.
  - Agent: `go-software-engineer`
  - Files: `cmd/main/main.go`
  - Steps:
    - Fix module import path and flag variable naming typos.
    - Ensure help/version flags parse and execute without dead code.
    - Remove placeholder output (`"I'm in danger!"`).
  - Verify: `go build ./cmd/main`
  - Done: `go build ./cmd/main` exits 0.

- [x] **Cycle 2 - Define final CLI contract and defaults**: Lock flag names and defaults to match project intent.
  - Agent: `go-software-engineer`
  - Files: `cmd/main/main.go`, `README.md`
  - Steps:
    - Implement flags: `--prompt`, `--prd`, `--progress`, `--iterations`, `--version`.
    - Make required flag behavior explicit in code and help text.
    - Ensure defaults are safe and documented.
  - Verify: `go run ./cmd/main --help`
  - Done: Help output lists all supported flags with accurate descriptions.

- [x] **Cycle 3 - Wire CLI to looper start path**: Call looper from `main` with validated inputs.
  - Agent: `go-software-engineer`
  - Files: `cmd/main/main.go`, `internal/looper/looper.go`
  - Steps:
    - Build a single start path from parsed flags to `looper.Start`.
    - Return non-zero exit on validation/runtime failures.
    - Keep `--version` as an immediate exit path.
  - Verify: `go run ./cmd/main --version`
  - Done: `--version` prints metadata and exits 0; invalid input exits non-zero.

- [x] **Cycle 4 - Implement looper input validation and progress file handling**: Complete `Start` contract.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper.go`
  - Steps:
    - Validate prompt and PRD existence with actionable errors.
    - Resolve/create `progress.txt` behavior from CLI-provided or derived path.
    - Return errors correctly (including missing return paths).
  - Verify: `go test ./internal/looper -run TestStart -v` (or add this test in Cycle 8)
  - Done: `Start` validates files and returns deterministic errors.

- [x] **Cycle 5 - Implement first open item detection**: Add parser for first `- [ ]` line.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper.go`
  - Steps:
    - Implement `getFirstOpenItem` against PRD content.
    - Preserve full line text for item identity checks.
    - Return empty result when no open items remain.
  - Verify: targeted unit test for open-item detection.
  - Done: function returns first open item or empty string with no false matches.

- [x] **Cycle 6 - Implement abandon-first-open-item mutation**: Change only first `- [ ]` to `- [~]`.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper.go`
  - Steps:
    - Implement deterministic first-match replacement.
    - Ensure only one checklist marker is changed per abandon action.
    - Write file updates safely.
  - Verify: targeted unit test for single-item replacement behavior.
  - Done: exactly one first open item is converted to `- [~]`.

- [x] **Cycle 7 - Implement Claude invocation wrapper**: Run one non-interactive Claude attempt per loop cycle.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper.go`
  - Steps:
    - Implement `invokeClaude` with `claude --print --dangerously-skip-permissions`.
    - Feed prompt text plus runtime PRD/progress paths.
    - Return command exit as error without crashing outer loop.
  - Verify: unit test with a stub command path or command runner abstraction.
  - Done: invocation path is testable and non-zero exits are surfaced as attempt failures.

- [x] **Cycle 8 - Implement loop control semantics**: Complete retry/completion/abandon logic in `exec`.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper.go`
  - Steps:
    - Track `current item` and reset attempt counter when item changes.
    - Detect completion when first open item changes or disappears.
    - Abandon when attempts hit configured limit; continue to next open item.
  - Verify: targeted tests for attempt reset, completion detection, and abandon threshold.
  - Done: loop semantics match `scripts/ralph.sh` behavior.

- [x] **Cycle 9 - Add deterministic logging**: Emit stable progress logs per phase.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper.go`
  - Steps:
    - Log start, invoke, check, completed, retry, abandoned, and done states.
    - Keep log messages short and grep-friendly.
    - Remove unused logging imports/variables.
  - Verify: run a short local loop with sample files and inspect output.
  - Done: each loop phase logs once per iteration with no noisy duplicates.

- [x] **Cycle 10 - Update tests for current CLI shape**: Replace stale e2e expectations.
  - Agent: `go-software-engineer`
  - Files: `tests/e2e/gralph-tests.go`
  - Steps:
    - Update tests that currently assume subcommands not present in current CLI.
    - Add coverage for flags: `--version`, missing required files, and successful startup path.
    - Keep tests black-box against built binary.
  - Verify: `go test ./tests/e2e/...`
  - Done: e2e tests reflect actual CLI behavior and pass reliably.

- [x] **Cycle 11 - Add looper unit test suite**: Cover parsing, mutation, and control flow.
  - Agent: `go-software-engineer`
  - Files: `internal/looper/looper_test.go` (new)
  - Steps:
    - Add table tests for open-item detection edge cases.
    - Add file mutation tests for abandon behavior.
    - Add control-flow tests with stubbed command execution.
  - Verify: `go test ./internal/looper -v`
  - Done: looper package has meaningful unit coverage for core behaviors.

- [x] **Cycle 12 - Docker-first build pipeline hardening**: Align Docker build outputs, quality gates, and CI-facing build behavior.
  - Agent: `devops-engineer`
  - Files: `build/Dockerfile`, `build/build.sh`, `Makefile` (if needed)
  - Steps:
    - Fix Docker cross-compile output paths so each OS/ARCH binary is emitted to the correct directory.
    - Ensure Docker builder runs required quality gates and tests for this project.
    - Ensure `build/build.sh` remains the primary Docker-first build entrypoint and exports binaries predictably.
    - Remove or adjust any build steps that are incorrect for this repo layout.
  - Verify: `./build/build.sh`
  - Done: Docker build completes, binaries are exported for expected targets, and e2e stage runs from the Docker-first flow.

- [x] **Cycle 13 - Final docs and end-to-end verification**: Ensure usage docs and full test pass.
  - Agent: `technical-writer` + `go-software-engineer`
  - Files: `README.md`, `docs/PROMPT.md` (adjust only if needed)
  - Steps:
    - Update README usage examples to exact implemented flags.
    - Document checklist format (`- [ ]`, `- [x]`, `- [~]`) and retry semantics.
    - Run full validation suite.
  - Verify: `go test ./... && go build ./...`
  - Done: docs match behavior; full build and tests pass.

## Risks and Mitigations

- Risk: Claude invocation is hard to test reliably.
  - Mitigation: isolate command execution behind small seam and stub in tests.
- Risk: PRD mutation corrupts file formatting.
  - Mitigation: test first-match replacement against multiline fixtures.
- Risk: CLI contract drifts from README.
  - Mitigation: require README update in final cycle and verify with `--help` output.

## Definition of Done

- CLI and looper behavior match `scripts/ralph.sh` semantics.
- Atomic cycle checklist is mostly checked through implementation.
- Unit and e2e tests pass under `go test ./...`.
- README and docs are accurate for first-time contributors.
