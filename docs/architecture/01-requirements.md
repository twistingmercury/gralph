# Gralph — Requirements

[Back to Overview](00-overview.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Problem Statement](#problem-statement)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Success Criteria](#success-criteria)
- [Constraints](#constraints)
- [Assumptions](#assumptions)

## Problem Statement

`scripts/ralph.sh` is a working loop driver for Claude-assisted PRD execution. It is non-portable: it requires Bash, perl (for BSD-safe in-place editing), and a POSIX shell environment. It cannot be installed to `$PATH` as a single binary, and its behavior cannot be tested without a real Claude CLI present.

Developers who want to run Ralph loops on machines without Bash, in Docker containers with minimal images, or from Go-based tooling have no option today. The goal is a statically-compiled Go binary that delivers identical loop semantics and is testable without Claude.

## Goals

### Primary Goals

1. Reproduce `scripts/ralph.sh` loop semantics exactly: first-open-item detection, Claude invocation via stdin, completion detection, retry counting, and abandon mutation with `- [~]`.
2. Accept file paths as explicit CLI flags (`--prd`, `--prompt`, `--progress`, `--iterations`) rather than relying on fixed directory conventions.
3. Produce cross-platform binaries (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/arm64) from a single Docker build command.
4. Enable unit testing of all loop logic without a real Claude CLI present.

### Secondary Goals

1. Embed version, build date, and git commit in the binary for `--version` output.
2. Emit structured, grep-friendly log lines for each loop phase.
3. Provide a `make install` path to `$GOBIN` for local developer use.

## Non-Goals

- Parallel or concurrent item processing — the loop is strictly sequential by design.
- Replacing or wrapping Claude CLI flags beyond `--print --dangerously-skip-permissions`.
- Supporting multiple PRD files in a single invocation.
- Adding new loop features beyond parity with `ralph.sh`.
- A configuration file format (flags only).

## Success Criteria

| Criterion                           | Target                                                                       | Measurement Method                                          |
| ----------------------------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------------- |
| Loop behavior parity                | All ralph.sh semantics reproduced                                            | Unit tests for completion, retry, and abandon paths         |
| Non-zero Claude exit is non-fatal   | Loop continues after Claude exits 1                                          | `TestRunLoop_AbandonAtLimit` with error-returning stub      |
| Cross-platform binaries             | darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/arm64 produced | `./build/build.sh` exits 0; `.bin/` contains the expected binaries |
| Unit tests pass without Claude      | `go test ./internal/looper -v` passes in CI                                  | commandRunner stub replaces real exec                       |
| E2E tests pass against built binary | `go test ./tests/e2e/...` passes                                             | TestMain builds binary; tests invoke it                     |
| Required flags enforced             | Missing `--prd` or `--prompt` exits non-zero with named flag in stderr | `TestMissingPrompt`, `TestMissingPrd`                   |

## Constraints

### Technical Constraints

- Go 1.26 is the project's Go version.
- Standard library preferred for all file and process operations.
- No framework abstractions (no Cobra, no command pattern) — pflag for parsing only.
- Must compile with `CGO_ENABLED=0` for static cross-compilation.
- Must pass `golangci-lint`, `govulncheck`, and `gosec` in the Docker build.

### Business Constraints

- The tool operates outside Claude sessions — it is not invoked from within a Claude context.
- The loop must be interruptible by SIGINT/SIGTERM at any Claude invocation boundary (context cancellation propagates to `exec.CommandContext`).

### Organizational Constraints

- `scripts/ralph.sh` is the behavioral specification. Any divergence is a bug, not a feature.

## Assumptions

| Assumption                                                                               | Impact if Wrong                                                      | Validation Plan                                    |
| ---------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------- |
| `claude` CLI is on PATH at runtime                                                       | Loop fails immediately on first invocation                           | Document as a prerequisite in README               |
| PRD checklist items are uniquely identified by their full line text                      | Attempt counter resets incorrectly if two items share identical text | Review PRD format; document uniqueness requirement |
| PRD and progress files are on the same filesystem as the temp file used for atomic write | `os.Rename` across filesystems fails on Linux                        | Test with files in `/tmp` vs. project root         |
| Single-writer access to PRD.md                                                           | Concurrent mutations corrupt the file                                | Out of scope; document as single-runner assumption |

**Next:** [Architectural Decisions](02-architectural-decisions.md)
