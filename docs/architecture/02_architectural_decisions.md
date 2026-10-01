# Gralph — Architectural Decisions

> **Version**: v08
> **Date**: 2026-09-30
> **Notes**: Added ADR-014: sessions run in Claude Code's sandbox from a `--sandbox-settings` file; `--skip-permissions` is the explicit way to keep the old bypass.

[Back to Overview](00_overview.md) | [Back to Project README](../../README.md)

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

| ADR     | Title                                          | Status   | Date       |
| ------- | ---------------------------------------------- | -------- | ---------- |
| ADR-001 | Claude Code only, no agent abstraction         | Accepted | 2026-09-25 |
| ADR-002 | YAML task file + shared prompt, strict parsing | Accepted | 2026-09-25 |
| ADR-003 | One fresh session per task, no retry           | Accepted | 2026-09-25 |
| ADR-004 | Gralph owns task state, atomic writes          | Accepted | 2026-09-25 |
| ADR-005 | Outcome from JSON result line, not exit code   | Accepted | 2026-09-25 |
| ADR-006 | Failed task blocks run until manual reset      | Accepted | 2026-09-25 |
| ADR-007 | Unix only, process group lifecycle management  | Accepted | 2026-09-25 |
| ADR-008 | Docker-first CI: build, test, e2e in container | Accepted | 2026-09-25 |
| ADR-009 | Embed the skill, install with --install-skill  | Accepted | 2026-09-25 |
| ADR-010 | Refuse to run with a stale installed skill     | Accepted | 2026-09-25 |
| ADR-011 | Full-screen TUI by default, plain mode intact  | Accepted | 2026-09-25 |
| ADR-012 | Bubble Tea v2 for the TUI, confined to its use | Accepted | 2026-09-25 |
| ADR-013 | Gralph runs a task's gates after a session     | Accepted | 2026-09-30 |
| ADR-014 | Sandboxed sessions; bypass only on request     | Accepted | 2026-09-30 |

## Decisions

### ADR-001: Claude Code only, no agent abstraction

**Status:** Accepted

**Context:**

Early versions of gralph (tags v0.4.0–v0.5.2) attempted to build an agent-agnostic runtime, supporting Claude API, OpenAI, and other providers through a profile abstraction. This added complexity and required managing multiple provider integrations. The project was built specifically to drive Claude Code workflows.

**Decision:**

Gralph is Claude Code only. No agent abstraction layer, no provider profiles, no `--agent-*` flags. The CLI invokes `claude --print --dangerously-skip-permissions` directly, coupling the tool to Claude Code as a deliberate design choice.

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
- Skill changes ship only with a new binary; a stale install is not detected during normal runs
- Two install paths exist (flag for users, script for development)

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

There is one loop. `looper.Run` → `runLoop` takes a `report func(Event)` hook:

- `report == nil` (plain): `runTaskPlain` runs `claude --print --dangerously-skip-permissions`, echoes the combined prompt, tees claude's stdout, and inherits stderr, exactly as before.
- `report != nil` (TUI): `runTaskStream` runs `claude --print --output-format stream-json --verbose --dangerously-skip-permissions`, writes nothing to gralph's stdout or stderr, and reports `TaskStarted`, `Activity` (assistant text and tool calls, and claude's stderr lines), and `TaskFinished` events, then a final `RunDone`. The outcome comes from the `result` event's text with the same rules as plain mode.

Both paths send the same combined prompt on stdin, and the loop rules (skip completed, save after every task, stop on first failure, cancel leaves the task untouched) live once in `runLoop`. Stream-json is used only on the TUI path. There are no runner, storage, or writer interfaces: `runLoop` still execs claude inline, and tests still drive a fake `claude` on `PATH`. `in progress` is display only and never written to `tasks.yaml`.

**Consequences:**

_Positive:_

- A run is easy to follow live: what Claude is doing, which task is running, which are done
- Plain mode, its tests, and every script or CI job that uses gralph are unchanged
- One copy of the loop rules; the two task paths differ only in how they talk to claude
- The e2e suite needs no change: it has no terminal, so it always runs plain mode

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

The TUI uses Bubble Tea v2 only: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2` (viewport, textinput), and `charm.land/lipgloss/v2`. The v1 `github.com/charmbracelet/bubbletea`, `bubbles`, and `lipgloss` modules are never imported. These modules are imported only by `internal/tui` and `cmd/main`; `internal/looper` and `internal/tasks` never import them, so dependencies point one way: `cmd/main` → `internal/tui` → `internal/looper` → `internal/tasks`. The looper reaches the TUI only through the `report` hook, which `tui.Run` forwards to the program with `Send`. `cmd/main` uses `github.com/charmbracelet/x/term` for the terminal check. Bubble Tea's own signal handling is off (`tea.WithoutSignalHandler`), so the SIGINT/SIGTERM context from `cmd/main` stays the only outside stop.

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

- Gate commands are shell text from the task file, run without a sandbox; the file was already code to be trusted (it drives `--dangerously-skip-permissions`), and now gralph executes part of it directly
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

**Next:** [System Architecture](03_system_architecture.md)
