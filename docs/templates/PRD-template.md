# Product Requirements Document: [Project Title]

*Gralph processes cycles in this document from top to bottom. Checklist markers are significant: `- [ ]` (open), `- [x]` (complete), `- [~]` (abandoned). Each cycle must be small, independently verifiable, and assigned to exactly one agent.*

## Objective

<!--
This section states what the deliverable is — a clear, outcome-focused sentence.
It answers: what will be built or fixed when all cycles are complete?
-->

*Example: Implement a PostgreSQL schema migration system that tracks applied migrations and prevents duplicate execution.*

## Problem Statement

<!--
Explain why this work is needed. This is the context that makes the objective matter.
Include: current state, gap, consequence of inaction.
-->

*Example: The current database setup is manual and error-prone. New environments require scripted migrations, but the codebase has no tooling to track which migrations have run. This blocks reliable reproducible deployments and causes manual rework.*

## Success Criteria

<!--
Observable, testable outcomes. Each criterion should be verifiable after the work is complete.
Use concrete language: "tests pass", "command does X", "file Y contains Z", not vague claims like "system is robust".
-->

- *Database migration order is deterministic and verified by tests.*
- *Running migrations twice against the same database is idempotent.*
- *README and `--help` output explain migration format and CLI usage.*
- *All unit and integration tests pass under `go test ./...`.*

## Scope

### In scope

<!--
What gralph cycles will implement. These are the boundaries of the work.
Be specific about features or components that will be included.
-->

- *Core schema migration runner and idempotency checks.*
- *CLI flags for migration directory, database connection, and dry-run mode.*
- *Migrations stored as `.up.sql` and `.down.sql` files with numeric prefixes.*
- *Unit tests for parser, executor, and state tracking.*

### Out of scope

<!--
What to explicitly exclude. This prevents scope creep and sets expectations.
Name features or concerns that sound in-scope but are not.
-->

- *Automatic rollback or recovery after failed migrations.*
- *Multi-database or federated migration coordination.*
- *Web UI for migration management.*

## Constraints and Decisions

<!--
Non-negotiable technical choices. These are constraints Claude must respect.
Include: architectural decisions, tooling choices, compatibility requirements, performance targets.
-->

- *Use only Go standard library for SQL driver integration; do not add new database dependencies.*
- *Migration files are immutable once applied. Modifications require new migration files.*
- *Prefer idempotence through schema-checking SQL over undo logic.*
- *Keep migration execution deterministic and testable without live database in unit tests.*

## Implementation Plan

<!--
Each cycle must be small, independently verifiable, and assigned to one agent.
Cycles should be ordered so earlier ones unblock later work.
If a cycle takes more than one agent, split it into separate cycles.
-->

- [ ] **Cycle 1 - Parse migration file format and detect applied migrations**
  - Agent: `go-software-engineer`
  - Files: `internal/migration/parser.go`, `internal/migration/parser_test.go`
  - Steps:
    - Implement `ParseMigrationFiles` to discover `.up.sql` files in a directory and extract numeric prefixes.
    - Implement `ParseAppliedMigrations` to read the migrations table and extract applied version numbers.
    - Return errors on missing directories, non-numeric prefixes, or malformed migration files.
  - Verify: `go test ./internal/migration -run TestParse -v`
  - Done: Parser correctly identifies available and applied migrations; no false positives on file names.

- [ ] **Cycle 2 - Execute a single migration with idempotence checks**
  - Agent: `go-software-engineer` <!-- Valid agents: go-software-engineer, go-software-architect, devops-engineer, data-engineer, data-architect, api-architect, technical-writer, solutions-architect, bats-test-engineer, go-e2e-test-engineer -->
  - Files: `internal/migration/executor.go`, `internal/migration/executor_test.go`
  - Steps:
    - Implement `ExecuteMigration` to run a single `.up.sql` file against a database connection.
    - Check for existence of migrations tracking table before insert; idempotently create if missing.
    - Record applied version, timestamp, and file hash to prevent duplicate runs.
    - Propagate database errors as non-zero returns without crashing the caller.
  - Verify: `go test ./internal/migration -run TestExecute -v`
  - Done: Executing the same migration twice does not duplicate state; database errors are surfaced.

- [ ] **Cycle 3 - CLI entrypoint and flag parsing**
  - Files: `cmd/migration/main.go`, `cmd/migration/main.go` <!-- Files touched by this cycle only -->
  - Steps:
    - Define flags: `--db-url`, `--migration-dir`, `--dry-run`, `--version`.
    - Validate required flags and database connection string format.
    - Wire parser and executor to command entry point.
  - Verify: `go run ./cmd/migration --help`
  - Done: Help output lists all flags; invalid flags or missing database URL exit with clear error message.

- [ ] **Cycle 4 - Loop over pending migrations and report progress**
  - Files: `internal/migration/runner.go`, `internal/migration/runner_test.go`, `cmd/migration/main.go`
  - Steps:
    - Implement `Run` to iterate parsed migrations in order, skip applied ones, and execute pending ones.
    - Track and report per-migration outcome: skipped, executed, failed.
    - Respect `--dry-run` flag and report what would run without modifying the database.
  - Verify: `go test ./internal/migration -run TestRunner -v`
  - Done: Loop executes all pending migrations in order; dry-run mode changes nothing in the database.

- [ ] **Cycle 5 - README and final validation**
  - Agent: `technical-writer`
  - Files: `README.md`
  - Steps:
    - Document CLI usage with examples for common workflows: initialize database, apply migrations, dry-run.
    - Explain migration file naming convention and `.up.sql` / `.down.sql` pattern.
    - Document migration table schema and idempotence guarantees.
    - Verify usage examples match implemented flags.
  - Verify: `go test ./...`
  - Done: README covers all supported flags and patterns; build and tests pass.

## Risks and Mitigations

<!--
Flag the most common gralph failure mode: cycles that loop indefinitely because they never reach their Done condition.
This happens when Done is vague ("system works"), when verification is disconnected from Done,
or when the cycle is too large and gets stuck on unrelated problems.

Include worked examples of Risk / Mitigation pairs.
-->

- Risk: Database errors in migration execution cause the loop to abort instead of cleanly reporting failure.
  - Mitigation: Wrap executor in error handler that logs the failure and continues to next migration; exit non-zero only after all pending migrations are attempted.

- Risk: Dry-run mode is incomplete and loop cycles get stuck debugging whether a change actually happened.
  - Mitigation: Implement dry-run as a parsed-but-not-executed path early in Cycle 2; verify in unit tests that dry-run queries the database but does not write.

## Definition of Done

<!--
This is the global exit condition for the entire loop.
When all cycles above are checked AND all conditions below are true, gralph stops and declares success.

Make each condition concrete and verifiable — not a restatement of the cycle checklist.
Example of bad: "Cycles are complete" (not verifiable).
Example of good: "go test ./... passes and all binaries build" (observable command).
-->

- `go test ./...` passes with no skipped tests.
- Migration tool compiles and runs: `go build ./cmd/migration` exits 0.
- Manual test: initialize empty database, apply migrations in order, verify idempotence by running again.
- README usage section matches `--help` output exactly.
