# Gralph — Requirements

> **Version**: v10
> **Date**: 2026-10-01
> **Notes**: Added run logging on request (`--log-dir`, ADR-016): problem statement, secondary goal, non-goals, and success criterion.

[Back to Overview](00_overview.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Problem Statement](#problem-statement)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Success Criteria](#success-criteria)
- [Constraints](#constraints)

## Problem Statement

Complex software tasks often require a sequence of steps that depend on prior work. When orchestrating AI-assisted workflows, executing each step in a fresh session (with no context from prior steps) ensures determinism but requires careful prompt engineering and state tracking.

Claude Code users need a way to:
- Define a sequence of related tasks
- Run each in a fresh Claude session with a shared prompt
- Track which tasks completed, which failed
- Confirm each task's result with checks that do not depend on the session's own report
- Limit what an unattended session can read, write, and reach on the network
- Block on failures until manually addressed
- Resume interrupted runs without re-running completed work
- Look back at a run after it ends: what each task did, and what ran when

## Goals

### Primary Goals

1. Run an ordered sequence of tasks, each with a fresh Claude session
2. Track task state (pending, completed, failed) across multiple runs
3. Block on the first failure; require manual intervention to resume
4. Support dry-run validation of task files without running Claude
5. Exit non-zero if any task fails, so automation scripts can detect failure
6. Be Claude Code only: no agent abstraction, no multi-provider support
7. Run only on Unix (Linux, macOS, BSDs)
8. Check a completed task independently: run the task's `gates` commands after the session and mark the task completed only when every one exits zero
9. Run sessions in Claude Code's sandbox, set up by the user's `--sandbox-settings` file; run without one only when the user asks by name with `--skip-permissions`. There is no default
10. Commit a completed task's work itself, on request (`--commit`), only after its gates pass, one commit per task

### Secondary Goals

1. Be installable to GOBIN and usable from anywhere
2. Show a run live in a full-screen view by default in a terminal, while plain mode (`--no-tui` or no terminal) stays unchanged for scripts and CI
3. Support Docker-based release builds and CI testing
4. Provide clear error messages when task files are invalid or tasks fail
5. Keep a record of a full-screen run on request (`--log-dir`): a JSON-lines ledger of the run plus each task's activity, written by gralph itself

## Non-Goals

- Automatic retry or fallback logic; users must fix failed tasks and reset state manually
- Multi-session context sharing (each task is independent)
- Agent abstraction layer; Claude Code is the only runtime
- Windows support
- Configuration files or environment variable override of task/prompt paths
- A TUI for `--dry-run`; it always prints plain text
- Writing the TUI's `in progress` state to tasks.yaml
- Sending a task's gates to Claude; they are gralph's check, and the stdin prompt does not include them
- File-level gates shared by all tasks, a file-level timeout key, running a gate with no time limit, or sandboxing what a gate command does
- Gralph running sessions in a container it manages, or detecting the toolchain to guess sandbox paths; the settings file is the user's to write
- Falling back to an unsandboxed run when the sandbox cannot start
- Re-running only the gates of a task; a task reset to `pending` runs its session again
- Pushing, writing commit bodies, or cleaning up a failed task's work tree
- Requiring a git repository
- Logging by default, logging in plain mode (a redirect keeps its output), keeping the raw stream-json, or rotating and deleting old logs
- Having the session write or know about the logs

## Success Criteria

| Criterion                               | Target                                 | Measurement Method                              |
| --------------------------------------- | -------------------------------------- | ----------------------------------------------- |
| Task file validation                    | Whole file rejected on any invalid     | Unit tests and e2e tests verify rejection       |
| State persistence                       | Atomic writes, no partial state       | E2E tests verify file state after each run      |
| Failed task blocking                    | Gralph refuses to run with any failed | E2E test attempts run with failed task, expect exit 1 |
| Dry-run accuracy                        | Task summary table matches actual run | Dry-run e2e tests compare output format         |
| Gate enforcement                        | Completed only when every gate exits zero; gates skipped when the session failed | Unit and e2e tests run real shell commands as passing and failing gates |
| Session permissions                     | Sandboxed argv with the three forced keys, or the bypass flag; neither or both flags exits 1 | Unit tests on the merged settings; e2e tests assert the recorded argv and the flag errors |
| Session independence                    | Each task has only its prompt + task  | Golden test asserts combined-prompt format      |
| Exit code correctness                   | Zero only when all tasks complete     | E2E test matrix covers pass/fail/cancel cases   |
| Signal handling                         | SIGINT/SIGTERM kills claude group    | Process tree test verifies Setpgid and signal   |
| Commit on request                       | One commit per completed task, named after it; none for a failed task; a dirty tree refuses to start | Unit and e2e tests run real git in temporary repositories |
| Run logging on request                  | With `--log-dir`, one folder per run holding the ledger and a detail file per task; nothing written without it; plain mode with the flag exits 1 | Unit tests feed events to `internal/runlog` and assert the files; e2e tests pin the plain-mode refusal and that `--dry-run` ignores the flag |
| Unix-only behavior                      | No Windows build, no runtime fallback | Build pipeline has no Windows target            |

## Constraints

### Technical Constraints

- **Go 1.27.1 or later** — Project uses modern Go features (range over int, etc.)
- **Unix process API** — Depends on Setpgid and process groups; Windows not supported
- **Single-file YAML task input** — No migration from other formats; tasks.yaml must be valid on the first parse
- **No injectable runner** — Both test suites use a fake `claude` on PATH to drive the looper; runLoop execs inline
- **Prompt passed via stdin** — Claude is invoked as `claude --print` plus the session flags, with the prompt on stdin: `--permission-mode acceptEdits --settings <merged JSON>` for `--sandbox-settings`, or `--dangerously-skip-permissions` for `--skip-permissions`. The TUI path adds `--output-format stream-json --verbose` right after `--print`; plain mode's output stays as it is
- **Claude Code's sandbox** — Sandboxed runs need what it needs: bubblewrap and socat on Linux, Seatbelt on macOS. On the BSDs only `--skip-permissions` works
- **Bubble Tea v2 only** — `charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`, imported only by `internal/tui`; `cmd/main` imports `github.com/charmbracelet/x/term` for terminal detection only; never the v1 `github.com/charmbracelet/*` modules

### Business Constraints

- **Emerging maturity** — CLI contract has changed between minor versions (e.g., v0.5.x to v0.6.0)
- **No CHANGELOG** — Versions are SemVer git tags only; no maintained changelog document
- **Skill-driven task generation** — The gralph-docs-writer skill generates tasks.yaml + prompt.md; field rules must stay in sync with internal/tasks

### Organizational Constraints

- **Claude Code only** — An agent-agnostic runtime (tags v0.4.0–v0.5.2) was built, tried, and rolled back; this constraint is intentional and load-bearing
- **No retry** — The `--iterations` flag and retry/abandon logic were removed (last in v0.6.0) and must not be reintroduced
- **PRs target develop** — Main is stable; develop is the development branch
