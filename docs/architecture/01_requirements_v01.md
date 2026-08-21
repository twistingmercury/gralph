# Gralph Requirements

> Version: v01
> Date: 2026-08-21
> Notes: Provider-neutral runtime requirements.

[Architecture Overview](00_overview_v01.md) | [Project README](../../README.md)

## Table of Contents

- [Problem statement](#problem-statement)
- [Goals](#goals)
- [Non-goals](#non-goals)
- [Compatibility contract](#compatibility-contract)
- [Constraints](#constraints)

## Problem statement

An AI-assisted PRD loop must work with compatible noninteractive agent CLIs
without embedding a provider executable, arguments, or permission policy.

## Goals

1. Accept an executable, ordered literal arguments, and `stdin` or `arg` prompt transport.
2. Preserve argument boundaries and never use a shell to invoke the agent.
3. Retry only started, non-zero agent processes; preserve the PRD on fatal failures.
4. Support SIGINT and SIGTERM cancellation, including Unix agent descendants.
5. Keep the CLI contract, output examples, and architecture documentation checked.

## Non-goals

- Selecting a provider implicitly.
- Adding permission-bypass arguments automatically.
- Parallel checklist processing or multiple PRDs per run.
- Guaranteeing Windows descendant-process termination before a Job Object strategy exists.

## Compatibility contract

| Requirement | Contract |
| --- | --- |
| Execution | One noninteractive command handles one loop attempt. |
| Prompt transport | `stdin`, or `arg` with exactly one whole `{prompt}` argument. |
| Exit status | Zero means normal completion; a started non-zero process is retryable. |
| Cancellation | The command must react to process termination; Gralph propagates cancellation. |
| Arguments | Values are literal argv elements; spaces and shell metacharacters are not interpreted. |

## Constraints

- Prompt and PRD paths must name readable regular files; symlinks are rejected.
- The configured executable and arguments are user-controlled trust-boundary data.
- PRD abandonment uses a temporary file and same-directory rename.
- Agent stdout and stderr are captured separately; their original interleaving is unavailable.

**Next:** [Architectural Decisions](02_architectural_decisions_v01.md)
