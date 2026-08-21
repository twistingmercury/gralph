# Gralph Architectural Decisions

> Version: v01
> Date: 2026-08-21
> Notes: Active provider-neutral ADR set; supersedes the previous execution-specific ADRs.

[Architecture Overview](00_overview_v01.md) | [Project README](../../README.md)

## Table of Contents

- [Decision summary](#decision-summary)
- [ADR-007: Generic agent command](#adr-007-generic-agent-command)
- [ADR-008: Fatal versus retryable outcomes](#adr-008-fatal-versus-retryable-outcomes)

## Decision summary

| ADR | Decision | Status |
| --- | --- | --- |
| ADR-007 | Generic agent command and explicit prompt transport | Accepted |
| ADR-008 | Only started non-zero processes are retryable | Accepted |
| ADR-009 | Explicit, opt-in provider profiles | Accepted |

## ADR-007: Generic agent command

**Status:** Accepted

**Context:** Provider-specific command names and flags prevent use with other
agents and obscure the security policy applied to an external command.

**Decision:** `AgentCommand` contains `Executable`, `Args`, and `PromptMode`.
`stdin` sends the assembled prompt through standard input. `arg` replaces one
whole `{prompt}` argument. Gralph uses direct process execution, never a shell.

**Consequences:** Arbitrary compatible CLIs work without new core code. Users
must explicitly choose and review any agent-specific security-sensitive flags.

## ADR-008: Fatal versus retryable outcomes

**Status:** Accepted

**Context:** Retrying an invocation that did not start could mutate a PRD by
abandoning work that no agent attempted.

**Decision:** A process that starts and exits non-zero is retryable. Invalid
configuration, setup/I/O failure, process-start failure, and cancellation are
fatal. Fatal outcomes clean captured files and preserve the PRD byte-for-byte.

**Consequences:** Failure behavior is safe and diagnosable; an agent must use
meaningful exit statuses for retries to be useful.

## ADR-009: Explicit, opt-in provider profiles

**Status:** Accepted

**Context:** Convenient integrations must not weaken arbitrary command support
or hide provider-specific permission choices.

**Decision:** A small verified-profile registry may supply an ordinary validated
`AgentCommand`. Explicit command flags override a profile. Profiles contain no
permission-bypass option.

**Consequences:** Profiles remain transparent convenience defaults, while the
generic path stays complete.

**Next:** [System Architecture](03_system_architecture_v01.md)
