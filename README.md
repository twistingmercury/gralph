# Gralph

> **Maturity Level**: Basic - ready for real work; a breaking change can still land before 1.0, and sandboxed runs are tested on Linux only  
> **Version**: v0.10.0
>
> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

Gralph runs "Ralph loops" with Claude Code. You give it a list of tasks in a
YAML file and one shared prompt. It works through the list one task at a time,
starting a fresh `claude --print` session for each, so every task gets a clean
context instead of one long session that drifts.

## Table of Contents

- [Install](#install)
- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [License](#license)

## Install

You don't need Go. Download the archive for your machine from the
[releases page](https://github.com/twistingmercury/gralph/releases) and unpack
`gralph` into `~/.local/bin`. Set `VERSION` to the release you want, `OS` to
`linux` or `darwin`, and `ARCH` to `amd64` or `arm64`:

```bash
VERSION=v0.10.0
OS=linux
ARCH=amd64

curl -fsSLO "https://github.com/twistingmercury/gralph/releases/download/${VERSION}/gralph_${VERSION}_${OS}_${ARCH}.tar.gz"
mkdir -p ~/.local/bin
tar -xzf "gralph_${VERSION}_${OS}_${ARCH}.tar.gz" -C ~/.local/bin gralph
```

The archive also holds the full guide, `howto.md`. Checking the download,
fixing your `PATH`, macOS notes, and upgrading are in
[Installing gralph](docs/howto.md#installing-gralph).

## Usage

```bash
gralph --install-skill   # once: installs the gralph-docs-writer skill for Claude Code
gralph -t path/to/tasks.yaml --dry-run
gralph --prompt path/to/prompt.md --tasks path/to/tasks.yaml --sandbox-settings path/to/sandbox.json
gralph -d path/to/folder --sandbox-settings path/to/sandbox.json
```

Run it in a terminal and you get a full-screen view of the run. Pipe it,
redirect it, run it in CI, or pass `--no-tui`, and you get plain text output
instead.

Every real run needs either `--sandbox-settings` or `--skip-permissions`. There
is no default.

In a terminal, a bare `gralph` opens a setup wizard that asks for whatever the
flags leave out (the run folder, the permission choice, the gates), then shows
the command line that starts the same run without it.

**The full guide is [docs/howto.md](docs/howto.md)**: the task file, sandbox
settings, gates, `--commit`, `--log-dir`, the full-screen view and its setup
wizard, and every flag.

Got a question or an idea? Ask in
[Discussions](https://github.com/twistingmercury/gralph/discussions).

## How it works

For each task, gralph glues the shared prompt and the task together and hands
the result to a new `claude --print` session on stdin. Tasks already marked
`completed` are skipped.

Gralph decides how a task went from the **last non-blank line** of Claude's
output, which should be a JSON line like `{"state": "completed", "error": ""}`.
A task that reports `completed` and exits zero then has its gates run (commands
from the task file that gralph runs itself) and, with `--commit`, is committed.
Anything else marks the task `failed` and stops the run. Gralph saves the task
file after every task.

The details are in [How a run works](docs/howto.md#how-a-run-works).

Want the background? The idea behind gralph is in
[.archive/gralph-concept.md](.archive/gralph-concept.md), and the design starts at
[docs/architecture/00_overview.md](docs/architecture/00_overview.md).

## Key Considerations

- **Don't run gralph from inside a Claude session.** It starts Claude itself, as
  a subprocess, so run it from a normal terminal.
- **You need the `claude` CLI on your `PATH`.** Gralph calls it directly.
- **Only run task files you trust.** The prompt and task files are code you're
  about to run. Gate commands and, with `--commit`, git and the repository's
  hooks are run by gralph itself, with no sandbox.
- **A failed task stops everything until you deal with it.** There's no retry.
  Fix whatever went wrong, set that task's `state` back to `pending` (or
  `completed`) by hand, and run again.
- **Built for Unix.** Gralph runs on Linux, macOS, and the BSDs. There's no
  Windows release. You can build one yourself by uncommenting the
  `GOOS=windows` line in [build/Dockerfile](build/Dockerfile), but the Windows
  version itself hasn't been directly exercised: treat it as untested (see the
  HOWTO for the known issues).

More on each of these in
[Before you start](docs/howto.md#before-you-start) and
[When things go wrong](docs/howto.md#when-things-go-wrong).

## Development Considerations

### Prerequisites

| Tool                                                 | Needed for                                            |
| ---------------------------------------------------- | ----------------------------------------------------- |
| Go 1.27.1 or later                                   | `make local`, `make test`, `make analyze`             |
| make                                                 | Every target                                          |
| git                                                  | Cloning; the version stamped into the binary          |
| Docker, with the `docker compose` plugin             | `make build` (the release build and the e2e suite)    |
| `goimports`, `golangci-lint`, `govulncheck`, `gosec` | `make analyze`, each on your `PATH`                   |

You don't need the lint tools or Docker for a quick local build. And `make
build` doesn't need the lint tools on your machine: they're in the image it
builds with.

### Quick Start

```bash
git clone https://github.com/twistingmercury/gralph.git
cd gralph
make local install   # builds .bin/local/gralph, then copies it to ~/go/bin
gralph --version
```

`make help` lists every target.

`make build` is the release build, and it's exactly what CI runs
([build/build.sh](build/build.sh) is the only thing the CI workflow executes).
It needs Docker and the `ghcr.io/twistingmercury/golang-tooling:go1.27.1`
image. It runs the linters, security scanners, and unit tests inside the image,
cross-compiles into `.bin/<arch>/<os>/`, then runs the e2e suite in a
container. If it passes locally, CI should pass too.

Releases are made by hand: after a version tag is pushed, the owner starts the
`Release` workflow with that tag. It runs the same `make build`, packs the
binaries with [build/package.sh](build/package.sh), and leaves a draft release
to publish.

### Testing

```bash
make test         # unit tests: go test ./cmd/... ./internal/... (run from src/)
make analyze      # goimports, golangci-lint, govulncheck, gosec (tools must be on PATH)
bats tests/bats   # tests for build/package.sh (needs bats; not run by CI)
```

One gotcha: the Go module is in `src/`. `go test ./...` from the repository root
finds no module; use `make test` instead, or run go commands from inside `src/`.
`tests/e2e` is its own Go module at the repository root, so `go test ./...` from
`src/` never includes it. Use `make build` to run the full e2e suite. The e2e
suite uses a fake `claude` on `PATH`, so you don't need the real Claude CLI or a
network connection.

### Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/).

Version is determined from git tags and embedded at build time:

```bash
git describe --tags --always
```

## License

[MIT](LICENSE)
