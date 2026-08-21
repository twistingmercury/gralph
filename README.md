# Gralph

> **Maturity Level**: Emerging — under active development; breaking changes are possible.

Gralph runs a Ralph loop against a Markdown PRD checklist. You configure the
noninteractive AI-agent command that handles each attempt; Gralph supplies the
assembled prompt, tracks retries, and updates the checklist only when an item
is abandoned.

## Quick start

Build a local binary and run it with an arbitrary compatible agent:

```bash
make local

./.bin/local/gralph \
  --prompt scripts/PROMPT.md \
  --prd scripts/PRD.md \
  --agent-exec my-agent \
  --agent-arg=--non-interactive \
  --prompt-mode stdin \
  --iterations 5
```

For an agent that accepts its prompt as an argument, include exactly one whole
`{prompt}` argument:

```bash
gralph \
  --prompt scripts/PROMPT.md \
  --prd scripts/PRD.md \
  --agent-exec my-agent \
  --agent-arg=run \
  --agent-arg='{prompt}' \
  --prompt-mode arg
```

The verified optional profiles are convenience configurations, not a default
provider choice. The generic examples above remain the provider-independent
path. Explicit `--agent-exec`, `--agent-arg`, and `--prompt-mode` values
override a profile.

### Codex profile

```bash
gralph \
  --prompt scripts/PROMPT.md \
  --prd scripts/PRD.md \
  --agent-profile codex
```

### Claude Code profile

```bash
gralph \
  --prompt scripts/PROMPT.md \
  --prd scripts/PRD.md \
  --agent-profile claude-code
```

Neither profile includes a permission-bypass option. The `claude-code` profile
uses Claude Code’s documented noninteractive `--print` mode with the assembled
prompt as one argument. Add an explicit `--agent-arg` only if you choose an
agent-specific permission policy.

## CLI contract

Required inputs are `--prompt`, `--prd`, and either `--agent-exec` or an agent
profile. `--agent-arg` may be repeated; every value remains one literal
argument, with no shell parsing or interpolation. In `stdin` mode, `{prompt}`
is forbidden. In `arg` mode, exactly one argument must be `{prompt}`.

See the captured, checked flag reference in [docs/cli-help.txt](docs/cli-help.txt).

An agent is compatible when it can run noninteractively for one attempt, accept
the selected prompt transport, return a meaningful exit status, and terminate
when Gralph cancels it. Agent-specific permission or sandbox-bypass options are
never implied; if needed, they must be explicit `--agent-arg` values that you
choose and can review.

## Loop behavior

Gralph finds the first open `- [ ]` item, appends the PRD and progress paths to
the prompt, then invokes the configured agent. If the item changes or is gone,
it proceeds. A process that starts and exits non-zero is retried up to
`--iterations`; an unchanged item then becomes `- [~]` through an atomic PRD
rewrite. Configuration, input, process-start, setup, and cancellation failures
are fatal and leave the PRD unchanged.

Prompt and PRD inputs must be readable regular files; symbolic links are
rejected. The progress file defaults to `progress.txt` next to the PRD.

During a completed or final failed attempt, Gralph prints captured agent stdout
followed by stderr. Their original interleaving cannot be reconstructed.

```text
Cycle 1: Implement feature
- Agent: codex
- Status: Running ... (1/3)
- Status: Complete
- Agent Output:
updated the PRD
```

SIGINT and SIGTERM stop an in-flight agent. On Unix, Gralph terminates its
agent process group, including descendants. Windows currently guarantees
termination only for the direct child process.

## Development

```bash
go test ./...
make e2e
make docs-check
make verify
make build
```

`make docs-check` builds the local binary, compares its `--help` output to the
captured reference, and validates local Markdown links. `make verify` is the
provider-agnostic release acceptance gate; `make build` is the Docker-based
release build.

Architecture contracts are in [docs/architecture/00_overview_v01.md](docs/architecture/00_overview_v01.md).

## Versioning

Gralph follows [Semantic Versioning 2.0.0](https://semver.org/). Build metadata
comes from Git tags, with `git describe --tags --always` as the local source.
