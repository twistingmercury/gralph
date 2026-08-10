# Code Review: 2026-08-10 — Full Codebase

**Review Date:** 2026-08-10
**Reviewers:** tactical code reviewer, solution architect, Go software architect
**Phase:** Provider-agnostic runtime assessment and full-codebase review

## Files Reviewed

### Source Files

- `cmd/main/main.go` — CLI flags, validation, signal handling, and loop startup
- `internal/looper/looper.go` — agent process execution, loop state, output handling, and PRD mutation
- `internal/version/version.go` — build metadata and version output
- `build/build.sh`, `build/Dockerfile`, `Makefile` — local and Docker build/test paths
- `tests/e2e/Dockerfile`, `tests/docker-compose.yaml` — containerized e2e execution
- `.github/workflows/*.yml` — CI and Claude-specific automation
- `README.md`, `docs/architecture/*.md`, `docs/gralph-concept.md` — public and architectural contracts
- `go.mod`, `go.sum`, `tests/e2e/go.mod`, `tests/e2e/go.sum`, `tests/e2e/tooling.go` — dependency definitions

### Test Files

- `internal/looper/looper_test.go`
- `tests/e2e/gralph_test.go`

## Validation Results

| Tool | Result |
| --- | --- |
| `go test ./...` | Pass for the root module |
| `go test ./...` in `tests/e2e` | Pass (6 black-box CLI tests) |
| `GOCACHE=/tmp/... go test -race ./...` | Pass for root and e2e modules; the default read-only cache caused an initial toolchain lookup failure |
| `go vet ./...` | Pass |
| `golangci-lint run` | Pass with writable temporary caches |
| `gosec -quiet -exclude-dir=tests ./...` | Pass |
| `gofmt -l` / `git diff --check` | Clean |
| `shellcheck build/build.sh` / `bash -n build/build.sh` | Pass |
| `go mod verify` | Pass for root and e2e modules |
| `govulncheck` | Not completed; sandboxed network/cache access prevented the vulnerability database lookup |
| Docker build/e2e pipeline | Not run; Docker daemon access was unavailable |

## Design Compliance

The current implementation still satisfies most of its original Claude-specific loop behavior, but it does **not** satisfy the newly confirmed requirement that users may select any compatible AI agent CLI (Claude, Codex, Grok, or another executable). The production path is hard-coded to Claude, and launch/configuration failures can destructively abandon PRD items while the process ultimately exits successfully. Those are release blockers for a provider-agnostic version.

### Behavioral Requirements Verified

- First top-level `- [ ]` item detection and sequential processing ✓
- Attempt counting and abandon-at-limit behavior for a started, unchanged agent invocation ✓
- Context checks around agent invocation and PRD reads ✓
- CLI validation rejects `--iterations < 1` ✓
- Progress file is created and passed to the prompt without a retained handle ✓
- Root unit tests and separate-module e2e tests pass ✓
- User-configurable agent executable and argument list ✗
- Fatal startup/configuration errors preserve the PRD and exit non-zero ✗
- SIGINT and SIGTERM both cancel the supervised agent process ✗
- Stable checklist item identity across text edits and duplicates ✗

### Design Doc Divergences (Post-Review)

The review did not change implementation or architecture documents. Existing documents materially diverge from both the current code and the new provider-agnostic requirement.

#### Naming Divergences (provider-specific names to replace)

| Old Name (in design docs) | New Name (recommended) | Reason |
| --- | --- | --- |
| `claudeRunner` / `defaultClaudeRunner` | `agentRunner` / `commandAgentRunner` | The execution boundary must not encode one provider |
| `invokeClaude` | `invokeAgent` | The loop should operate on any compatible CLI |
| `claudeOutputPath` | `agentOutputPath` or structured `InvocationResult` | Output ownership belongs to the configured agent invocation |
| `--dangerously-skip-permissions` core default | User-supplied provider arguments or an explicit opt-in profile | Permission-bypass flags are provider-specific and security-sensitive |

#### Structural Divergences (justified improvements over design doc)

| Divergence | Design Doc | Implementation / Requirement | Assessment |
| --- | --- | --- | --- |
| Runtime provider selection | Architecture requires `claude --print --dangerously-skip-permissions` | Users must be able to select Claude, Codex, Grok, or another CLI | Replace the Claude-specific contract with a generic executable, argument list, and prompt transport contract |
| Runner injection | ADR-002 describes a package-level mutable runner | `runLoop` now receives a function parameter | Implementation is better; ADR-002 must be superseded |
| Output contract | README and architecture promise `[gralph]` logs and forwarded stdout/stderr | Current code buffers combined output, discards intermediate attempts, and prints human-readable status blocks | Documentation and output semantics must be reconciled |
| Signal handling | Requirements explicitly promise SIGINT/SIGTERM | `main` registers only `os.Interrupt` | Implementation violates the documented lifecycle contract |
| E2E Docker execution | ADR-005 says Docker recompiles the binary | Docker e2e now consumes the exported binary via `GRALPH_BINARY` | Documentation is stale; implementation is preferable |
| Local artifact path | Deployment docs claim `.bin/$GOOS/$GOARCH/gralph` | `make local` writes `.bin/local/gralph` | Documentation is incorrect |

#### Documents Updated

| Document | Scope | Status |
| --- | --- | --- |
| `docs/code-reviews/2026-08-10-full-codebase.md` | Review findings and provider-agnostic recommendation | Added |
| Runtime README and architecture documents | Provider-neutral contract and current behavior | Pending approved implementation design |

## Findings

### HIGH Priority

| ID | Source | Finding | Resolution |
| -- | ------ | ------- | ---------- |
| H1 | all reviewers | **The only production runner is hard-coded to Claude.** `internal/looper/looper.go:20-30,80` always runs `claude --print --dangerously-skip-permissions`; `Start` has no provider configuration. Claude-specific behavior also permeates `README.md:7-9,44-47,79-82` and both `.github/workflows/claude*.yml`. With Claude unavailable, normal runs and the two automation workflows are nonfunctional. | Introduce a provider-neutral execution boundary and require/configure an executable plus argument vector. A recommended API is `AgentRunner.Run(ctx, Invocation) (Result, error)` backed by `exec.CommandContext(name, args...)`. Expose `--agent-exec` and repeatable `--agent-arg`; never accept a shell command string or use `sh -c`. Support explicit prompt transport (`stdin` by default, optionally a whole-argument `{prompt}` placeholder). Keep provider profiles optional and layered over the generic path. |
| H2 | all reviewers | **Startup and infrastructure failures can abandon every PRD item and still exit zero.** `invokeClaude` returns prompt-read, temp-file, command-start, and process-exit errors through the same channel (`internal/looper/looper.go:24-39,233-243`). `runLoop` retains the error, consumes attempts, rewrites the item to `[~]`, and continues (`:125,154-169`). A missing executable—or even a directory passed as `--prompt`, which passes `os.Stat`—can therefore mutate the entire PRD without any agent work. | Validate agent configuration and readable regular input files before creating progress state. Return typed outcomes that distinguish fatal configuration/setup/start/I/O errors from a process that actually started and exited non-zero. Only the latter may consume an attempt; fatal errors must preserve the PRD and exit non-zero. Decide separately whether any abandonment should produce an aggregate non-zero result. |
| H3 | Go + architecture (severity disputed) | **Checklist identity is the full mutable line text.** Completion is inferred from `itemAfter != currentItem` (`internal/looper/looper.go:113-118,131-151`). Editing an unchecked line can be reported as completion and reset attempts; adjacent duplicate lines can cause the next, never-invoked item to inherit attempts and be abandoned. One reviewer rated this medium because the template encourages unique titles; another rated it high because ordinary text edits still violate the core lifecycle invariant. It is listed high conservatively because the failure can mutate the wrong work item. | Give each cycle a stable unique identifier and compare explicit marker/state transitions, not display text. Validate duplicate IDs before execution. Before abandonment, use compare-and-swap semantics so gralph mutates only the exact item/version it attempted. Add edited-open-line and adjacent-duplicate regression tests. |
| H4 | Go + architecture (severity disputed) | **SIGTERM is not supervised despite an explicit requirement.** `cmd/main/main.go:28` registers only `os.Interrupt`, while `docs/architecture/01-requirements.md:67` requires SIGINT/SIGTERM. A container/service shutdown may terminate gralph without cancelling or waiting for a repository-mutating child agent. Reviewers split between medium for a foreground CLI and high for documented CI/container use; it is listed high because the repository explicitly supports those supervised environments. | Register `syscall.SIGTERM`, preserve cancellation through the runner, and define process-group/job-object behavior so descendants do not survive. Add black-box SIGINT and SIGTERM tests with a fake agent and verify the PRD remains unchanged after cancellation. |

### MEDIUM Priority

| ID | Source | Finding | Resolution |
| -- | ------ | ------- | ---------- |
| M1 | all reviewers | **Atomic abandonment changes filesystem semantics.** `os.CreateTemp` creates a mode-0600 replacement and `os.Rename` installs it over the PRD (`internal/looper/looper.go:317-335`), so a shared 0644 file becomes private. A symlink path is replaced rather than updating its target; ACLs and extended attributes may also be lost. | Resolve or reject symlinks explicitly, preserve the original permission bits on the temporary file, sync/close before rename, and document other metadata limitations. Add mode and symlink tests on supported platforms. |
| M2 | tactical + architectural | **The production process boundary has no end-to-end coverage.** `tests/e2e/gralph_test.go:111-169` tests help, version, and validation failures only. It does not exercise a successful loop, non-zero agent exit, missing executable, output handling, cleanup, or cancellation. | Build a deterministic fake agent fixture and test stdin and argument prompt modes, literal args containing spaces/metacharacters, success, retryable exit, fatal startup, output separation, SIGINT/SIGTERM, descendant cleanup, and byte-for-byte PRD preservation on fatal errors. Provider-profile contract tests should remain offline. |
| M3 | Go reviewer | **E2E timeouts can be mistaken for ordinary non-zero exits.** `tests/e2e/gralph_test.go:98-104` checks `*exec.ExitError` before `ctx.Err()`. A command killed at the deadline commonly returns `*exec.ExitError`, allowing a test that expects merely non-zero to pass when the CLI actually hung. | Check `ctx.Err()` before classifying `*exec.ExitError`, fail immediately on deadline/cancellation, and add a deliberately blocking fixture. |
| M4 | tactical + architectural | **Documentation describes a different system.** `README.md:61-75` promises obsolete `[gralph]` logs; ADR-002 and `docs/architecture/03-system-architecture.md:72` describe a removed global runner; output forwarding and progress-handle descriptions are stale; deployment docs give the wrong local build path; README still states v0.1.0 while the repository reports v0.4.0. | Refresh README, requirements, ADRs, system architecture, and deployment docs as one part of the provider migration. Supersede rather than silently rewrite decisions, identify a maintained behavioral specification now that `scripts/ralph.sh` was deleted, and avoid a hard-coded current version in README. |
| M5 | tactical reviewer | **A failing Docker e2e run skips cleanup.** `build/build.sh:30-33` runs under `set -e`; a failing `docker compose up` exits before `docker compose down`, leaving the fixed-name container/network behind. | Install an `EXIT` trap after resources may be created, or capture the test status, always run `compose down`, then return the original status. Add a failure-path shell test. |
| M6 | tactical reviewer | **The Docker quality gate rewrites code before validating it.** `build/Dockerfile:18-24` runs `goimports -w .`, so unformatted committed code can pass and binaries can be built from source different from the reviewed checkout. | Replace the mutating step with `goimports -l`/`-d` plus an assertion that no differences exist. Keep `-w` in a developer formatting target only. |
| M7 | tactical + architectural | **The Claude review workflow cannot perform its requested write.** `.github/workflows/claude-code-review.yml:22-26` grants `pull-requests: read`, while `:52-56` directs the action to run `gh pr comment`. | During provider migration, either grant narrowly scoped `pull-requests: write` or publish through a job summary/artifact. Do not carry this permission mismatch into the replacement workflow. |
| M8 | architectural reviewer | **The Docker e2e image is host-architecture sensitive.** `tests/e2e/Dockerfile:7-9` defaults to the linux/amd64 artifact, while `tests/docker-compose.yaml` neither pins `linux/amd64` nor selects the host architecture. | Pin the service platform or wire BuildKit `TARGETARCH` to the matching exported binary; exercise amd64 and arm64 in CI. |

### LOW Priority

| ID | Source | Finding | Resolution |
| -- | ------ | ------- | ---------- |
| L1 | Go reviewer | `maxAttempts >= 1` is enforced only by `cmd/main`; the exported `looper.Start` still accepts zero or negative values and immediately abandons an open item. | Validate invariants inside the new looper configuration API while retaining CLI validation for user-friendly errors. |
| L2 | all reviewers | `tests/e2e/tooling.go` blank-imports Testify packages that no test uses, retaining four unnecessary dependencies in the nested module. | Delete `tooling.go` and tidy `tests/e2e` after approval. |
| L3 | tactical reviewer | README's testing instructions show only root `go test ./...`, which cannot traverse the nested e2e module. | Document `make test` and `make e2e`, or add and recommend one aggregate verification target. |
| L4 | tactical reviewer | CI configures a Buildx cache, but `build/build.sh` uses plain `docker build --no-cache --pull`, so the cache is never consumed. | Use `docker buildx build` with matching cache options or remove the unused cache setup. |
| L5 | tactical reviewer | CI verifies only that one `gralph*` artifact exists, not the five paths required by the documented build matrix. | Assert each required OS/architecture artifact explicitly. |
| L6 | Go reviewer | Core loop output is coupled to global `os.Stdout`; a test swaps `os.Stdout`, preventing safe parallelization and risking leaked global state after a fatal test exit. | Inject an `io.Writer` through loop configuration and test against a buffer directly. |
| L7 | architectural reviewer | There is no `.dockerignore`, so the release builder receives `.git` and local artifacts through `COPY . .`. | Add scoped Docker ignore rules, retaining only the `.bin` input needed by the e2e Dockerfile. |

## Patterns to Document

1. **Provider-neutral command specification:** executable and argument slices remain separate; no shell parsing, interpolation, or implicit permission-bypass flags.
2. **Typed invocation outcomes:** configuration/start/I/O failures are fatal and non-mutating; only a started agent's process exit participates in retry policy.
3. **Stable checklist identity:** immutable IDs and explicit state transitions govern retries and abandonment; display text is never identity.
4. **Fake-agent contract testing:** deterministic local executables validate prompt transport, output, exit classification, and signal behavior without network access.
5. **Metadata-aware atomic replacement:** atomic content replacement must also define symlink, permission, ownership, ACL, and durability semantics.

## Notes for Future Phases

**Provider-agnostic runtime (release blocker):** Replace `Start`'s positional arguments with a validated configuration object containing paths, attempt policy, output writer, and an `Agent` command specification. A practical generic CLI is `--agent-exec <path-or-name>` plus repeatable `--agent-arg <arg>` and `--prompt-mode stdin|arg`. For argument mode, permit only a whole-argument `{prompt}` placeholder. Named profiles may supply convenient defaults for known CLIs, but the generic command path remains authoritative.

**Compatibility contract:** A supported agent must provide a noninteractive one-shot mode, accept the prompt through the configured transport, use a meaningful process exit status, and return control. Interactive-only tools require a dedicated adapter. Authentication may inherit the caller environment, but environment values and secrets must never be logged.

**Output contract:** Capture stdout and stderr separately or tee them through injected writers. Define retention and cleanup explicitly. Do not label arbitrary agent output as `Files Changed` unless the selected adapter guarantees that format.

**Prior review status:** All cycle-13 high findings and most medium/low findings were addressed. The old root dependency-retention issue reappears in the e2e module, and implementation changes were not propagated to README/architecture documentation. No code fixes were applied in this review; resolutions await user approval.
