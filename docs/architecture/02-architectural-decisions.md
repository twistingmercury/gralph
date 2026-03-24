# Gralph — Architectural Decisions

[Back to Overview](00-overview.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Decision Record Format](#decision-record-format)
- [Decision Summary](#decision-summary)
- [Decisions](#decisions)

## Decision Record Format

Each architectural decision is recorded as an ADR with the following structure:

- **Title**: Short descriptive name for the decision
- **Status**: Proposed | Accepted | Deprecated | Superseded by ADR-NNN
- **Context**: The situation, forces at play, and why a decision is needed
- **Decision**: What was decided and the rationale
- **Consequences**: Both positive outcomes and trade-offs accepted

## Decision Summary

| ADR #   | Title                                                  | Status   | Date       |
| ------- | ------------------------------------------------------ | -------- | ---------- |
| ADR-001 | pflag only — no Cobra                                  | Accepted | 2025-01-01 |
| ADR-002 | Package-level commandRunner variable as test seam      | Accepted | 2025-01-01 |
| ADR-003 | Atomic PRD mutation via temp file and rename           | Accepted | 2025-01-01 |
| ADR-004 | Docker-first release build with scratch export stage   | Accepted | 2025-01-01 |
| ADR-005 | Separate Go module for e2e tests                       | Accepted | 2025-01-01 |
| ADR-006 | Progress file opened in Start but not written by loop  | Accepted | 2025-01-01 |
| ADR-007 | internal/tooling.go blank imports for go.mod retention | Accepted | 2025-01-01 |

## Decisions

### ADR-001: pflag only — no Cobra

**Status:** Accepted

**Context:**

Gralph has a single execution mode: run the loop. It has five flags and no subcommands. A command framework adds indirection and dependency weight for no structural benefit at this scale. The team wanted to keep the dependency graph minimal while retaining POSIX-style flag parsing (short and long forms, `--help` auto-generation).

**Decision:**

Use `github.com/spf13/pflag` directly in `cmd/main/main.go`. Define all flags as package-level vars. Validation is a simple loop over missing-flag names, not a framework hook. Cobra is retained in `go.mod` only via the `internal/tooling.go` blank import.

**Consequences:**

_Positive:_

- Simple, readable entrypoint with no framework ceremony.
- Flag parsing and validation are directly visible in `main.go` with no callbacks.

_Negative:_

- Adding subcommands later (e.g., `gralph validate`, `gralph status`) would require manual routing, not a framework-managed dispatch table.
- Cobra is a go.mod dependency that contributes to binary size and vulnerability surface without being used in production code.

---

### ADR-002: Package-level commandRunner variable as test seam

**Status:** Accepted

**Context:**

`invokeClaude` calls `exec.CommandContext("claude", ...)`. This cannot be tested without a real Claude binary on PATH. The loop logic — retry counting, completion detection, abandon mutation — is valuable to test in isolation. A test seam is required.

**Decision:**

Declare `claudeRunner commandRunner` as a package-level variable initialized to `defaultClaudeRunner`. Tests swap it in `t.Run` blocks with `defer func() { claudeRunner = orig }()`. The `commandRunner` type is `func(ctx context.Context, stdin string) error`.

**Consequences:**

_Positive:_

- All loop control-flow tests run without a Claude binary.
- The seam is small and contained to one package; it does not leak into the public API.
- Test replacement is idiomatic Go for small packages.

_Negative:_

- Package-level mutable state is not safe for parallel test execution (`t.Parallel()`). Tests in `looper_test.go` cannot use `t.Parallel()` as long as `claudeRunner` is a single var.
- An interface-based injection approach (passing `commandRunner` as a parameter to `runLoop`) would eliminate the global state at the cost of a slightly more verbose call chain.

---

### ADR-003: Atomic PRD mutation via temp file and rename

**Status:** Accepted

**Context:**

`abandonFirstOpenItem` must rewrite PRD.md. A naïve `os.WriteFile` truncates the file before writing — a crash mid-write produces a corrupted or empty PRD. This is recoverable only from git, which is a poor user experience.

**Decision:**

Write the updated content to `os.CreateTemp` in the same directory as the PRD, then `os.Rename` over the original. On the same filesystem, `rename(2)` is atomic. The temp file is cleaned up on any error path before returning.

**Consequences:**

_Positive:_

- PRD.md is never partially written. Either the old file survives or the new file replaces it atomically.
- Crash-safe for all failure modes except a crash between `Rename` completion and the OS flushing the directory entry (negligible and unrecoverable regardless of strategy).

_Negative:_

- `os.Rename` is not atomic across filesystems. If `--prd` points to a file on a different filesystem from the temp directory (e.g., a network mount), the rename will fail. The constraint is documented in requirements.
- The temp file uses `prd-*.tmp` in the PRD's directory, which is visible to users watching the filesystem during a write.

---

### ADR-004: Docker-first release build with scratch export stage

**Status:** Accepted

**Context:**

Cross-compiling Go for three targets (darwin/amd64, darwin/arm64, linux/amd64) on a developer laptop requires matching toolchain versions. Running quality gates (golangci-lint, govulncheck, gosec) locally requires those tools to be installed and pinned. The team wanted reproducible builds that do not depend on local tool state.

**Decision:**

`build/Dockerfile` uses a builder stage (`ghcr.io/twistingmercury/golang-tooling:alpine`) that includes all required tools. It runs linters, scanners, and `go test ./...` before compiling. A `scratch` export stage extracts only the binaries. `build/build.sh` invokes `docker build --target export --output .bin`.

**Consequences:**

_Positive:_

- Quality gates are mandatory — a build cannot skip them by omission.
- Any machine with Docker produces identical binaries from a clean state.
- Binary artifacts are extracted cleanly without a running container.

_Negative:_

- The Docker build is slow compared to `go build` locally — not suitable for rapid iteration. The Makefile `local` target exists for this reason.
- `go mod tidy` runs in the Dockerfile before tests, which can silently alter `go.sum` during the Docker build and cause a mismatch with the committed file. This should be `go mod verify` instead.
- The e2e test stage runs via `docker compose` after the binary build, creating a two-step Docker invocation in `build/build.sh`. The e2e container rebuilds the binary from source rather than using the already-exported binary, wasting build time.

---

### ADR-005: Separate Go module for e2e tests

**Status:** Accepted

**Context:**

E2e tests invoke the compiled binary via `os/exec`. They have no import dependency on gralph's internal packages. Keeping them in a separate module (`github.com/twistingmercury/gralph/tests/e2e`) prevents accidental internal coupling, keeps `go test ./...` from pulling e2e dependencies into the main module's test graph, and allows the e2e module to have its own toolchain and dependency versions.

**Decision:**

`tests/e2e/` is a standalone Go module. `TestMain` builds the gralph binary with `go build` against the project root before running any test, resolving the path via `os.Getwd()` + `../..`. The e2e Dockerfile installs both modules independently.

**Consequences:**

_Positive:_

- No risk of e2e test imports leaking internal packages into the main module's dependency surface.
- E2e tests are fully black-box; they cannot accidentally call internal functions.

_Negative:_

- `go test ./...` from the project root does not run e2e tests. Developers must know to run them separately or via `build/build.sh`.
- The `TestMain` path calculation (`wd + ../..`) is fragile — it relies on `os.Getwd()` returning the package directory, which is true for `go test` but breaks if the test binary is run directly from a different directory.

---

### ADR-006: Progress file opened in Start but not written by loop

**Status:** Accepted

**Context:**

`Start` opens `progressFile` for append at startup to validate the path and ensure the file exists (creating it if needed). The loop body (`runLoop`) receives the path string but does not write to the file. No progress entries are written during the loop.

**Decision:**

The current behavior is intentional for the v1 milestone. The progress file is created (or validated) at startup to give Claude a file it can append to during its own session. The gralph process does not own the progress log — Claude's instructions tell it to write there. Gralph's role is to ensure the file exists and pass its path in the prompt.

**Consequences:**

_Positive:_

- Separates concerns: gralph manages loop control; Claude manages progress narrative.
- The file handle in `Start` is opened, deferred-closed, but not written — this is correct if the intent is only to create the file and validate the path.

_Negative:_

- The open file handle (`file`) in `Start` is never written to and is closed by `defer`. This is a minor resource oddity — opening and immediately closing a file handle for side-effect (file creation) is unconventional. `os.OpenFile` could be replaced with a dedicated "ensure file exists" helper that opens and closes immediately before calling `runLoop`.
- The intent is not obvious from the code. A comment or helper name would clarify that the open is for creation/validation, not for write use by the loop.
- If the intent ever changes to have gralph write progress entries, the file handle must be plumbed through to `runLoop`, which currently receives only the path string.

---

### ADR-007: internal/tooling.go blank imports for go.mod retention

**Status:** Accepted

**Context:**

`go.mod` includes Cobra, Viper, and testify as direct dependencies. Cobra is not used in production code; it was a placeholder from project setup. Viper is not used. testify is used in tests. Without a reference in non-test code, `go mod tidy` would remove Cobra and Viper from `go.mod`. The team wants them retained (for future use or to preserve a known-good dependency baseline) without adding `// indirect` noise.

**Decision:**

`internal/tooling.go` blank-imports Cobra, Viper, and testify, keeping them as direct dependencies in `go.mod`. The file contains a comment noting it should be removed later.

**Consequences:**

_Positive:_

- Prevents `go mod tidy` from removing dependencies that may be used in future cycles.
- Makes the intent explicit in one file rather than scattered `// indirect` markers.

_Negative:_

- Cobra and Viper add transitive dependencies (mousetrap, afero, fsnotify, mapstructure, etc.) that increase binary size and vulnerability surface for no runtime benefit.
- `gosec` and `govulncheck` scan these transitive deps, potentially flagging issues in code that is never executed.
- The pattern is unusual enough that future contributors may not understand why it exists. The "remove later" comment should be paired with a specific cycle or condition.

**Next:** [System Architecture](03-system-architecture.md)
