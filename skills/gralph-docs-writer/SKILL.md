---
name: gralph-docs-writer
description: >-
  Use when creating or updating the tasks.yaml task list and shared prompt.md
  that Gralph feeds to Claude Code, or when breaking a build plan or PRD into
  Gralph loop tasks.
---

# Ralph Loop Docs

Generate the two files a Gralph run needs: `tasks.yaml`, an ordered task list,
and `prompt.md`, the shared prompt. Respect the user's destination, project
scope, verification, and commit policies.

## How Gralph uses the pair

Gralph works through `tasks.yaml` in file order. It skips `completed` tasks; for
each `pending` task it combines the shared prompt with that task's
`prompt` and hands the result to a new Claude Code session. Every session starts
with no memory of earlier tasks, so the pair must carry everything the session
needs.

Each task gets one session. There is no retry and no `--iterations` flag, so
scope every task to what a single session can finish and verify.

Gralph owns `state` and writes it back to `tasks.yaml`. The session never edits
the file. To remove a task without running it, delete it from the file; there is
no `abandoned` state. Gralph decides the outcome from the JSON result line the
shared prompt asks for at the end of the session's output. A missing or invalid
result line, or a non-zero exit, marks the task `failed`. Gralph refuses to run
while any task is `failed`; a person fixes the cause (often by editing the
task's `prompt`) and resets its `state` to `pending` by hand.

A `tasks.yaml` that does not match the rules below is invalid and Gralph
refuses to run it. There is no migration from other formats or older states.

## Published-document preservation

Inspect Git history and remote-tracking refs before editing generated
supporting documents. Preserve published standalone documents by creating the
next snake_case version with synchronized `Version`, `Date`, and `Notes`
metadata. Treat committed documents as published when publication is uncertain.
`tasks.yaml` and `prompt.md` are canonical living workflow files; update them
in place when authorized.

## Generate the pair

1. Read repository instructions and relevant design/build documents. Establish
   scope, output destination, actual document paths, and project verification
   and Git policies. Do not add automatic commits where none are required.
2. Read [the YAML task template](templates/tasks_template.yaml) and
   [the prompt template](templates/prompt_template.md).
3. Interview the user about quality gates before drafting tasks. Propose the
   gates the repository already has (tests, linters, formatters, security
   scanners, builds, end-to-end suites) as runnable commands, then ask which
   ones every task must pass and what to add. Ask one question at a time.
   Every task's Verification lists the agreed gates, plus its own
   task-specific checks.
4. Draft the task list and show the user a summary, one line per task:
   `<id>: <name> - <one short sentence>`. Nothing else goes in the summary.
   Ask whether they approve it or want changes. Revise and show the summary
   again until they approve. Write neither file before approval.
5. Write `tasks.yaml` with a top-level `tasks` sequence holding at least one
   task. Each task has a stable positive integer `id` (at most 32767), a
   nonblank `name`, a nonblank block-scalar `prompt`, and an optional `state`.
   IDs are unique and independent of order; Gralph runs tasks in file order.
   Names are unique ignoring case and surrounding whitespace. `state` is
   `pending`, `completed`, or `failed`; leave it empty for new work, which
   Gralph reads as `pending`. The optional `error` field is gralph-only: never
   write it; preserve it if present when updating an existing file. Write no
   other keys: Gralph ignores them when reading and drops them the first time
   it saves the file.
6. Put scope, steps, the agreed quality gates, task-specific verification, and
   completion criteria inside each task's prompt. Preserve project-specific
   instructions. YAML comments are not durable task instructions: they never
   reach the session. Gralph rewrites the task file with every state change,
   so comments and custom formatting are not preserved.
7. Write `prompt.md` from the prompt template. Replace every `GENERATE_*` token
   with project content and actual paths: full build/test commands and the
   project's commit policy, since the session sees only this prompt and one
   task. Keep commits conditional on the project's authorization. Keep the
   Rules and Finish sections as written so the pair works without this skill
   installed. Apart from the quality gates, keep generic rules out of task
   prompts; they live once, in `prompt.md`.
8. Validate `tasks.yaml` with `gralph -t <tasks.yaml> --dry-run`: it applies
   the same rules a real run does and exits non-zero on an invalid file. If
   `gralph` is not installed, check YAML syntax and the field rules in step 5
   by hand. Also check paths, runnable verification commands, safe cleanup
   instructions, and preservation of project policies, and confirm no
   `GENERATE_*` token remains in either file.
9. Report both output paths and the checks performed. Do not launch the loop
   merely to validate generated files.

## Task scope

Each task is a complete unit of work that can be implemented, tested, and
committed on its own. The session that runs it sees only the shared prompt and
that one task, so a task must not refer to future tasks, leave work for a
later task to finish, or depend on anything a later task will add. It may
build on what earlier tasks delivered, because that work is already in the
repository; order tasks so that holds. Verification commands must fit the
target repository; do not invent passes or require source tests for
documentation-only changes.

Make each task small enough that a person can verify it from its commit alone:
one focused change, a short diff, and a runnable check that shows it works.
Split a larger change into a sequence of such steps, each passing the agreed
quality gates on its own. When one step would break the gates until another
lands, put the prerequisite step first (for example, update test fixtures
before the change that needs them).

Honor project policies for source commits, and never instruct the session to
edit or commit `tasks.yaml`.
