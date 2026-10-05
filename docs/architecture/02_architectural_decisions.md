# Gralph — Architectural Decisions

> **Version**: v21
> **Date**: 2026-10-05
> **Notes**: ADR-011, ADR-016, and ADR-018 consequences now say which full-screen behaviour the pty e2e tests pin (wizard happy path, `--log-dir` record of a stopped run) instead of saying the suite has no terminal.

[Back to Overview](00_overview.md) | [Back to Project README](../../README.md)

## Table of Contents

- [Decision Record Format](#decision-record-format)
- [Decision Summary](#decision-summary)
- [Decisions](#decisions)

## Decision Record Format

Each architectural decision is recorded as an ADR with the following structure:

- **Title**: Short descriptive name for the decision
- **Status**: Proposed (designed, not built) or Accepted
- **Context**: The situation, forces at play, and why a decision is needed
- **Decision**: What was decided and the rationale
- **Consequences**: Both positive outcomes and trade-offs accepted

## Decision Summary

| ADR     | Title                                                         | Status   | Date       |
| ------- | ------------------------------------------------------------- | -------- | ---------- |
| ADR-001 | Claude Code only, no agent abstraction                        | Accepted | 2026-09-25 |
| ADR-002 | YAML task file + shared prompt, strict parsing                | Accepted | 2026-09-25 |
| ADR-003 | One fresh session per task, no retry                          | Accepted | 2026-09-25 |
| ADR-004 | Gralph owns task state, atomic writes                         | Accepted | 2026-09-25 |
| ADR-005 | Outcome from JSON result line, not exit code                  | Accepted | 2026-09-25 |
| ADR-006 | Failed task blocks run until manual reset                     | Accepted | 2026-09-25 |
| ADR-007 | Unix only, process group lifecycle management                 | Accepted | 2026-09-25 |
| ADR-008 | Docker-first CI: build, test, e2e in container                | Accepted | 2026-09-25 |
| ADR-009 | Embed the skill, install with --install-skill                 | Accepted | 2026-09-25 |
| ADR-010 | Refuse to run with a stale installed skill                    | Accepted | 2026-09-25 |
| ADR-011 | Full-screen TUI by default, plain mode intact                 | Accepted | 2026-09-25 |
| ADR-012 | Bubble Tea v2 for the TUI, confined to its use                | Accepted | 2026-09-25 |
| ADR-013 | Gralph runs a task's gates after a completed session          | Accepted | 2026-09-30 |
| ADR-014 | Sessions run in Claude Code's sandbox; bypass only on request | Accepted | 2026-09-30 |
| ADR-015 | Gralph commits a completed task on request                    | Accepted | 2026-09-30 |
| ADR-016 | Gralph logs a run on request                                  | Accepted | 2026-10-01 |
| ADR-017 | Release archives, built by a workflow started by hand         | Accepted | 2026-10-02 |
| ADR-018 | A run folder and a setup wizard                               | Accepted | 2026-10-02 |
| ADR-019 | One gate list per task file                                   | Accepted | 2026-10-02 |

## Decisions

### ADR-001: Claude Code only, no agent abstraction

**Status:** Accepted

**Context:**

Early versions of gralph (tags v0.4.0–v0.5.2) attempted to build an agent-agnostic runtime, supporting Claude API, OpenAI, and other providers through a profile abstraction. This added complexity and required managing multiple provider integrations. The project was built specifically to drive Claude Code workflows.

**Decision:**

Gralph is Claude Code only. No agent abstraction layer, no provider profiles, no `--agent-*` flags. The CLI invokes `claude --print --dangerously-skip-permissions` directly, coupling the tool to Claude Code as a deliberate design choice.

_Amended by ADR-014:_ Sessions run with `--permission-mode acceptEdits --settings <merged JSON>` (sandboxed) or `--dangerously-skip-permissions` (not sandboxed), chosen by `--sandbox-settings` or `--skip-permissions`.

**Consequences:**

_Positive:_

- Simpler codebase: no provider interface, no profile configuration
- Direct coupling to Claude Code's actual behavior (stdin/stdout, permission model)
- Easier to reason about prompt delivery and result parsing
- No false promise of portability to other LLM backends

_Negative:_

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

_Positive:_

- Single source of truth: one file, parsed all-or-nothing
- Validation is simple and deterministic; no partial states
- Field rules in internal/tasks match skill generation rules, kept in sync manually
- YAML is human-readable and standard in the Go ecosystem

_Negative:_

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

_Amended by ADR-014:_ The session flags are chosen by `--sandbox-settings` (sandboxed) or `--skip-permissions` (no sandbox).

**Consequences:**

_Positive:_

- No test burden for retry logic, backoff, or fallback states
- Clear responsibility model: gralph runs, humans fix
- Easy to reason about: each task has one outcome
- Simpler state machine: pending, completed, failed (no "abandoned" or attempt counts)

_Negative:_

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

_Positive:_

- Atomic writes guarantee consistency: either the old state or the new state, never partial
- Single source of truth: the tasks.yaml file reflects gralph's knowledge of state
- Error tracking: gralph records failure reasons (from JSON or exit code) for human review
- Safe to resume: re-running gralph always reads the current persisted state

_Negative:_

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

_Positive:_

- Outcome is explicit and independent of exit code noise (warnings, debug output)
- Claude can signal failure even on zero exit (e.g., validation failed)
- Result parsing is deterministic: last JSON line wins
- Shared prompt controls the contract; gralph just reads what was promised

_Negative:_

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

_Positive:_

- Failures are visible and explicit; loops cannot silently skip bad tasks
- Users get a clear signal: fix this and resume
- Prevents cascade failures (task 1 fails, task 2 tries to use task 1's output, also fails)
- Encourages debugging: the person sees the failure and must understand why

_Negative:_

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

_Positive:_

- Simple, reliable process lifecycle: one signal kills the whole tree
- Matches user expectations: Ctrl-C stops everything
- No platform-specific code for Windows; simpler binary
- Test suite uses Unix process APIs and knows it can rely on them

_Negative:_

- Cannot run on Windows; future contributors must not add Windows support back

**Amendment (2026-10-02):**

A `windows/amd64` binary can now be built, on the owner's request, but it is not released: its line in `build/Dockerfile` is commented out, and people who want it uncomment that line and build it themselves. It has never been run or tested, and nothing above changes on Unix. Off Unix, `process_tree_other.go` (`//go:build !unix`) replaces the process-group setup with no-ops, so cancelling kills only the direct child (its descendants keep running) and git gets no SIGTERM-first stop, which can leave `index.lock` behind. Sandboxed runs are unavailable there and gates still need `sh`. The split is by build constraint only; there are still no `runtime.GOOS` branches, and no Windows-specific features or tests.

---

### ADR-008: Docker-first CI: build, test, e2e in container

**Status:** Accepted

**Context:**

Gralph depends on Go, Docker, and specific tool versions (golangci-lint, gosec, govulncheck). Keeping dev and CI environments in sync is difficult. Docker-first CI ensures build/lint/test/e2e all run in the same reproducible image. `build/build.sh` is the single source of truth for the full build.

**Decision:**

The Makefile's `local` target builds a native binary. The `build` target runs `build/build.sh`, which builds a Docker image, runs lint/gosec/govulncheck/unit tests inside it, exports cross-compiled binaries, and runs e2e tests in a container using docker-compose. CI (GitHub Actions) runs only `build/build.sh`. The Dockerfile is split into layers: one for building and testing, one for exporting binaries.

**Consequences:**

_Positive:_

- One source of truth: build/build.sh is the canonical build
- Reproducibility: CI and local builds use the same image
- No tool version drift: linters and scanners are pinned in the Dockerfile
- CI configuration is simple: just run build/build.sh
- e2e tests have a fake claude on PATH and no network access needed

_Negative:_

- Docker is required for the full build; `make local` is native but not tested by CI
- Build time is longer (Docker image pull, build, e2e container startup)
- Native local development requires Go toolchain installed; Docker is not optional for release builds
- Debugging failed CI runs requires running `docker build` locally
- E2e tests always run in a container, not natively; cache issues possible

---

### ADR-009: Embed the skill, install with --install-skill

**Status:** Accepted

**Context:**

The `gralph-docs-writer` skill generates task files that must satisfy the parser in `internal/tasks`. Installing it with `scripts/install_skill.sh` requires a checkout of the repository, and nothing ties the installed copy to the gralph binary in use, so the skill and the parser can drift apart.

**Decision:**

`src/skills/embed.go` embeds the whole `src/skills/gralph-docs-writer` folder (SKILL.md and templates/) in the binary with `//go:embed`. The `--install-skill` flag runs before the `--prompt`/`--tasks` checks: `internal/skillinstall` resolves the home directory with `os.UserHomeDir`, removes `~/.claude/skills/gralph-docs-writer`, writes every embedded file under it preserving the layout, prints the path, and exits 0 (1 on error). There is no override flag or env var. `scripts/install_skill.sh` stays for development, installing the working-tree copy.

**Consequences:**

_Positive:_

- The installed skill always matches the binary's version and parser rules
- Installing needs only the binary, not a repository checkout
- Reinstalling is clean: stale files from an older skill are removed

_Negative:_

- Any local edits to the installed skill folder are lost on reinstall
- Two install paths exist (flag for users, script for development)

- Skill changes ship only with a new binary; a stale install is not detected during normal runs

_Amended by ADR-010:_ A stale install is now detected: a run or dry run exits 1 and tells the user to run `gralph --install-skill`.

---

### ADR-010: Refuse to run with a stale installed skill

**Status:** Accepted

**Context:**

ADR-009 ties the embedded skill to the binary, but nothing checked the installed copy: the installed `gralph-docs-writer` skill went stale more than once after gralph was upgraded or the skill changed. The skill's field rules must match the parser in `internal/tasks`, so a stale skill generates task files the current gralph may reject or read differently.

**Decision:**

`--install-skill` writes a `VERSION` file into `~/.claude/skills/gralph-docs-writer` holding two lines: the gralph version and a SHA-256 content hash of the embedded skill (`sha256:<hex>`, computed over each file's relative path and bytes in `fs.WalkDir` order). Normal runs and `--dry-run` call `skillinstall.Check` after the flag checks and refuse to start (exit 1) when the skill folder is missing or its `VERSION` hash differs from the binary's, telling the user to run `gralph --install-skill`. The stored version is informational only, used in the error message.

A content hash was chosen over the version tag or the git commit:

- A version tag misses skill changes made between releases, so development builds would accept a stale skill.
- A commit changes on unrelated edits, forcing reinstalls when the skill did not change, and its length differs between `make local` and `make build`, so the same source would stamp different values.
- The content hash changes exactly when the embedded skill changes, independent of how the binary was built.

**Consequences:**

_Positive:_

- A stale or missing skill is caught before any task runs, with a one-line fix in the error
- Reinstalls are required only when the skill content actually changes
- Local and release builds of the same source agree on the hash

_Negative:_

- Every run and dry-run needs the skill installed, even when the user never generates task files with it
- Local edits to the installed skill make every run fail until `--install-skill` overwrites them
- Tests that run gralph need a `HOME` with the skill installed (the e2e suite's `skillHome`)
- `scripts/install_skill.sh` writes no `VERSION`, so a development install fails the check until `--install-skill` is run

---

### ADR-011: Full-screen TUI by default, plain mode intact

**Status:** Accepted

**Context:**

In plain mode a run shows the combined prompt and then nothing from Claude until the session ends, because `claude --print` writes its final message only when it exits. Tasks can run for many minutes, so the user cannot tell what the session is doing or how far the run has got. Plain mode is also what CI, pipes, and the e2e suite rely on, byte for byte, so it must not change.

**Decision:**

When stdin and stdout are both terminals, gralph opens a full-screen view (`internal/tui`) with the current task's prompt, the task list and statuses, the current session's live activity, and a key legend. Plain mode is chosen by `--no-tui`, by `--dry-run`, or automatically when either stream is not a terminal, and keeps today's argv, output, and exit codes. In the TUI, a missing `--prompt` or `--tasks` is asked for on a setup screen instead of being an error.

_Amended by ADR-018:_ the setup screen is replaced by the setup wizard, which asks for a run folder instead of typed paths, and also for a missing permission choice and an undecided gate list, then shows a review screen. It still opens only in the TUI; plain mode is unchanged.

There is one loop. `looper.Run` → `runLoop` takes a `report func(Event)` hook:

- `report == nil` (plain): `runTaskPlain` runs `claude --print --dangerously-skip-permissions`, echoes the combined prompt, tees claude's stdout, and inherits stderr, exactly as before.
- `report != nil` (TUI): `runTaskStream` runs `claude --print --output-format stream-json --verbose --dangerously-skip-permissions`, writes nothing to gralph's stdout or stderr, and reports `TaskStarted`, `Activity` (assistant text and tool calls, and claude's stderr lines), and `TaskFinished` events, then a final `RunDone`. The outcome comes from the `result` event's text with the same rules as plain mode.

_Amended by ADR-014:_ The session flags are `--permission-mode acceptEdits --settings <merged JSON>` (sandboxed) or `--dangerously-skip-permissions` (not sandboxed), chosen by `--sandbox-settings` or `--skip-permissions`.

Both paths send the same combined prompt on stdin, and the loop rules (skip completed, save after every task, stop on first failure, cancel leaves the task untouched) live once in `runLoop`. Stream-json is used only on the TUI path. There are no runner, storage, or writer interfaces: `runLoop` still execs claude inline, and tests still drive a fake `claude` on `PATH`. `in progress` is display only and never written to `tasks.yaml`.

**Consequences:**

_Positive:_

- A run is easy to follow live: what Claude is doing, which task is running, which are done
- Plain mode, its tests, and every script or CI job that uses gralph are unchanged
- One copy of the loop rules; the two task paths differ only in how they talk to claude
- The plain-mode e2e tests need no change: with no terminal they run plain mode; the full-screen view has its own pty tests

_Negative:_

- Two ways to run claude, each with its own code and tests
- The TUI path depends on the stream-json event format, which Claude Code may change; unknown events and undecodable lines are ignored, but a changed `result` event would fail every task
- The TUI cannot be exercised by the e2e suite; it is tested through its Bubble Tea models and a manual run
- In the TUI the terminal is raw, so Ctrl-C is a key that opens a confirm; only an outside SIGINT/SIGTERM stops the run at once

---

### ADR-012: Bubble Tea v2 for the TUI, confined to its use

**Status:** Accepted

**Context:**

ADR-011 needs a full-screen terminal UI: an alt screen, resizable panes, scrolling, text input for the setup screen, and key handling alongside a loop that runs in a goroutine. Writing that on raw terminal escape codes would be a lot of code to own. Bubble Tea exists in a v1 (`github.com/charmbracelet/*`) and a v2 (`charm.land/*`) line with different APIs.

**Decision:**

The TUI uses Bubble Tea v2 only: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2` (viewport, textinput), and `charm.land/lipgloss/v2`. The v1 `github.com/charmbracelet/bubbletea`, `bubbles`, and `lipgloss` modules are never imported. These modules are imported only by `internal/tui`; `cmd/main`, `internal/looper`, and `internal/tasks` never import them, so dependencies point one way: `cmd/main` → `internal/tui` → `internal/looper` → `internal/tasks`. The looper reaches the TUI only through the `report` hook, which `tui.Run` forwards to the program with `Send`. `cmd/main` uses `github.com/charmbracelet/x/term` for the terminal check. Bubble Tea's own signal handling is off (`tea.WithoutSignalHandler`), so the SIGINT/SIGTERM context from `cmd/main` stays the only outside stop.

_Amended by ADR-018:_ `charm.land/huh/v2` joins the TUI's modules for the setup wizard, imported only by `internal/tui` like the rest. huh v2 is built on the same Bubble Tea v2 modules. The wizard's forms run one after another before the run view opens, and ctrl+c or Esc in any of them cancels the setup.

**Consequences:**

_Positive:_

- Layout, scrolling, input, and resize handling come from maintained libraries
- The looper stays free of UI code and testable without a terminal
- The models are plain values that tests drive directly with messages

_Negative:_

- New third-party dependencies to track and scan (govulncheck, gosec)
- v2 differs from v1 (`View()` returns `tea.View`, keys are `tea.KeyPressMsg`), so most examples and answers written for v1 do not apply
- If Bubble Tea fails to start despite the terminal check, gralph exits 1 and suggests `--no-tui`; there is no automatic fallback

---

### ADR-013: Gralph runs a task's gates after a completed session

**Status:** Accepted

**Context:**

Under ADR-005 the only evidence that a task worked is the session's own result line. The prompts ask Claude to run the task's verification, but gralph checks nothing itself. A session that skips a check, or reports `completed` anyway, marks the task `completed`, and the next task builds on it. The checks that matter (lint, tests, a build) are ordinary commands with exit codes, and gralph can run them the same way every time, whatever the session did or said.

**Decision:**

_Amended by ADR-019:_ gates are one top-level list per file; the per-task list, its example, its dry-run line, and its skill paragraph are replaced by ADR-019.

A task may carry an optional `gates` list. Each entry is a mapping with keys `cmd` (required, nonblank string) and `timeout` (optional, a duration string):

```yaml
tasks:
  - id: 1
    name: Add the widget repository
    prompt: |
      ...
    gates:
      - cmd: golangci-lint run ./...
        timeout: 5m
      - cmd: go test ./internal/widget/...
        timeout: 20m
```

When a session exits zero and ends with the `completed` result line (ADR-005), gralph runs that task's gates in file order, one at a time, each through `sh` in gralph's working directory under a timeout. Each gate's timeout is determined by (in order): the `--gate-timeout` CLI flag if set, the gate's `timeout` field if present, or the built-in default 10m. The task is `completed` only when every gate exits zero before its timeout. The first gate that exits non-zero or times out makes the task `failed` with `error` set to `gate "<cmd>" failed: <exit error>` or `gate "<cmd>" timed out after <timeout>` (`<cmd>` is the command's first line only, so a multi-line gate keeps the reason readable on one line); later gates do not run, the file is saved, and the run stops as it does for any failed task (ADR-003, ADR-006). The rest of the rules:

- **Gates belong to gralph, not to Claude.** Gralph reads them from `tasks.yaml`. They are never sent to the session: the combined prompt on stdin is unchanged, and `Task.String()` does not include them. The shared prompt and task prompts may still tell Claude to verify its own work; that is Claude's self-check and a separate layer.
- **Gates run only after a `completed` session.** If the session failed (non-zero exit, `failed` result, or no valid result line), no gate runs and the task fails with the session's error, as today.
- **A task with no `gates`, or an empty list, behaves exactly as before.**
- **A gate is any command, judged only by its exit code.** Gralph does not restrict what a gate does or look at its output. The skill suggests commands that exit non-zero when the check fails.
- **Per-task only.** There is no file-level gate list and no retry.
- **Every gate has a time limit.** The `--gate-timeout` flag, when passed, overrides every gate, including those with their own `timeout`; otherwise the gate's `timeout` applies; otherwise the built-in 10m. There is no file-level timeout key and no way to run a gate without a limit. The limit is a defence for unattended runs: a gate that never exits would otherwise hang the run. When the limit is hit, gralph kills the gate's process group and the error names the limit that applied, as written where it came from. `--dry-run` lists each gate's effective timeout and its source (`task <id> gate: <first line of cmd>: <timeout> (flag|gate|default)`) after the task table.
- **Validation happens at parse time**, in `internal/tasks`, with the existing error style (`tasks[<i>] (id <id>): gates[<j>]: cmd: <problem>`), so `--dry-run` and the setup screen catch a bad `gates` value. `gates` must be a sequence; each element must be a mapping with keys `cmd` (required, nonblank string) and `timeout` (optional); any other key is an error (`...: <key>: unknown key; a gate has only cmd and timeout`). `timeout`, when present, must be a duration string with a unit (e.g., `90s`, `10m`, as Go's `time.ParseDuration` accepts) and greater than zero; errors are `...: timeout: must be a duration string such as 90s or 10m` and `...: timeout: must be greater than zero`. The stored `cmd` and `timeout` are never altered and never written back from runtime decisions (the `--gate-timeout` flag value is never saved to the file). `SaveTasks` writes `gates` back unchanged (omitted when empty). The `--gate-timeout` flag is validated before any file load or run; a bad value prints an error and exits 1.
- **How the command reaches the shell.** Each gate runs as `sh -c <cmd>`, with the `cmd` text from the file as the script, so pipes, substitutions, and multi-line commands work. gosec flags this call (G204: a subprocess whose argv comes from a variable). The finding is accurate and accepted: running commands from the task file is what a gate is. The call carries a single `// #nosec G204` with its reason, approved by the project owner as an explicit exception to the "never add `#nosec`" rule. It is not a precedent; any other suppression needs the owner's explicit approval too.
- **Process handling matches claude's** (ADR-007): each gate runs in its own process group with no stdin. Cancelling the context while a gate runs kills the group and leaves the task's state and the file untouched, so the next run starts that task again.
- **Output.** Plain mode prints `gate: <cmd>` to stdout before each gate, and the gate's stdout and stderr pass straight through. On the TUI path gralph writes nothing itself: it reports an `Activity` event `→ gate <cmd>` (only the command's first line, so a multi-line command stays one activity line) and then one `Activity` event per line of the gate's stdout and stderr. The task stays `in progress` until its gates finish; `TaskFinished` is reported after them. No new event kinds.
- **The skill** (`gralph-docs-writer`) gains `gates` in its template and field rules, and proposes each task's gates from the project's agreed quality gates plus the task's own checks. It keeps writing the prompt-side verification for Claude.

Alternatives not taken: opt-in only with no default (leaves hand-written files unprotected), file-level default key in the task file (a second place to configure; the `--gate-timeout` flag covers it), the flag acting only as a default or as a cap rather than an override (the override level lets one run adjust without editing the file); letting the gates replace the result line (Claude can know the task was not finished when every gate would still pass); sending the gate list to Claude (gralph's check should not depend on what the session was told); a file-level list shared by all tasks (not needed to start; can be added without changing per-task gates).

**Consequences:**

_Positive:_

- A `completed` state now means gralph itself saw every gate pass, not only that Claude said so
- The same commands run the same way for every task and every run, independent of the session
- Plain mode, the stdin contract, and task files without gates are unchanged
- One place for the rule: both task paths go through the same gate runner after `finishTask`

_Negative:_

- Gate commands are shell text from the task file, run without a sandbox; the file was already code to be trusted (it drives `--dangerously-skip-permissions`), and now gralph executes part of it directly. _Amended by ADR-014:_ sessions no longer always run with `--dangerously-skip-permissions`; gates still run without a sandbox either way
- A failed gate leaves whatever the session did, including commits, in the repository for a person to sort out
- Resetting a gate-failed task to `pending` runs the whole session again, not just the gates; setting it to `completed` by hand skips the gates
- Common gates are repeated in every task
- The gate call carries the project's one approved inline gosec suppression (`#nosec G204`), which a later scanner upgrade or code move has to keep intact
- A gate that never exits is killed by its timeout (the gate's value, `--gate-timeout`, or the 10m default); existing files without explicit timeouts now get the 10m limit instead of running indefinitely
- A gate that changes files leaves those changes uncommitted for the next session
- Claude is not told the gates, so a session can report `completed` and still fail one; the skill keeps the prompt-side verification and the gates in step

---

### ADR-014: Sessions run in Claude Code's sandbox; bypass only on request

**Status:** Accepted

**Context:**

Every session has run as `claude --print --dangerously-skip-permissions` (ADR-001, ADR-003). That flag lets a session run any command and read or write anything the user can: the home directory, SSH keys, cloud credentials, the network. Gralph runs unattended across a whole task list, so nobody is watching when a task file is wrong, a session reads something hostile mid-task (prompt injection), or Claude simply makes a bad call. The only defence so far was a sentence in the README: only run task files you trust.

Claude Code's own documentation says bypass mode is meant for containers and VMs. Claude Code also ships an OS-level sandbox (bubblewrap on Linux, Seatbelt on macOS), configured through its settings, that limits what shell commands and their children can read, write, and reach on the network.

A spike on 2026-09-30 (Claude Code 2.1.286, Linux) ran the same eight probes headless under two configurations and checked the disk afterwards:

| Probe                                   | Sandbox on, defaults, bypass flag kept | Tight sandbox, `--permission-mode acceptEdits` |
| --------------------------------------- | -------------------------------------- | ---------------------------------------------- |
| Shell write inside the project          | worked                                 | worked                                         |
| Shell write outside the project         | blocked                                | blocked                                        |
| Shell read of a file in the home dir    | leaked                                 | blocked                                        |
| `curl` to the internet                  | went through                           | blocked                                        |
| `go build` and run                      | failed (build cache read-only)         | worked (cache path allowed)                    |
| Read tool on a file in the home dir     | leaked                                 | denied                                         |
| Write tool outside the project          | wrote the file                         | denied                                         |
| Session asks to run outside the sandbox | escaped                                | ignored                                        |

Two things follow. Turning the sandbox on while keeping the bypass flag protects almost nothing: the session can ask to leave the sandbox and bypass mode approves it, and the Read and Write tools are not sandboxed at all. And the tight configuration is per project: Go needed read access to `GOROOT` and `GOPATH` and write access to its build cache, and every other toolchain has its own list. Gralph cannot know that list.

**Decision:**

A run needs exactly one of two new flags. There is no default.

- `--sandbox-settings <path>`: a Claude Code settings JSON file. Gralph runs every session sandboxed with it.
- `--skip-permissions`: no sandbox. Sessions run with `--dangerously-skip-permissions`, exactly as before this ADR. The user who passes it owns the result; the README says so in plain words.

The rules:

- **Neither flag, or both, is an error.** A real run (plain or TUI) with neither exits 1 with `error: pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox`. Both together exits 1 with `error: --sandbox-settings and --skip-permissions cannot be used together`. Both checks happen in every mode, after the `--gate-timeout` check and plain mode's missing `--tasks`/`--prompt` check, and before the skill check and any task or prompt file loads. An empty `--sandbox-settings=` counts as not passed. The setup screen does not ask for either; it only asks for paths that have a safe meaning when missing.

  _Amended by ADR-018:_ in the TUI, passing neither flag is no longer an error at startup: the setup wizard asks for the permission choice, with nothing pre-selected, so there is still no default. Its answer then goes through the same check, strict, before anything runs. Plain mode and the both-flags error are unchanged.
- **The settings file is validated before anything runs.** It must be readable and hold a JSON object; `sandbox`, when present, must be an object. Errors read `--sandbox-settings: <problem>` and exit 1. Gralph does not check the rest: unknown keys and bad values are Claude Code's to reject.
- **Gralph forces three keys and leaves the rest alone.** On top of the user's file it sets `sandbox.enabled: true`, `sandbox.allowUnsandboxedCommands: false`, and `sandbox.failIfUnavailable: true`. The first turns the sandbox on. The second closes the escape hatch the spike showed. The third stops a file from asking Claude to carry on unsandboxed when the sandbox cannot start. Paths, domains, and every other settings key pass through untouched. The file on disk is never written.
- **The argv.** Sandboxed sessions run as `claude --print --permission-mode acceptEdits --settings <merged JSON>`; the TUI path still inserts `--output-format stream-json --verbose` right after `--print`. With `--skip-permissions` the argv is today's, unchanged. The merged settings travel inline as one compact JSON argument, so there is no temporary file to clean up. Both paths still build the command in `claudeCmd` (ADR-011).
- **Why `acceptEdits`.** It auto-approves file edits inside the working directory. Sandboxed shell commands are auto-approved by the sandbox's own default. Everything else that would prompt (a tool reading or writing outside the project, a web fetch with no allow rule) is denied, because a `--print` session has nobody to ask. The session sees the denial and carries on or gives up; the outcome rule (ADR-005) is unchanged.
- **Read once.** The file is read, checked, and merged once at startup. Editing it during a run changes nothing until the next run.
- **`--dry-run`** needs neither flag, since it runs nothing. Given `--sandbox-settings`, it validates the file and prints `sandbox settings: <path>` before the final `<tasks path> is valid` line. The both-flags error still applies.
- **The stdin contract is unchanged**, as are task files, state handling, and gates.
- **Gates are not sandboxed.** Gralph runs them itself through `sh` (ADR-013); this ADR covers sessions only.
- **No detection, no fallback.** If the sandbox cannot start (no bubblewrap or socat, an unsupported OS), Claude exits non-zero and the task fails by the normal rule. Gralph does not probe for the sandbox and never drops to an unsandboxed run on its own.

Alternatives not taken: tightening permissions with an allowlist of tools alone (a string check before the command runs; an allowed `make` or `go test` is still arbitrary code); gralph running sessions in a container (needs an image per toolchain, credential and file-ownership plumbing, gates moved inside, and a new way to kill the session); sandbox fields in the task file (grows the YAML format and the skill for something that belongs to the machine, not the task list); gralph guessing the toolchain's paths (guesswork, and wrong guesses fail in confusing ways); bypass as the default with the sandbox opt-in (the unsafe path stays the easy one).

**Consequences:**

_Positive:_

- A session can no longer read the home directory, write outside the project, or reach the network unless the user's file allows it, and the operating system enforces that for shell commands whatever the session was told
- The unsafe path still exists but has to be asked for by name on every run
- One small seam: a load-and-merge function and a change to `claudeCmd`; the stdin contract, outcome rule, and process handling are untouched
- No new dependency in gralph; the sandbox is Claude Code's

_Negative:_

- Breaking change: every existing invocation fails until it adds one of the two flags
- The user has to write and maintain a settings file per project and toolchain (allowed paths, allowed domains); a file that is too tight makes tasks fail in ways that take a session to discover
- Sandboxed runs need Claude Code's sandbox to work: bubblewrap and socat on Linux, Seatbelt on macOS; on the BSDs only `--skip-permissions` works
- The file can still loosen things: `sandbox.excludedCommands`, broad `allowWrite` paths, and permission allow rules are the user's to set, and gralph does not second-guess them
- The user's own Claude settings (`~/.claude/settings.json`, the project's `.claude/settings.json`) still merge in, so the same sandbox file can behave differently on two machines
- Network limits are by hostname through a proxy; Claude Code's documentation notes that tricks such as domain fronting can get past it
- The Read and Edit tools are held by permission rules, not the OS sandbox; that is as strong as Claude Code's permission checks
- Gates still run unsandboxed
- The merged JSON shows up in the process list as an argument; it holds paths and domains, not secrets, unless the user puts some there
- Verified on Linux only; macOS and the TUI's stream-json argv were not part of the spike

---

### ADR-015: Gralph commits a completed task on request

**Status:** Accepted

**Context:**

The shared prompt has told the session to commit its own work, "only after the task's verification passes". The session cannot keep that rule. The verification that counts is the task's gates (ADR-013), and gralph runs those after the session has exited. So a session commits first, a gate fails afterwards, and the history holds a commit for a task gralph marked `failed`; ADR-013 lists this among its costs.

The rule is also gralph's business leaking into the session. A session needs its task and the project's rules. When a commit is allowed depends on gralph's order of work, which the session has no need to know.

**Decision:**

A new `--commit` flag makes gralph commit each task itself. Without the flag gralph never calls git, as before this ADR.

- **Opt-in per run.** `--commit` is a boolean flag with no value and no default-on. There is no task-file key for it.
- **Outside a repository the flag does nothing.** At startup gralph asks git whether its working directory is inside a work tree (`git rev-parse --show-toplevel`). If git says it is not, or git is not installed, the run goes ahead with no check and no commits. Any other git failure (a broken config, a repository git refuses to trust) is an error, `--commit: git rev-parse: <git's message>`, exit 1: a repository gralph cannot read must not turn into a run that silently commits nothing. Plain mode and `--dry-run` print `commit: not a git repository, nothing will be committed` to stdout; the full-screen view prints nothing. Gralph does not require a repository.
- **Git must not see the task file.** Inside a repository, the task file has to be ignored by git or kept outside the work tree; otherwise gralph exits 1 with `error: --commit needs the task file ignored by git or outside the repository: <path>`. Gralph rewrites the file after every task, and it never names the file to git, so an ignore rule is the only thing keeping it out of the commits. A tracked task file is refused by the same check.
- **Inside a repository the work tree must be clean at startup.** If `git status --porcelain` reports any change in the work tree (modified, staged, or untracked and not ignored), gralph exits 1 with `error: --commit needs a clean work tree; commit, stash, or remove:` followed by the paths. This happens in every mode, after the task file is loaded and the failed-task refusal (ADR-006), and before the first session; in the TUI, after the setup screen and before the view opens. The check is what makes each commit hold exactly one task's work.

  _Amended by ADR-018:_ in the TUI the check runs after the setup wizard, which replaced the setup screen. The wizard's commit step runs the same checks when the user says yes, so a dirty tree or a task file git can see is refused there first.
- **When gralph commits.** The order for a task is session, gates, commit, save. The commit step runs only when the session reported `completed` (ADR-005) and every gate passed (ADR-013). A failed session or gate means no commit.
- **What is committed.** Gralph stages every change in the work tree with a plain `git add -A`, run from the work tree's root, and commits it: exactly what git would stage for a person, so ignored files stay out. Gralph passes git no path list and no exclusions. Gralph cannot tell which files a session touched, and with a clean start it does not need to. Files changed by a gate are part of the commit.
- **The message** is the task's `name`, passed to `git commit -m` as stored. There is no message field in the task file and the session supplies nothing.
- **Nothing to commit is not a failure.** If nothing is staged, gralph makes no commit and the task is `completed`. It never creates an empty commit. This also covers a session that committed by itself: gralph does not police sessions.
- **A failed commit fails the task.** If `git add` or `git commit` exits non-zero (a pre-commit hook rejects the change, no author is configured), the task is `failed` with `error` set to `commit failed: <exit error>`, the file is saved, and the run stops as for any failed task (ADR-003, ADR-006). Hooks always run; gralph never passes `--no-verify`.
- **A failed task leaves its changes in the work tree.** Gralph never resets, stashes, or cleans. The next `--commit` run is refused by the clean-tree check until a person deals with the leftovers: discard or stash them and set the task to `pending`, or finish the work, commit it, and set the task to `completed`.
- **Gralph never pushes** and sets no author: the commit is made with the user's own git configuration.
- **Process handling.** Git runs in its own process group with no stdin, like a gate (ADR-013). Unlike a gate, a cancel first sends the group SIGTERM and only kills it after a short grace period: git removes its `index.lock` when asked to stop, and a killed git leaves the lock behind, which blocks every later git command in the repository. Cancelling the context during the commit step leaves the task's state and the file untouched.
- **Output.** Once something is staged, plain mode prints `commit: <name>` to stdout, and git's output passes straight through. On the TUI path gralph reports an `Activity` event `→ commit <name>` and one `Activity` event per line of git's output. Both show only the name's first line. No new event kinds.
- **`--dry-run`** with `--commit` runs the same startup check. In a clean repository it prints `commit: <work tree root>` before the final `<tasks path> is valid` line; a dirty tree exits 1 with the error above; outside a repository it prints the not-a-repository line.
- **The stdin contract is unchanged.** The session is told nothing about `--commit`.
- **The skill and its prompt template.** The template loses the "commit only after verification passes" rule. The skill asks whether the run will use `--commit`. If it will, the generated `prompt.md` tells the session not to commit and to leave its changes in the work tree; if not, it carries the project's own commit rules, as before.
- **Keep the run's files out of git.** The task file must be ignored or outside the repository (above). The HOWTO and the skill recommend the same for the shared prompt and a sandbox settings file kept in the project: add them, or the directory that holds them, to the project's `.gitignore`. Ignored files never trip the clean-tree check and are never staged. Gralph does not edit `.gitignore` itself; when the skill writes the pair for a `--commit` run it tells the user which line to add.

Alternatives not taken: always committing (breaks projects that are not repositories or do not want gralph's commits); a per-task key (grows the task file for a choice that belongs to the run); a commit message field in the task file, or one supplied by the session in its result line (the name already reads as a subject, and the second puts commit knowledge back in the session); requiring a repository when `--commit` is passed (there is simply nothing to commit there); passing git an exclude pathspec for the task file (the first implementation did: `git add` rejects a pathspec that names an ignored path, the answer can change mid-run, a symlinked task file slips past it, and a path list on `git commit` holds the index lock for the whole hook run; a plain `git add -A` plus an ignore rule has none of these problems); gralph discarding a failed task's changes (destroys the evidence, and gralph deleting work unasked); allowing a dirty tree on a rerun (mixes two attempts in one commit and needs gralph to remember which changes were leftovers).

**Consequences:**

_Positive:_

- A commit now means gralph saw the session finish and every gate pass; a failed task leaves no commit behind
- One commit per completed task, named after the task, with nothing else in it
- The session needs no rule about when to commit, and the shared prompt no longer describes gralph's order of work
- Runs without `--commit`, the stdin contract, and task files are unchanged

_Negative:_

- A run with `--commit` needs a clean work tree, so the prompt file, a sandbox settings file, and anything else kept in the repository must be committed or ignored first; the task file must be ignored or outside the repository
- After a failed task a person has to clean the work tree by hand before the next `--commit` run
- A task file tracked by git cannot be used with `--commit`
- The commit message is only the task name; there is no body describing the change
- Gralph runs git, and so the repository's hooks, outside the sandbox (ADR-014), like gates; a session can change a hook script that gralph then runs
- The commit step has no time limit, unlike a gate; a hook that never exits hangs the run until it is stopped
- A session can still commit by itself if its prompt does not forbid it; gralph does not detect that
- Passing `--commit` outside a repository is easy to miss in the full-screen view, which prints no notice

> Amended by ADR-016: the looper now also reports a `Committed` event after a commit is made, so "No new event kinds" above no longer holds. The `Activity` lines are unchanged.

---

### ADR-016: Gralph logs a run on request

**Status:** Accepted

**Context:**

Once a run in the full-screen view ends and the view closes, nothing is left of it but each task's `state` and `error` in the task file. The Claude activity pane, the gates' output, and git's output are gone. Two things cannot be done afterwards: read what a failed (or odd) task did, and see what a run did and when: which tasks ran, how long each gate took, which commit a task produced.

Plain mode does not have the problem: everything it shows goes to stdout and stderr, and a shell redirect keeps it.

The looper already tells the view what happens through `Event`s (`TaskStarted`, `Activity`, `TaskFinished`, `RunDone`), but a gate or a commit is only a display line there (`→ gate <cmd>`). Its result, its duration, and the commit's hash are known to the looper and never reported.

**Decision:**

A new `--log-dir <path>` flag makes gralph write a record of the run. Without the flag gralph writes nothing but the task file, as before this ADR. Gralph writes the record itself from what it observes; the session is told nothing and the stdin contract is unchanged.

- **Opt-in per run.** `--log-dir` takes the directory to write under. There is no default location and no task-file key. An empty `--log-dir=` counts as not passed.
- **Full-screen view only.** With `--no-tui`, or when stdin or stdout is not a terminal, a run given `--log-dir` exits 1 with `error: --log-dir only works with the full-screen view` before anything loads or runs. `--dry-run` ignores the flag completely, as it ignores `--prompt`: no check, no output line, nothing created.
- **One folder per run.** At startup gralph creates `<log-dir>` if needed and, inside it, a folder named after the run's start time in local time, `YYYYMMDDTHHMMSS` (for example `20261001T140211`). Folders are created with mode `0700` and files with `0600`: the record holds Claude's text and whatever a gate or git printed. If the run folder already exists, or anything cannot be created, gralph exits 1 with an error starting `--log-dir:` after the setup screen and before the view opens or any session starts. Gralph never rotates or deletes old run folders.

  _Amended by ADR-018:_ the setup screen is now the setup wizard, so these errors come after it. With a run folder, the wizard offers `<folder>/logs`, used only when the user says yes; with no yes and no flag, nothing is logged. An explicit `--log-dir` wins and hides the question.
- **With `--commit`, git must not see the logs.** When a repository is open (ADR-015) and the run folder is inside its work tree, the folder must be ignored by git; otherwise gralph exits 1 with `error: --commit needs the log directory ignored by git or outside the repository: <path>`. The check is made on the run folder's path before it is created, after the task-file and clean-tree checks. `git add -A` takes no exclusions, so an ignore rule is the only thing keeping a log out of a task's commit. Without `--commit`, or outside a repository, there is no check.
- **The ledger, `run.jsonl`.** One JSON object per line, written as each thing happens. Every line has `time` (RFC 3339, local time with its UTC offset, like `2026-10-01T14:02:11-04:00`) and `event`:

  | `event`            | Other fields                                                                                                                                                          |
  | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
  | `run_started`      | `version`, `tasks_file`, `prompt_file`, `permissions` (`sandbox` or `skip`), `sandbox_settings` (the path, when given), `gate_timeout` (when passed), `commit` (bool) |
  | `task_started`     | `task` (the id), `name`                                                                                                                                               |
  | `session_finished` | `task`, `state`, `error` (when not empty), `duration`                                                                                                                 |
  | `gate_finished`    | `task`, `cmd` (first line), `result` (`passed`, `failed`, or `timed_out`), `timeout`, `duration`                                                                      |
  | `committed`        | `task`, `hash`                                                                                                                                                        |
  | `task_finished`    | `task`, `state`, `error` (when not empty), `duration`                                                                                                                 |
  | `run_finished`     | `result` (`completed`, `failed`, or `stopped`), `error` (when not empty), `duration`                                                                                  |

  Durations are strings as Go prints them (`41.2s`). A task skipped because it is already `completed` writes no line. `committed` is written only when a commit was made; nothing staged writes nothing. A stopped run writes `run_finished` with `stopped` and no `task_finished` for the task that was running, which matches the task file: that task stays as it was. `run_finished` says `failed` when the last `task_finished` was a failed task, and `stopped` when the run ended with any other error: the looper's error does not say which it was, but a stopped task never gets a `task_finished`. The ledger does not tell a stop by the user from a stop by signal. The sandbox settings file's path is recorded, never its contents.

- **The detail files, `task-<id>.log`.** One plain-text file per task that ran, holding every `Activity` line reported for that task, in order, each prefixed with its local time as `HH:MM:SS`: Claude's text, the `→ <tool> <target>` lines, stderr, each `→ gate` line and the gate's output, the `→ commit` line and git's output. It is what the Claude activity pane showed. The raw stream-json is not kept.
- **New looper events.** On the stream path only, the looper reports three more event kinds: `SessionFinished` (the session's outcome and duration), `GateFinished` (the gate, its timeout, its error, where nil means passed, and its duration), and `Committed` (the new commit's hash, read with one `git rev-parse HEAD` after a successful commit; if the hash cannot be read the event is not sent and the task stays `completed`, because the commit is already made and a lookup for the record must not change the task's outcome). `TaskFinished` and `RunDone` gain a duration. The looper measures the durations, being the only part that knows when a step began. The view ignores the new kinds, so the screen is unchanged, and the plain path reports nothing, as before.
- **A separate package writes the files.** `internal/runlog` turns events into the ledger and detail lines: `RunDir` names the run folder, so that `cmd/main` can have the repository check that path before anything exists; `Open` creates the folder and writes `run_started`, `Record` handles one event, `Close` closes the files. It imports `looper` for the event type; `looper` and `tui` do not import it. `cmd/main` opens the log and hands `Record` to `tui.Run` as an optional `observe func(looper.Event) error`, called for every event before the view gets it. The ledger is written with `encoding/json`; there is no new dependency.
- **A write error stops the run.** The first time `observe` returns an error, `tui.Run` cancels the run, and the final status and summary read `Run stopped: log: <error>`, exit 1. The running task stays as it was, as for any stop, and the view shows it as pending again: a run that ends with an error resets any task still shown as in progress. `observe` is not called again after its first error, so the ledger simply ends there, with no `run_finished` line. There is no other handling: the case is rare, and a full disk usually stops the run through the task-file save anyway.

Alternatives not taken: the looper writing the log itself (a ninth parameter on `Run` and a second job for the loop; every later consumer of the same facts would need threading through it again, where another listener on the events costs the looper nothing); a logger that parses the `→ gate` and `→ commit` activity lines (guesses structure from display text, cannot tell a gate's result or duration, and a line of Claude's text can imitate one); logging in plain mode (a redirect already keeps what plain mode prints, and a full record would mean running claude with stream-json there, which changes plain mode's output contract, ADR-011); keeping the raw stream-json (very large, and it holds everything a tool read); a single log file for ledger and detail (the compact trail gets buried); a plain-text ledger (anything reading it later has to guess the format); `log/slog` for the ledger (its handlers insist on `level` and `msg` keys that would need stripping); a fixed log location next to the task file (gralph choosing where to write inside a project); having `--dry-run` check the folder (nothing is logged in a dry run); carrying on after a failed write (a run with a hole in its record is what the flag exists to prevent).

**Consequences:**

_Positive:_

- A failed task's full activity, gate output, and git output can be read after the view closes
- A run leaves a compact, machine-readable trail: what ran, with which flags, how long each step took, and which commit each task produced
- Gate and commit results become structured events that any later listener (or the view) can use without changes to the loop
- Runs without `--log-dir`, plain mode, the stdin contract, and task files are unchanged

_Negative:_

- Logging is not available in plain mode; a command line with `--log-dir` fails when reused in a pipe or in CI
- The logs can hold anything a session, gate, or hook printed, secrets included; the file modes keep them private to the user, nothing more
- Run folders pile up until a person deletes them
- With `--commit`, a log folder inside the repository needs an ignore rule first
- A stop by the user and a stop by signal read the same in the ledger
- A write error cancels the running task; its work so far stays in the work tree, uncommitted
- Without a terminal the e2e suite can pin only the two plain-mode rules; one test under a pseudo-terminal (`log_dir_pty_linux_test.go`) pins the ledger and the task log of a stopped run, and the rest of the logging path is covered by unit tests

---

### ADR-017: Release archives, built by a workflow started by hand

**Status:** Accepted

**Context:**

The only way to get gralph is to clone the repository and run `make local install`, which needs Go and make. `go install` does not work: the module lives in `src/`, so its path does not match its location, the binary would be named `main`, and it would print `dev` as its version. People who want to run Ralph loops do not all have a Go toolchain.

Everything needed for a download already exists. `build/build.sh` (ADR-008) cross-compiles `gralph` for Linux and macOS on amd64 and arm64 into `.bin/<arch>/<os>/gralph`, after the linters, the scanners, and the unit tests, and then runs the e2e suite. Versions are signed git tags the owner makes by hand on `develop`. The repository has tags but no GitHub releases, and CI runs on pushes and pull requests to `develop` and `main`, never on a tag. `docs/howto.md` was written to ship next to the binary.

**Decision:**

Each version gets a GitHub release with one archive per platform. A workflow the owner starts by hand builds the archives and leaves the release as a draft.

- **Started by hand, per tag.** `.github/workflows/release.yaml` has `workflow_dispatch` as its only trigger and one required input, `tag`. Pushing a tag starts nothing. The workflow file must be on the default branch (`develop`) for GitHub to offer the run.
- **The tag must exist and look like a version.** The input must match `vMAJOR.MINOR.PATCH` (digits only) or the run fails before checkout. The input reaches the shell through an environment variable, never by being pasted into a script. The workflow checks out that tag with full history; a name that is not a tag fails the run. The workflow never creates, moves, or signs a tag.
- **Same build as CI.** The workflow runs `build/build.sh` with `BUILD_VER` set to the tag, so a release passes the linters, scanners, unit tests, and e2e suite on the exact commit it ships. Before packaging, the workflow runs the linux/amd64 binary with `--version` and fails unless the output names the tag.
- **`build/package.sh` makes the archives.** It takes the version from `BUILD_VER`, the variable `build/build.sh` uses, and requires the same `vMAJOR.MINOR.PATCH` form. It reads the four binaries from `.bin/` and writes to `.dist/` (git-ignored; archives it left there for another version are removed, nothing else):
  - `gralph_<version>_<os>_<arch>.tar.gz` for `linux` and `darwin`, `amd64` and `arm64`. Each holds three files at its top level: `gralph` (executable), `howto.md`, and `LICENSE`.
  - `checksums.txt`: one SHA-256 line per archive in `sha256sum` format, so `sha256sum -c` (Linux) and `shasum -a 256 -c` (macOS) can check a download.

  The four binaries, the HOWTO, and the license are checked before anything is written: a missing one is an error, exit 1, and `.dist/` gets no files. The archives and `checksums.txt` are built in a temporary folder and moved into `.dist/` only once all of them exist, so a failure while building them (a failed `tar`) exits 1 and leaves `.dist/` as it was. Another version's archives are removed only after the new files are in place, so a failed move (a full disk) also exits 1 and loses nothing that was there. The script takes no part in building: it can be run on any machine after `make build`.
- **A draft release.** The last step runs `gh release create <tag> --draft --verify-tag --generate-notes` with everything in `.dist/` attached. The owner reads the draft and publishes it. A second run for a tag that already has a release fails; delete the draft to run again.
- **Least permission.** The workflow's token gets `contents: write` and nothing else. The CI workflow is unchanged.
- **Documented install.** `docs/howto.md` gains an "Installing gralph" section: download the archive for the machine, check it against `checksums.txt`, unpack only `gralph`, straight into `~/.local/bin` (the archive is flat, so unpacking all of it would overwrite a `LICENSE` or `howto.md` in the reader's directory), run `gralph --install-skill`. The commands take the version from a `VERSION` variable the reader sets, because the archive names carry the version. The section says to add `~/.local/bin` to `PATH` when it is not already there, and how to upgrade (the same steps, then `gralph --install-skill` again, ADR-010). The README carries a short version that links there. The clone-and-make path stays in the README for development.
- **Maturity label.** The README's label moves from Emerging to Basic in the same change: with a documented install and the planned work being conveniences that leave existing behaviour alone, the CLI contract is no longer expected to shift between minor versions. The README still says a breaking change can land before 1.0 and that sandboxed runs are tested on Linux only.

Alternatives not taken: a release on every tag push (the owner wants to start each release, at least for now); publishing straight away (a draft costs one click and keeps a bad run from being public); bare binaries (they need `chmod` and a rename, and the HOWTO would not travel with them); an install script or a Homebrew tap (more to write and keep working; either can sit on top of these archives later); GoReleaser (it would build outside the pinned image and skip the checks built into it, and it is a new dependency); packaging written into the workflow file (it could only be tried by running a release); moving the module back to the repository root so `go install` works (it still needs Go, and it undoes the `src/` layout); signing or attesting the binaries (a checksum file is enough for now); a Windows archive (ADR-007).

**Consequences:**

_Positive:_

- Gralph can be installed with `curl` and `tar`; no Go, make, Docker, or clone
- A released binary went through the same build and tests as CI, on the tagged commit, on a clean runner
- The HOWTO and the license travel with the binary
- Nothing public happens without the owner: the tag, the run, and the publish are each a manual step
- The packaging can be run and tested without GitHub

_Negative:_

- A release takes three manual steps (tag, run, publish); a forgotten run leaves a tag with no download
- The first run can only be tried on a tag that already holds `build/package.sh`, so the workflow is proven by its first real release
- The macOS archives are untested; a download made with a browser is quarantined by Gatekeeper and needs `xattr -d com.apple.quarantine` or a `curl` download
- The binaries are not signed; `checksums.txt` comes from the same release, so it catches a damaged download, not a tampered release
- The tests for `build/package.sh` (BATS, in `tests/bats/`, with `bats-support` and `bats-assert` vendored beside them) are not run by CI (`build/build.sh` has no place for them); they are run by hand with `bats tests/bats`
- No package manager knows about gralph; upgrading means downloading again

---

### ADR-018: A run folder and a setup wizard

**Status:** Accepted

**Context:**

A full run names every input on the command line: `gralph -p .local/feat/prompt.md -t .local/feat/tasks.yaml --sandbox-settings ~/sandbox.json --commit --log-dir .local/feat/logs --gate-timeout 5m`. Most of it repeats from run to run, and the two files almost always sit together in one folder, under the names the gralph-docs-writer skill gives them. The full-screen view's setup screen (ADR-011) asks only for a missing `--tasks` or `--prompt` path, typed by hand, and a missing permission flag (ADR-014) is still an error. A user who starts `gralph` with no arguments cannot get to a run without knowing the flags.

**Decision:**

Two additions: a `-d/--dir` flag that names a run folder, and a setup wizard in the full-screen view that asks for whatever the flags and the task file leave open, then shows a review screen before the run starts.

- **The run folder.** `-d/--dir <folder>` means `<folder>/tasks.yaml` and `<folder>/prompt.md`. The names are fixed; `-t` and `-p` override either file, so `-d foo -t foo/tasks-v2.yaml` works and every command line that works today keeps working. A file the folder lacks and no flag supplies is an error in plain mode, `--dir: no tasks.yaml in <folder>` (or `prompt.md`), exit 1. In plain mode `-d` is shorthand for `-t`/`-p` and changes nothing else. `-d` is resolved into the two paths before any other startup check, so the fixed order after it (`--gate-timeout`, plain mode's required flags, `--log-dir`, the session flags, the skill check) is unchanged.
- **Logs in the folder.** With a folder, saying yes to logging in the wizard means `--log-dir <folder>/logs`. An explicit `--log-dir` wins. This is an opt-in made in the wizard, not a default: with no wizard answer and no flag, nothing is logged, and plain mode still refuses `--log-dir` (ADR-016).
- **When the wizard opens.** Full-screen mode only, and only when the folder, permissions, or gates step is still open. Commit, logging, and the gate timeout ride along when it opens and never open it alone; otherwise every run without `--commit` would stop to ask. When flags and the task file answer those three, there is no wizard and no review screen; the run starts as it does today. Plain mode never opens it; a missing input there is an error, as today. A dry run is always plain mode, so it never opens the wizard either. The wizard replaces the setup screen.
- **The steps, in order, each hidden when something already answers it:**

  | Step         | Asks                                                                    | Hidden when                                                   |
  | ------------ | ----------------------------------------------------------------------- | ------------------------------------------------------------- |
  | Folder       | A folder, browsed with a picker; a lone `-t` or `-p` overrides its file | `-d`, or both `-t` and `-p`                                   |
  | Permissions  | Sandbox or skip permissions, nothing pre-selected; sandbox picks a JSON | `--sandbox-settings` or `--skip-permissions`                  |
  | Commit       | Yes or no, "No" pre-selected (the same as no flag)                      | `--commit`                                                    |
  | Logging      | Yes or no, "No" pre-selected; yes means `<folder>/logs`                 | `--log-dir`, or no folder                                     |
  | Gate timeout | "Default (each gate's own timeout, else 10m)", pre-selected, or a value | `--gate-timeout`                                              |
  | Gates        | The file's shared gate list (ADR-019): add, edit, delete                | The task file has a top-level `gates:` key, even an empty one |

- **Pickers.** The folder picker starts in the current working directory (not limited to it) and shows hidden entries, because run folders usually live under `.local/`. It selects folders only: with just one of `-t` or `-p` passed, the user still picks a folder, which supplies the other file. Chosen paths are shown relative to the working directory when possible. There is no picker for the task or prompt file. The sandbox picker selects `.json` files and also starts in the working directory with paths shown relative to it.
- **Checked where they are asked.** Each step validates its answer with the same functions a run uses and keeps the user on the step with the error shown: the folder step loads both files (a missing file, a parse error, or a `failed` task, the last pointing at `gralph -d <folder> --dry-run` for the table); the sandbox picker loads the settings file; the commit step, on yes, opens the repository as a `--commit` run does (`looper.OpenRepo`), so a dirty work tree or a task file git does not ignore is refused there (ADR-015); the logging step refuses yes when the run commits (by flag or the commit step) and `<folder>/logs` is inside the work tree but not ignored by git (ADR-015, ADR-016); the timeout step parses the value with `time.ParseDuration` and requires it greater than zero.
- **One source of truth.** The wizard's answers fill the same values the flags fill. After it closes, the existing startup checks run unchanged on those values; they never assume the wizard checked anything.
- **The review screen.** Lists every choice, the gates (or "no gates"), and when `-t` or `-p` override the folder, also `Tasks:` and/or `Prompt:`. Then the command line that reproduces the run, such as `gralph -d .local/foo --sandbox-settings ~/sb.json --commit`. Gates are not on the command line; they are in the file. The choices are Start, Edit gates (back to the gates step), and Cancel.
- **Nothing is written before Start.** On Start, the commit repository check (clean tree, task file ignored) and the log folder's git-ignore check run first, before the gates are saved to the task file. If both pass, gralph saves the task file with the new `gates:` list if changed, prints one unwrapped line to stdout (`Same run, no wizard: <command>`) before the full-screen view opens (so the command can be copied even when wrapped), and the run begins. The wizard's forms run without Bubble Tea's signal handler so gralph's signal context is the only path that stops them, avoiding a race where the two handlers could crash or hang the process. Esc or ctrl+c anywhere prints `error: setup cancelled` and exits 1 with nothing written, the message the setup screen used.
- **Built with huh.** The wizard is a `charm.land/huh/v2` form inside `internal/tui`: `FilePicker` (with `DirAllowed`, `FileAllowed`, `ShowHidden`, `AllowedTypes`, `Validate`), `Select`, `Confirm`, `Input`, and `Note`, with groups hidden per run by `WithHideFunc`. huh v2 is built on the Bubble Tea v2 modules gralph already uses. It has no list editor, so the gates step is a short loop of small huh forms (add, edit, delete, done; each gate a command and an optional timeout). Which steps are open is a plain function of the flags and the task file, and the reproducing command line is a plain function of the answers, so both are tested without a terminal.

**Corrected on acceptance:** the proposal opened the wizard when any step was open and had `--dry-run` show the folder and file steps. Planning found that the commit, logging, and timeout steps would then open it on every run without those flags, and that a dry run is always plain mode, so the wizard opens only for the folder, permissions, or gates step, and never on a dry run. The commit step's check was added to the list above.

**Changes to earlier ADRs on acceptance:** ADR-011's setup screen becomes this wizard. ADR-012 adds `charm.land/huh/v2` to the TUI's modules, still imported only by `internal/tui`. ADR-014 notes that the wizard asks for the permission choice with nothing pre-selected, so there is still no default. ADR-015 notes that the clean-tree check runs after the wizard, and on its commit step. ADR-016 notes that `<folder>/logs` is used only when the user says yes in the wizard.

Alternatives not taken: looking for any `*.md` and `*.yaml` pair in the folder (unpredictable, and needs an ambiguity prompt); a bare positional folder argument (every other input is a flag); the wizard only on a bare `gralph` (a single flag would throw away all its help); the wizard on every run, with flags only pre-filling it (second-guesses what the user typed); a remembered settings file (a stored default for the permission choice, against ADR-014); a hand-built multi-step model on `textinput` and `bubbles/filepicker` (more code for the same screens); a new `internal/wizard` package (a second package and import edge for what is the setup screen grown up).

**Consequences:**

_Positive:_

- `gralph` with no arguments walks a user from nothing to a running loop, without knowing the flags
- `gralph -d .local/feat --sandbox-settings ~/sb.json` replaces the two file paths
- The review screen teaches the flags: its command line can be copied to skip the wizard next time
- Every existing command line, and plain mode, behaves as before

_Negative:_

- A new third-party dependency to track and scan
- A run without `--commit` or `--log-dir` still shows those steps whenever the wizard opens for anything else; "No" is pre-selected, so each costs one Enter
- Two e2e tests run the binary under a pseudo-terminal: `wizard_signal_linux_test.go` pins that SIGTERM and SIGINT cancel the open wizard, and `wizard_pty_linux_test.go` walks the happy path with real keystrokes; the other wizard steps are covered by unit tests and checked by hand
- Fixed names mean a folder holding two task files still needs `-t`

---

### ADR-019: One gate list per task file

**Status:** Accepted

**Context:**

Under ADR-013 each task carries its own `gates`. In practice the gralph-docs-writer skill interviews the user once about quality gates and copies the same list into every task, so the check that decides whether a task is done is written by a language model, once per task, and can drift between tasks. ADR-013 left room for a file-level list. The setup wizard (ADR-018) needs one place to show and edit the gates for a run.

**Decision:**

Gates move from each task to one top-level list in the task file, and per-task gates are removed. The wizard owns the list; the skill stops writing gates.

- **Format.** A top-level `gates:` sequence next to `tasks:`. Each entry keeps ADR-013's shape and validation: `cmd` required and nonblank, `timeout` optional with a unit and greater than zero, any other key an error. Errors name the list as `gates[<j>]: ...`, in the existing style.

  ```yaml
  gates:
    - cmd: make test
      timeout: 10m
    - cmd: test -z "$(gofmt -l .)"
  tasks:
    - id: 1
      ...
  ```

- **Absent and empty differ.** No `gates:` key means not decided yet, and the wizard's gates step opens. `gates: []` means decided: no gates. `SaveTasks` keeps whichever the file had: an absent key stays absent, an empty list is written as `gates: []`. Zero gates is a valid run.
- **Per-task gates are an error.** A `gates` key on a task fails parsing with `tasks[<i>] (id <id>): gates: gates are set once for the whole file now, as a top-level gates: list; see the HOWTO`, so `--dry-run`, the wizard's folder step, and a run all report it. Other unknown task keys are still ignored and dropped on save. Dropping per-task gates quietly would leave an old file running with no checks at all.
- **Running them.** After every session that reports `completed` (ADR-005), gralph runs the file's gates in order, with ADR-013's rules unchanged: the timeout order (`--gate-timeout`, then the gate's `timeout`, then 10m), the first failure fails the task, the same error text, output, process handling, and cancel behaviour, and the same events and log record. `--dry-run` lists the gates once, `gate: <first line of cmd>: <timeout> (flag|gate|default)`, instead of once per task.
- **The skill.** gralph-docs-writer drops its gate interview and never writes a `gates:` key, so a freshly generated file opens the wizard's gates step on its first run. The prompt template gains a generic instruction to run the project's own checks (tests, linters, the build) before reporting `completed`. In its generated files, the skill guides the person writing and editing them to check each task against every run-level gate, and to add to the run's gates any check the session cannot run itself. The stale-skill check (ADR-010) makes anyone with the old skill reinstall it.
- **The wire contract is unchanged.** Gates are still never sent to Claude.

**Changes to earlier ADRs on acceptance:** ADR-013 is amended: its "Per-task only" rule, its example, its dry-run line, and its skill paragraph give way to this ADR; the rest of its rules apply to the file's list.

Alternatives not taken: keeping per-task gates beside the shared list (two places to look for one check, and the skill only ever wrote one list); the skill writing the shared list and the wizard only reviewing it (keeps a model-written check as the default); sending the gates to Claude so the session knows what it will be checked against (breaks the wire contract and puts gralph knowledge in the session); a separate `gates.yaml` in the run folder (a second file to load, and a `--gates` flag for runs without a folder); gates held only in the wizard for one run (not repeatable).

**Consequences:**

_Positive:_

- One list, written by a person, decides whether every task is done
- The wizard has a single place to show and edit the run's checks
- The skill and the task format get simpler

_Negative:_

- A breaking change to the task file: files with per-task gates must be edited by hand before they run again
- A check that only one task needs can no longer be a gate; it lives in that task's prompt, as Claude's self-check
- Sessions are told only to run the project's checks, not which commands gralph will run, so more sessions report `completed` and then fail a gate
- Editing the gates in the wizard rewrites the task file, which already loses comments and custom formatting on every save (ADR-004)

---

### Build order and future work for ADR-018 and ADR-019

ADR-019 goes first: the wizard's gates step needs the top-level list. Then `-d`, then the wizard. The breaking task-file change suggests v0.10.0; the owner picks the version at release.

A later change may fold the shared prompt into `tasks.yaml` and then deprecate `-t` and `-p`. The run folder survives it: `-d` would then name a folder holding `tasks.yaml` and `logs/`, and the wizard still starts by picking it. Nothing in either ADR depends on there being two files, and the wizard has no code that serves only `-t` or `-p`: they stay in this round as overrides, which is path resolution alone.

---

**Next:** [System Architecture](03_system_architecture.md)
