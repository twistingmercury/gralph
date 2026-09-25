# Gralph — Requirements

> **Version**: v02
> **Date**: 2026-09-25
> **Notes**: Skill renamed to gralph-docs-writer.

[Back to Overview](00_overview_v02.md) | [Back to Project README](../../README.md)

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
- Block on failures until manually addressed
- Resume interrupted runs without re-running completed work

## Goals

### Primary Goals

1. Run an ordered sequence of tasks, each with a fresh Claude session
2. Track task state (pending, completed, failed) across multiple runs
3. Block on the first failure; require manual intervention to resume
4. Support dry-run validation of task files without running Claude
5. Exit non-zero if any task fails, so automation scripts can detect failure
6. Be Claude Code only: no agent abstraction, no multi-provider support
7. Run only on Unix (Linux, macOS, BSDs)

### Secondary Goals

1. Be installable to GOBIN and usable from anywhere
2. Support Docker-based release builds and CI testing
3. Provide clear error messages when task files are invalid or tasks fail

## Non-Goals

- Automatic retry or fallback logic; users must fix failed tasks and reset state manually
- Multi-session context sharing (each task is independent)
- Agent abstraction layer; Claude Code is the only runtime
- Windows support
- Configuration files or environment variable override of task/prompt paths
- Rich UI; plain text tables and console output only

## Success Criteria

| Criterion                               | Target                                 | Measurement Method                              |
| --------------------------------------- | -------------------------------------- | ----------------------------------------------- |
| Task file validation                    | Whole file rejected on any invalid     | Unit tests and e2e tests verify rejection       |
| State persistence                       | Atomic writes, no partial state       | E2E tests verify file state after each run      |
| Failed task blocking                    | Gralph refuses to run with any failed | E2E test attempts run with failed task, expect exit 1 |
| Dry-run accuracy                        | Task summary table matches actual run | Dry-run e2e tests compare output format         |
| Session independence                    | Each task has only its prompt + task  | Golden test asserts combined-prompt format      |
| Exit code correctness                   | Zero only when all tasks complete     | E2E test matrix covers pass/fail/cancel cases   |
| Signal handling                         | SIGINT/SIGTERM kills claude group    | Process tree test verifies Setpgid and signal   |
| Unix-only behavior                      | No Windows build, no runtime fallback | Build pipeline has no Windows target            |

## Constraints

### Technical Constraints

- **Go 1.27.1 or later** — Project uses modern Go features (range over int, etc.)
- **Unix process API** — Depends on Setpgid and process groups; Windows not supported
- **Single-file YAML task input** — No migration from other formats; tasks.yaml must be valid on the first parse
- **No injectable runner** — Both test suites use a fake `claude` on PATH to drive the looper; runLoop execs inline
- **Prompt passed via stdin** — Claude is invoked as `claude --print --dangerously-skip-permissions` with prompt on stdin

### Business Constraints

- **Emerging maturity** — CLI contract has changed between minor versions (e.g., v0.5.x to v0.6.0)
- **No CHANGELOG** — Versions are SemVer git tags only; no maintained changelog document
- **Skill-driven task generation** — The gralph-docs-writer skill generates tasks.yaml + prompt.md; field rules must stay in sync with internal/tasks

### Organizational Constraints

- **Claude Code only** — An agent-agnostic runtime (tags v0.4.0–v0.5.2) was built, tried, and rolled back; this constraint is intentional and load-bearing
- **No retry** — The `--iterations` flag and retry/abandon logic were removed (last in v0.6.0) and must not be reintroduced
- **PRs target develop** — Main is stable; develop is the development branch
