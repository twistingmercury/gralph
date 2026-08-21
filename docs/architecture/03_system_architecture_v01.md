# Gralph System Architecture

> Version: v01
> Date: 2026-08-21
> Notes: Provider-neutral component and boundary model.

[Architecture Overview](00_overview_v01.md) | [Project README](../../README.md)

## Table of Contents

- [Components](#components)
- [Invocation flow](#invocation-flow)
- [Security boundary](#security-boundary)

## Components

| Component | Responsibility |
| --- | --- |
| `cmd/main` | Parse and validate flags, resolve optional profile, install signal context. |
| `internal/agent` | Validate command specification, run agent, capture outputs, terminate process tree. |
| `internal/looper` | Validate files, assemble prompt, retry, print output, and atomically abandon items. |
| `internal/version` | Render build metadata. |

## Invocation flow

```mermaid
sequenceDiagram
    participant CLI as cmd/main
    participant Loop as looper
    participant Run as agent runner
    participant A as configured agent
    CLI->>Loop: Config and cancellation context
    Loop->>Loop: validate prompt and PRD; ensure progress file
    Loop->>Run: assembled prompt
    Run->>A: exec(executable, args...) + stdin or {prompt}
    A-->>Run: separate stdout, stderr, exit status
    Run-->>Loop: capture plus classified outcome
    Loop->>Loop: complete, retry, abandon, or fail
```

## Security boundary

The agent command is intentionally a user-selected external process. Its
executable and each argument are passed directly to `exec.CommandContext`; no
shell expansion is available. That prevents command injection through prompt or
argument content but does not make the configured agent trusted. The agent may
read or change files allowed by its own permissions, including the PRD and
progress path that Gralph provides in the prompt.

On Unix, a configured process group allows cancellation to terminate agent
descendants. On Windows, direct-child termination is the documented limit.

**Next:** [Deployment Architecture](05_deployment_architecture_v01.md)
