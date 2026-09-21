# Gralph

> **Maturity Level**: Emerging — under active development, expect breaking changes

---

Gralph drives "Ralph loops" against a PRD checklist using Claude Code. It runs
`claude --print --dangerously-skip-permissions` in a loop, advancing through
checklist items until all are complete.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)

## Usage

```bash
gralph --prompt path/to/PROMPT.md --prd path/to/PRD.md [--progress path/to/progress.txt]
```

| Flag        | Required | Default                     | Description                                                    |
| ----------- | -------- | --------------------------- | -------------------------------------------------------------- |
| `--prompt`  | Yes      | —                           | Path to the PROMPT.md template passed to Claude each iteration |
| `--prd`     | Yes      | —                           | Path to the PRD.md checklist that drives the loop              |
| `--progress` | No      | `<prd dir>/progress.txt`    | Path to the progress log file                                  |
| `--version` | No       | —                           | Print version information and exit                             |

### Example

```bash
gralph \
  --prompt scripts/PROMPT.md \
  --prd scripts/PRD.md
```

## How it works

Each iteration gralph finds the first unchecked item (`- [ ]`) in the PRD file
and invokes Claude exactly once with the PROMPT.md template appended with the
runtime paths of the PRD and progress files. After Claude exits, gralph
re-reads the PRD: if the item is gone or changed it is counted as complete and
the loop advances to the next item. If the item is unchanged, gralph prints a
failure message and exits with a non-zero status, leaving the PRD untouched so
the run can be resumed. The loop ends when no `- [ ]` items remain.

### Checklist markers

| Marker  | Meaning                                          |
| ------- | ------------------------------------------------ |
| `- [ ]` | Open — will be processed                         |
| `- [x]` | Complete — skipped                               |
| `- [~]` | Closed — skipped; gralph no longer writes it     |

### Log output

Gralph writes structured log lines to stdout and brackets Claude's raw output so each attempt is easier to scan:

```
[gralph] start
[gralph] attempt item="Cycle 1 - Short title"
[gralph] claude_output_begin item="Cycle 1 - Short title"
... raw claude output ...
[gralph] claude_output_end item="Cycle 1 - Short title" status=ok
[gralph] check item="Cycle 1 - Short title"
[gralph] completed item="Cycle 1 - Short title"
[gralph] done
```

If an item is unchanged after invocation:

```
[gralph] start
[gralph] attempt item="Cycle 1 - Short title"
[gralph] claude_output_begin item="Cycle 1 - Short title"
... raw claude output ...
[gralph] claude_output_end item="Cycle 1 - Short title" status=ok
[gralph] check item="Cycle 1 - Short title"
[gralph] failed item="Cycle 1 - Short title"
```

## Key Considerations

- **Runs outside Claude sessions**: gralph is a wrapper that launches Claude as a subprocess. Do not invoke it from within a running Claude session.
- **`claude` must be on PATH**: gralph calls `claude --print --dangerously-skip-permissions` directly; the Claude CLI must be installed and accessible.
- **Permission checks are bypassed**: `--dangerously-skip-permissions` allows gralph to run Claude without user interaction. Be mindful of the security implications when using gralph with untrusted prompts or PRDs.
- **PRD is never modified**: gralph does not write to the PRD file. An unchanged item causes gralph to exit with a non-zero status, leaving the PRD untouched for manual inspection or resumption.
- **One invocation per item**: each unchecked item gets exactly one Claude invocation. If the item is still unchecked afterward, the loop fails immediately with no retries.
- **Signal handling**: sending SIGINT (Ctrl-C) or SIGTERM to gralph cancels the current Claude invocation. On macOS and Linux, the entire Claude process group is terminated; on other platforms only the direct child process is guaranteed to be killed. The PRD is left untouched on cancellation.

## Development Considerations

### Quick Start

```bash
git clone https://github.com/twistingmercury/gralph.git
cd gralph
go build ./cmd/main
```

### Building

```bash
go build -o gralph ./cmd/main
```

To embed version metadata at build time:

```bash
go build \
  -ldflags "-X github.com/twistingmercury/gralph/internal/version.version=$(git describe --tags) \
            -X github.com/twistingmercury/gralph/internal/version.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
            -X github.com/twistingmercury/gralph/internal/version.gitCommit=$(git rev-parse --short HEAD)" \
  -o gralph ./cmd/main
```

### Testing

```bash
go test ./...
```

## Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). Current version: `v0.1.0`.

Version is determined from git tags:

```bash
git describe --tags --always
```
