# Gralph — Architectural Decisions

> **Version**: v02
> **Date**: 2026-09-25
> **Notes**: Skill renamed to gralph-docs-writer.

[Back to Overview](00_overview_v02.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Decision Record Format](#decision-record-format)
- [Decision Summary](#decision-summary)
- [Decisions](#decisions)

## Decision Record Format

Each architectural decision is recorded as an ADR with the following structure:

- **Title**: Short descriptive name for the decision
- **Status**: Accepted
- **Context**: The situation, forces at play, and why a decision is needed
- **Decision**: What was decided and the rationale
- **Consequences**: Both positive outcomes and trade-offs accepted

## Decision Summary

| ADR # | Title                                          | Status   | Date       |
| ----- | ---------------------------------------------- | -------- | ---------- |
| 001   | Claude Code only, no agent abstraction         | Accepted | 2026-09-25 |
| 002   | YAML task file + shared prompt, strict parsing | Accepted | 2026-09-25 |
| 003   | One fresh session per task, no retry           | Accepted | 2026-09-25 |
| 004   | Gralph owns task state, atomic writes          | Accepted | 2026-09-25 |
| 005   | Outcome from JSON result line, not exit code   | Accepted | 2026-09-25 |
| 006   | Failed task blocks run until manual reset      | Accepted | 2026-09-25 |
| 007   | Unix only, process group lifecycle management  | Accepted | 2026-09-25 |
| 008   | Docker-first CI: build, test, e2e in container | Accepted | 2026-09-25 |

## Decisions

### ADR-001: Claude Code only, no agent abstraction

**Status:** Accepted

**Context:**

Early versions of gralph (tags v0.4.0–v0.5.2) attempted to build an agent-agnostic runtime, supporting Claude API, OpenAI, and other providers through a profile abstraction. This added complexity and required managing multiple provider integrations. The project was built specifically to drive Claude Code workflows.

**Decision:**

Gralph is Claude Code only. No agent abstraction layer, no provider profiles, no `--agent-*` flags. The CLI invokes `claude --print --dangerously-skip-permissions` directly, coupling the tool to Claude Code as a deliberate design choice.

**Consequences:**

*Positive:*
- Simpler codebase: no provider interface, no profile configuration
- Direct coupling to Claude Code's actual behavior (stdin/stdout, permission model)
- Easier to reason about prompt delivery and result parsing
- No false promise of portability to other LLM backends

*Negative:*
- Cannot be reused for non-Claude workflows
- Tightly bound to Claude CLI; changes to claude --print could break compatibility
- Users wanting OpenAI or other LLM support must fork or build a separate tool

---

### ADR-002: YAML task file + shared prompt, strict parsing

**Status:** Accepted

**Context:**

Task definitions need to be:
1. Portable and versionable (YAML fits this well)
2. Human-readable without special tooling
3. Safe: a corrupted file should be rejected entirely, not partially loaded

Early designs considered PR Markdown checklists, JSON, and config-file overrides. YAML was chosen because the gralph-docs-writer skill already generates tasks.yaml + prompt.md pairs, and YAML parsing is a known quantity in Go.

**Decision:**

Tasks are defined in a single tasks.yaml file with fields: id (positive int16), name, prompt, state (optional), and error (written by gralph only). Parsing is strict: any invalid element (bad id, empty name/prompt, unknown state, non-core YAML tags, duplicate id/name) rejects the whole file at parse time. There is no migration from other formats; tasks.yaml must be valid on first parse. No `state` means `pending`.

**Consequences:**

*Positive:*
- Single source of truth: one file, parsed all-or-nothing
- Validation is simple and deterministic; no partial states
- Field rules in internal/tasks match skill generation rules, kept in sync manually
- YAML is human-readable and standard in the Go ecosystem

*Negative:*
- A single typo in any task (e.g., id 0, empty prompt) fails the entire file parse
- No backward compatibility or migration path if the task schema changes
- Comments and custom formatting are lost when gralph rewrites the file
- Manual sync required between skill field rules and internal/tasks validation

---

### ADR-003: One fresh session per task, no retry

**Status:** Accepted

**Context:**

Early versions (through v0.6.0) included an `--iterations` flag to retry failed tasks and an "abandon" state. This was complex, added test burden, and conflicted with the "one session per task" model. A person can always manually fix a task and reset it to pending; retries add little value and complicate the mental model.

**Decision:**

Each task runs in exactly one fresh `claude --print --dangerously-skip-permissions` session. There is no `--iterations` flag, no retry, no fallback logic. A failed task blocks the run. To resume, a person must inspect the failure, fix the cause (in the repo or the task prompt), then manually set the task's state to `pending` (or `completed`) before running again.

**Consequences:**

*Positive:*
- No test burden for retry logic, backoff, or fallback states
- Clear responsibility model: gralph runs, humans fix
- Easy to reason about: each task has one outcome
- Simpler state machine: pending, completed, failed (no "abandoned" or attempt counts)

*Negative:*
- Users must manually fix tasks; no automatic recovery
- If a failure is transient (e.g., network hiccup from Claude), user must reset and re-run
- No ability to skip a task that is known-bad; only completed or pending are runnable
- Long-running loops cannot auto-recover from Claude timeouts

---

### ADR-004: Gralph owns task state, atomic writes

**Status:** Accepted

**Context:**

Task state must persist between runs so resumed runs don't repeat completed work. The state must be written safely: if a write fails mid-operation, the file should not be corrupted or left in a half-updated state. The looper must control when and how state is written.

**Decision:**

Gralph owns the task state (pending, completed, failed, error). After each task run, the looper writes the entire task list to a temporary file, then renames it over the original (atomic on POSIX systems). This ensures a complete state write or no change; never a partially written file. Claude sessions never write files; they only produce output. The error field is written by gralph on failure, never by Claude.

**Consequences:**

*Positive:*
- Atomic writes guarantee consistency: either the old state or the new state, never partial
- Single source of truth: the tasks.yaml file reflects gralph's knowledge of state
- Error tracking: gralph records failure reasons (from JSON or exit code) for human review
- Safe to resume: re-running gralph always reads the current persisted state

*Negative:*
- Comments and custom formatting in tasks.yaml are lost on each rewrite
- Task state is ephemeral to the file; if tasks.yaml is edited by hand, those changes overwrite the file's state
- If a session exits zero but writes no result line (or a malformed one), the task is marked failed; the session's actual work may have been done

---

### ADR-005: Outcome from JSON result line, not exit code

**Status:** Accepted

**Context:**

A Claude session may exit zero but output an incomplete result, or exit non-zero after producing a valid result. Exit code alone is unreliable. The shared prompt should ask Claude to end its output with a JSON line like `{"state": "completed", "error": ""}` on success, or `{"state": "failed", "error": "reason"}` on failure. This gives Claude a way to signal outcome independent of exit code.

**Decision:**

Gralph reads the last non-blank line of Claude's output (skipping code-fence markers) and parses it as JSON with `state` and `error` fields. If that line is valid JSON with `state: "completed"` and exit code is zero, the task is completed. Any other outcome (JSON with `state: "failed"`, missing/invalid result line, non-zero exit) marks the task failed. The `error` field from JSON is used if present; otherwise, the exit error message is used.

**Consequences:**

*Positive:*
- Outcome is explicit and independent of exit code noise (warnings, debug output)
- Claude can signal failure even on zero exit (e.g., validation failed)
- Result parsing is deterministic: last JSON line wins
- Shared prompt controls the contract; gralph just reads what was promised

*Negative:*
- Shared prompt must document the result line format; mismatch causes all tasks to fail
- If Claude's output has no JSON line, no valid JSON, or JSON without the right structure, task fails (no fallback to exit code)
- Fence lines must be skipped; a result line wrapped in ``` will parse, but if it's the only line and misses code fence, parsing may fail
- No way to distinguish between "Claude did the work but forgot the result line" and "Claude crashed and produced garbage"

---

### ADR-006: Failed task blocks run until manual reset

**Status:** Accepted

**Context:**

If a task fails mid-loop, subsequent tasks should not run automatically. This enforces a checkpoint model: a person reviews what went wrong, fixes it (code, prompt, or task state), and resumes intentionally. Automatic fallthrough could hide failures or mask cascade problems.

**Decision:**

Before running, the looper checks if any task has `state: failed`. If so, gralph prints the task summary table with a "Needs review!" flag on failed rows and exits non-zero without running Claude. The person must manually change the failed task's state to `pending` (or `completed`) and re-run. Once a task fails during a run, it is saved with that state and all later tasks are skipped.

**Consequences:**

*Positive:*
- Failures are visible and explicit; loops cannot silently skip bad tasks
- Users get a clear signal: fix this and resume
- Prevents cascade failures (task 1 fails, task 2 tries to use task 1's output, also fails)
- Encourages debugging: the person sees the failure and must understand why

*Negative:*
- Requires manual intervention; no hands-off automation for multi-hour loops
- If a failure is transient, a second run is needed (no automatic retry)
- Long workflows may require many manual resets if failures are frequent
- No state to distinguish "I fixed this and want to resume" from "I want to force-reset this task"

---

### ADR-007: Unix only, process group lifecycle management

**Status:** Accepted

**Context:**

Managing child process lifecycle is OS-specific. On Unix, process groups allow killing an entire tree of processes (claude and any children) with one signal. On Windows, this requires different APIs. Since the project is for Claude Code (primarily macOS and Linux users), Unix-only was chosen, and Windows support was deliberately removed.

**Decision:**

Gralph runs only on Unix (Linux, macOS, BSDs). No Windows build target, no runtime `if runtime.GOOS == "windows"` fallbacks. When spawning claude, the process is configured with `Setpgid` so it runs in its own process group. On SIGINT or SIGTERM, the entire process group is killed using `kill(-pgid, signal)`, ensuring claude and any children it spawned are all terminated.

**Consequences:**

*Positive:*
- Simple, reliable process lifecycle: one signal kills the whole tree
- Matches user expectations: Ctrl-C stops everything
- No platform-specific code for Windows; simpler binary
- Test suite uses Unix process APIs and knows it can rely on them

*Negative:*
- Cannot run on Windows; future contributors must not add Windows support back

---

### ADR-008: Docker-first CI: build, test, e2e in container

**Status:** Accepted

**Context:**

Gralph depends on Go, Docker, and specific tool versions (golangci-lint, gosec, govulncheck). Keeping dev and CI environments in sync is difficult. Docker-first CI ensures build/lint/test/e2e all run in the same reproducible image. `build/build.sh` is the single source of truth for the full build.

**Decision:**

The Makefile's `local` target builds a native binary. The `build` target runs `build/build.sh`, which builds a Docker image, runs lint/gosec/govulncheck/unit tests inside it, exports cross-compiled binaries, and runs e2e tests in a container using docker-compose. CI (GitHub Actions) runs only `build/build.sh`. The Dockerfile is split into layers: one for building and testing, one for exporting binaries.

**Consequences:**

*Positive:*
- One source of truth: build/build.sh is the canonical build
- Reproducibility: CI and local builds use the same image
- No tool version drift: linters and scanners are pinned in the Dockerfile
- CI configuration is simple: just run build/build.sh
- e2e tests have a fake claude on PATH and no network access needed

*Negative:*
- Docker is required for the full build; `make local` is native but not tested by CI
- Build time is longer (Docker image pull, build, e2e container startup)
- Native local development requires Go toolchain installed; Docker is not optional for release builds
- Debugging failed CI runs requires running `docker build` locally
- E2e tests always run in a container, not natively; cache issues possible

---

**Next:** [System Architecture](03_system_architecture_v01.md)
