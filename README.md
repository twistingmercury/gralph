# Gralph

> **Maturity Level**: Emerging — under active development, expect breaking changes

---

Gralph drives "Ralph loops" against a PRD checklist using Claude Code. It runs
`claude --print --dangerously-skip-permissions` in a loop, advancing through
checklist items until all are complete or abandoned.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)

## Usage

```bash
gralph --prompt-md path/to/PROMPT.md --prd-md path/to/PRD.md [--progress-file path/to/progress.txt] [-i 10]
```

| Flag                  | Required | Default                     | Description                                                    |
| --------------------- | -------- | --------------------------- | -------------------------------------------------------------- |
| `--prompt-md`         | Yes      | —                           | Path to the PROMPT.md template passed to Claude each iteration |
| `--prd-md`            | Yes      | —                           | Path to the PRD.md checklist that drives the loop              |
| `--progress-file`     | No       | `<prd-md dir>/progress.txt` | Path to the progress log file                                  |
| `--iterations` / `-i` | No       | `10`                        | Maximum attempts per checklist item before it is abandoned     |
| `--version`           | No       | —                           | Print version information and exit                             |

### Example

```bash
gralph \
  --prompt-md scripts/PROMPT.md \
  --prd-md scripts/PRD.md \
  --iterations 5
```

## How it works

Each iteration gralph finds the first unchecked item (`- [ ]`) in the PRD file
and invokes Claude with the PROMPT.md template appended with the runtime paths
of the PRD and progress files. After Claude exits, gralph re-reads the PRD: if
the item is gone or changed it is counted as complete and the loop advances. If
the item is unchanged after `--iterations` attempts, gralph rewrites it as
`- [~]` (abandoned) and moves on. The loop ends when no `- [ ]` items remain.

### Checklist markers

| Marker  | Meaning                                               |
| ------- | ----------------------------------------------------- |
| `- [ ]` | Open — will be processed                              |
| `- [x]` | Complete — skipped                                    |
| `- [~]` | Abandoned — skipped after hitting the iteration limit |

### Log output

Gralph writes structured log lines to stdout:

```
[gralph] start max_attempts=<n>
[gralph] attempt item="..." attempt=<n> max=<n>
[gralph] invoke
[gralph] invoke_failed err=<error>
[gralph] check item="..."
[gralph] completed item="..."
[gralph] retry item="..." attempt=<n> max=<n>
[gralph] abandoned item="..."
[gralph] done
```

## Key Considerations

- **Runs outside Claude sessions**: gralph is a wrapper that launches Claude as a subprocess. Do not invoke it from within a running Claude session.
- **`claude` must be on PATH**: gralph calls `claude --print --dangerously-skip-permissions` directly; the Claude CLI must be installed and accessible.
- **PRD mutation is destructive**: gralph overwrites the PRD file in place when abandoning items (`- [~]`). Keep a copy or use version control.
- **Non-zero Claude exits are retried**: a failed Claude invocation counts as one attempt and does not crash the loop.

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
