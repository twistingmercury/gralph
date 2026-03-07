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

- [ ] **Cycle 2 - <short title>**: <one-sentence description of what this cycle delivers>.
  - Agent: <!-- one of: go-software-engineer, go-software-architect, devops-engineer, data-engineer, data-architect, api-architect, technical-writer, solutions-architect, bats-test-engineer, go-e2e-test-engineer -->
  - Files: <!-- list only files this cycle touches -->
  - Steps:
    - <!-- atomic, grep-friendly action -->
    - <!-- atomic, grep-friendly action -->
  - Verify: <!-- runnable command, e.g. `go test ./internal/parser -v` — not a description -->
  - Done: <!-- observable exit condition, not a restatement of steps -->

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
