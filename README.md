# Gralph

> **Maturity Level**: Emerging - under active development; the CLI contract has already changed between minor versions
> **Version**: v0.6.0

> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

Gralph drives "Ralph loops" against a PRD checklist using Claude Code. It runs
`claude --print --dangerously-skip-permissions` once per open checklist item,
advancing until every item is complete or one is left unfinished.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)
- [License](#license)

## Usage

```bash
gralph --prompt path/to/PROMPT.md --prd path/to/PRD.md [--progress path/to/progress.txt]
```

| Flag         | Required | Default                  | Description                                                    |
| ------------ | -------- | ------------------------ | -------------------------------------------------------------- |
| `--prompt`   | Yes      | —                        | Path to the PROMPT.md template passed to Claude each iteration |
| `--prd`      | Yes      | —                        | Path to the PRD.md checklist that drives the loop              |
| `--progress` | No       | `<prd dir>/progress.txt` | Path to the progress log file                                  |
| `--version`  | No       | —                        | Print version information and exit                             |

```bash
gralph \
  --prompt scripts/PROMPT.md \
  --prd scripts/PRD.md
```

Start a new checklist from [docs/templates/PRD-template.md](docs/templates/PRD-template.md);
[scripts/PROMPT.md](scripts/PROMPT.md) is a working prompt example.

## How it works

Each iteration gralph finds the first unchecked item (`- [ ]`) in the PRD file
and invokes Claude exactly once with the PROMPT.md template appended with the
runtime paths of the PRD and progress files. After Claude exits, gralph
re-reads the PRD: if the item is gone or changed it is counted as complete and
the loop advances to the next item, even when Claude exited non-zero. If the
item is unchanged, gralph prints a failure line and exits with a non-zero
status, leaving the PRD untouched so the run can be resumed. The loop ends when
no `- [ ]` items remain.

| Marker  | Meaning                                      |
| ------- | -------------------------------------------- |
| `- [ ]` | Open — will be processed                     |
| `- [x]` | Complete — skipped                           |
| `- [~]` | Closed — skipped; gralph no longer writes it |

Gralph writes structured log lines to stdout and brackets Claude's raw output:

```text
[gralph] start
[gralph] attempt item="Cycle 1 - Short title"
[gralph] claude_output_begin item="Cycle 1 - Short title"
... raw claude output ...
[gralph] claude_output_end item="Cycle 1 - Short title" status=ok
[gralph] check item="Cycle 1 - Short title"
[gralph] completed item="Cycle 1 - Short title"
[gralph] done
```

When the item is unchanged, the last two lines are replaced by
`[gralph] failed item="Cycle 1 - Short title"` and gralph exits non-zero.

Background on the approach is in [docs/gralph-concept.md](docs/gralph-concept.md);
the design is documented from [docs/architecture/00-overview.md](docs/architecture/00-overview.md).

## Key Considerations

- **Runs outside Claude sessions**: gralph launches Claude as a subprocess. Do not invoke it from within a running Claude session.
- **`claude` must be on PATH**: gralph calls `claude --print --dangerously-skip-permissions` directly; the Claude CLI must be installed and accessible.
- **Permission checks are bypassed**: `--dangerously-skip-permissions` lets Claude run without user interaction. Treat prompts and PRDs as code you are choosing to execute, and do not run gralph against untrusted ones.
- **PRD is never modified**: gralph does not write to the PRD file; Claude does. An unchanged item ends the run non-zero and leaves the PRD untouched for inspection or resumption.
- **One invocation per item**: each unchecked item gets exactly one Claude invocation. There are no retries.
- **Signal handling**: SIGINT (Ctrl-C) or SIGTERM cancels the current Claude invocation and leaves the PRD untouched. On macOS and Linux the entire Claude process group is terminated; on other platforms only the direct child process is guaranteed to be killed.

## Development Considerations

### Quick Start

Requires Go 1.27.1 or later.

```bash
git clone https://github.com/twistingmercury/gralph.git
cd gralph
make local install   # builds .bin/local/gralph, then copies it to ~/go/bin
gralph --version
```

`make install` only copies the last local build, so run it together with
`make local`. Run `make help` to list every target.

`make build` is the Docker-based release build that CI runs
([build/build.sh](build/build.sh) is the only thing the CI workflow executes).
It needs Docker and the `ghcr.io/twistingmercury/golang-tooling:go1.27.1`
image, runs the linters, security scanners, and unit tests inside the image,
cross-compiles into `.bin/<arch>/<os>/`, then runs the e2e suite in a container.

### Testing

```bash
make test      # unit tests: go test -v ./internal/...
make e2e       # builds the local binary, then runs the black-box suite against it
make analyze   # goimports, golangci-lint, govulncheck, gosec (tools must be on PATH)
```

`tests/e2e` is a separate Go module, so `go test ./...` from the repository root
never runs it. The e2e suite puts a fake `claude` executable on `PATH`; no real
Claude CLI or network access is needed.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags and embedded at build time:

```bash
git describe --tags --always
```

## License

[MIT](LICENSE)
