# PRD Template Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create `docs/templates/PRD-template.md` — an opinionated, annotated gralph PRD template with inline examples that authors copy and fill in.

**Architecture:** Single Markdown file. Each section carries an HTML comment explaining intent and an inline italicized or blockquoted example. Two example cycles: one fully worked, one bare scaffold. No tooling or automation.

**Tech Stack:** Markdown only.

**Design doc:** `docs/plans/2026-03-06-prd-template-design.md`
**Reference PRD:** `scripts/PRD.md`

---

### Task 1: Create docs/templates/PRD-template.md

**Files:**
- Create: `docs/templates/PRD-template.md`

**Step 1: Verify the templates directory does not yet exist**

Run:
```bash
ls docs/templates 2>&1
```
Expected: `No such file or directory` (directory will be created with the file).

**Step 2: Create the template file**

Create `docs/templates/PRD-template.md` with the following exact content:

````markdown
# Product Requirements Document: <!-- Project name here -->

<!--
  This file drives a gralph loop. gralph reads it top-to-bottom and processes
  cycles in order. Checklist markers have specific meaning:

    - [ ]  Open — gralph will process this cycle
    - [x]  Complete — gralph skips this cycle
    - [~]  Abandoned — gralph gave up after hitting the iteration limit

  Do not reorder cycles while a loop is running.
-->

## Objective

<!--
  One sentence: what artifact or capability does this project deliver?
  Focus on the deliverable, not the process.
-->

> *Example: Implement a Go CLI that watches a directory and posts new files to a webhook.*

## Problem Statement

<!--
  2-3 sentences explaining why this work is needed and what is currently broken
  or missing. This context helps Claude understand intent across all cycles.
-->

> *Example: The team manually uploads build artifacts after each release. This is
> error-prone and blocks downstream consumers. An automated uploader will remove
> the manual step and provide an audit log.*

## Success Criteria

<!--
  Concrete, testable outcomes. Each item should be verifiable with a command or
  an observable behavior — not a description of effort.
-->

- *Example: `go test ./...` passes with no skipped tests.*
- *Example: Running the CLI with `--dry-run` prints the files it would upload without making network calls.*
- *Example: A missing required flag exits non-zero and prints a usage hint.*

## Scope

### In scope

<!--
  List what gralph cycles will implement. Be specific enough that Claude can
  tell when something is in scope vs. not.
-->

- *Example: CLI flag parsing and validation*
- *Example: Directory watcher using `fsnotify`*
- *Example: HTTP POST with retry on transient errors*

### Out of scope

<!--
  Explicitly call out what will NOT be built. This prevents Claude from
  gold-plating cycles with unrequested features.
-->

- *Example: Authentication beyond a static API key flag*
- *Example: Parallel uploads*
- *Example: Windows support*

## Constraints and Decisions

<!--
  Non-negotiable technical choices that Claude must respect across every cycle.
  List language, libraries, patterns, and any architectural guardrails here.
-->

- *Example: Use Go standard library for HTTP — no third-party HTTP clients.*
- *Example: Use `pflag` for flag parsing, not `cobra`.*
- *Example: All file I/O must go through a seam (interface or function type) so it can be stubbed in tests.*

## Implementation Plan

<!--
  Each cycle must be:
    - Small: one logical unit of work completable in a single Claude session.
    - Independently verifiable: the Verify command must be runnable after this cycle alone.
    - Assigned to exactly one agent (see the delegation table in CLAUDE.md).

  Cycle order matters — later cycles may depend on earlier ones.
  Keep cycles focused; resist bundling unrelated changes.
-->

- [ ] **Cycle 1 - Scaffold CLI entrypoint and flag definitions**: Set up `cmd/main/main.go` with all flags parsed and validated.
  - Agent: `go-software-engineer`
  - Files: `cmd/main/main.go`
  - Steps:
    - Define flags: `--watch-dir`, `--webhook-url`, `--api-key`, `--dry-run`, `--version`.
    - Implement required-flag validation; exit non-zero with usage hint when missing.
    - Add `--version` as an immediate-exit path.
  - Verify: `go build ./cmd/main && ./main --help`
  - Done: `--help` lists all flags with descriptions; missing required flag exits 1.

<!--
  Scaffold for additional cycles — copy and fill in as needed.
  Delete this comment block when done.

- [ ] **Cycle N - <short title>**: <one-sentence description of what this cycle delivers>.
  - Agent: <!-- one of: go-software-engineer, go-software-architect, devops-engineer,
                        data-engineer, data-architect, api-architect, technical-writer,
                        solutions-architect, bats-test-engineer, go-e2e-test-engineer -->
  - Files: <!-- list only files this cycle touches -->
  - Steps:
    - <!-- atomic action -->
    - <!-- atomic action -->
  - Verify: <!-- a runnable command, e.g. `go test ./internal/watcher -v` — not a description -->
  - Done: <!-- observable exit condition, e.g. "watcher emits an event for each new file" -->
-->

## Risks and Mitigations

<!--
  Flag anything that could cause a gralph cycle to loop indefinitely. The most
  common failure mode is a cycle whose "Done" condition is ambiguous or whose
  Verify command always passes regardless of implementation.
-->

- Risk: Claude may implement the watcher but not write a test, causing Cycle 2 to pass vacuously.
  - Mitigation: Verify command for Cycle 2 runs `go test ./internal/watcher -v` and requires at least one test function.

- Risk: Webhook URL is user-supplied; a bad URL will cause all invocations to fail silently.
  - Mitigation: Cycle 1 validates the URL format before the loop starts; any error exits non-zero.

## Definition of Done

<!--
  The global exit condition for the entire loop. When gralph sees no remaining
  `- [ ]` items, it stops. These statements describe what "done" looks like
  from the outside — not the state of the code.
-->

- `go test ./...` passes with no skipped tests.
- `go build ./cmd/main` exits 0 and produces a working binary.
- Running `./main --watch-dir /tmp --webhook-url http://example.com --api-key x --dry-run` prints files without making network calls.
- README usage section matches the implemented flags exactly.
````

**Step 3: Verify the file renders correctly**

Run:
```bash
cat docs/templates/PRD-template.md | head -20
```
Expected: first 20 lines show the title and opening comment.

**Step 4: Commit**

```bash
git add docs/templates/PRD-template.md
git commit -m "docs: add gralph PRD template with guided scaffold and examples"
```

---
