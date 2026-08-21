# Gralph Deployment Architecture

> Version: v01
> Date: 2026-08-21
> Notes: Provider-neutral runtime and documentation verification.

[Architecture Overview](00_overview_v01.md) | [Project README](../../README.md)

## Table of Contents

- [Build and distribution](#build-and-distribution)
- [Runtime requirements](#runtime-requirements)
- [Documentation gate](#documentation-gate)

## Build and distribution

Gralph is a statically compiled CLI. `make local` produces a development binary
in `.bin/local/gralph`; `make build` is the Docker-based release build and
produces the supported platform artifacts. Deployment consists of placing the
appropriate binary on `PATH` and supplying a compatible agent command at run
time.

## Runtime requirements

| Requirement | Detail |
| --- | --- |
| Agent | Compatible noninteractive executable reachable by configured path or name. |
| Files | Readable regular prompt and PRD files; a writable PRD directory for abandon mutations. |
| Signals | SIGINT/SIGTERM supported; Unix descendants are terminated as a process group. |
| Permissions | Agent-specific elevated permissions, if any, are explicit caller-owned arguments. |

## Documentation gate

`make docs-check` first builds the local binary. It normalizes only the binary
path in `--help`, compares the result with `docs/cli-help.txt`, and verifies
that every local Markdown file link resolves. This keeps the user-facing flag
contract and document navigation in sync with the binary.

**Next:** [Architecture Overview](00_overview_v01.md)
