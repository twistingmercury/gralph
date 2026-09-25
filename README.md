# Gralph

> **Maturity Level**: Emerging - under active development; the CLI contract has already changed between minor versions
> **Version**: v0.6.2

> - **Emerging**: Prototype, not production-ready, expect breaking changes
> - **Basic**: Production-ready but actively evolving, expect minor version changes
> - **Mature**: Stable, battle-tested, changes are rare

---

Gralph drives "Ralph loops" with Claude Code. It reads an ordered task list
from a YAML file and runs `claude --print --dangerously-skip-permissions` once
per task, feeding each session a shared prompt plus that task's own prompt.

## Table of Contents

- [Usage](#usage)
- [How it works](#how-it-works)
- [Key Considerations](#key-considerations)
- [Development Considerations](#development-considerations)
- [Versioning](#versioning)
- [License](#license)

## Usage

```bash
gralph --prompt path/to/prompt.md --tasks path/to/tasks.yaml
```

| Flag               | Required | Description                                            |
| ------------------ | -------- | ------------------------------------------------------ |
| `--prompt` / `-p`  | Yes, unless `--dry-run` | Path to the shared prompt sent to Claude for every task |
| `--tasks` / `-t`   | Yes      | Path to the YAML task list that drives the loop        |
| `--dry-run`        | No       | Validate the task file and report on it without running anything |
| `--install-skill`  | No       | Install the bundled `gralph-docs-writer` skill for Claude Code and exit |
| `--version` / `-v` | No       | Print version information and exit                     |

`gralph -t tasks.yaml --dry-run` checks the task file with the same rules a
real run uses, without launching Claude or writing any file. `--prompt` is
ignored. An invalid file prints the error to stderr and exits non-zero. A valid
file prints a table of each task's id, state, and name, and exits zero. If any
task is `failed` (which a real run would refuse), the table follows `Some tasks
failed previous runs:` and each failed row ends in `← Needs review!`; otherwise
the table is followed by `<tasks path> is valid`.

A task file is a `tasks` sequence. Each task has a unique positive integer `id`,
a `name` (unique, ignoring case and surrounding whitespace), a `prompt`, and an
optional `state`:

```yaml
tasks:
  - id: 1
    name: Add the widget repository
    prompt: |
      Objective:
      Add a Postgres-backed WidgetRepository with Create and GetByID.

      Verification:
      - Command: go test ./internal/widget/...
  - id: 2
    name: Expose GET /widgets/{id}
    state: pending
    prompt: |
      Add the HTTP handler using the repository from task 1.
```

`state` is `pending`, `completed`, or `failed`; leave it out for new work and
it is read as `pending`. An optional `error` field may be present when a task
has failed; gralph writes it, never the session. Gralph runs tasks in file
order — `id` identifies a task, it does not order them. The
`gralph-docs-writer` skill in [skills/](skills/gralph-docs-writer/SKILL.md)
generates a task file and shared prompt for a project. It is embedded in the
binary: `gralph --install-skill` replaces `~/.claude/skills/gralph-docs-writer/`
with the copy matching that binary's version and prints the path.
`scripts/install_skill.sh` installs the working-tree copy instead, for
development.

## How it works

For each task, gralph combines the shared prompt with the task, prints the
result to stdout, and starts a new `claude --print` session with it on stdin.
The text Claude receives is:

```text
<shared prompt>

<id>: <name>

<task prompt>
```

Claude's own output passes straight through to gralph's stdout and stderr. Gralph
skips tasks with `state: completed` (printing `task <id>: <name> already
completed, skipping`). For pending tasks, gralph reads the last
non-blank line of Claude's output (skipping lines that are only code fences).
If that line is valid JSON with `state: "completed"` and the session exits zero,
gralph marks the task `completed` and writes the file. Otherwise, gralph marks
the task `failed`, writes the file, reports `task <id>: <name> failed: <error>`
(from the JSON `error` field or the exit code if missing), and exits non-zero;
later tasks do not run. When every task succeeds, gralph exits zero and returns.

The task file is rewritten atomically (write to temporary file, rename over
original) with every state change. Comments and custom formatting are not
preserved; each task's state is made explicit.

Background on the approach is in [docs/gralph-concept.md](docs/gralph-concept.md);
the design is documented from [docs/architecture/00_overview.md](docs/architecture/00_overview.md).

## Key Considerations

- **Runs outside Claude sessions**: gralph launches Claude as a subprocess. Do not invoke it from within a running Claude session.
- **`claude` must be on PATH**: gralph calls `claude --print --dangerously-skip-permissions` directly; the Claude CLI must be installed and accessible.
- **Permission checks are bypassed**: `--dangerously-skip-permissions` lets Claude run without user interaction. Treat the prompt and task files as code you are choosing to execute, and do not run gralph against untrusted ones.
- **Each session starts cold**: a session sees only the shared prompt and its one task. Anything it needs to know about earlier tasks must be in those two texts or in the repository.
- **Completed tasks are skipped**: gralph runs only pending tasks.
- **Failed tasks block the run**: gralph refuses to start while any task is `failed`, printing the same task summary table as `--dry-run` and exiting 1, launching no session and leaving the file untouched. Fix the cause, then set the task's `state` to `pending` (or `completed`) by hand before running again.
- **Signal handling**: SIGINT (Ctrl-C) or SIGTERM cancels the current Claude session and stops the run. The entire Claude process group is terminated, so nothing Claude spawned outlives gralph.
- **Unix only**: gralph runs on Linux, macOS, and the BSDs. Windows is not supported.

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
