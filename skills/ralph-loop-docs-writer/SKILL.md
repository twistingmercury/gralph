---
name: ralph-loop-docs-writer
description: >-
  Use when creating or updating the tasks.yaml task list and shared prompt.md
  that Gralph feeds to Claude Code, when breaking a build plan or PRD into
  Gralph loop tasks, or when migrating a Markdown PRD checklist to Gralph's
  YAML task format.
---

# Ralph Loop Docs

Generate the two files a Gralph run needs: `tasks.yaml`, an ordered task list,
and `prompt.md`, the shared prompt. Respect the user's destination, project
scope, verification, and commit policies.

## How Gralph uses the pair

Gralph works through `tasks.yaml` in file order. For each pending task it
combines the shared prompt with that task's `prompt` and hands the result to a
new Claude Code session. Every session starts with no memory of earlier tasks,
so the pair must carry everything the session needs.

Each task gets one session. There is no retry and no `--iterations` flag, so
scope every task to what a single session can finish and verify.

Gralph owns `state`; the session never edits `tasks.yaml`. `abandoned` is set
by a person to make Gralph pass over a task.

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
3. Write `tasks.yaml` with a top-level `tasks` sequence holding at least one
   task. Each task has a stable positive integer `id` (at most 32767), a
   nonblank `name`, a nonblank block-scalar `prompt`, and an optional `state`.
   IDs are unique and independent of order; Gralph runs tasks in file order.
   Names are unique ignoring case and surrounding whitespace. `state` is
   `pending`, `completed`, or `abandoned`; leave it empty for new work, which
   Gralph reads as `pending`. Gralph ignores any other key.
4. Put scope, steps, runnable verification, and completion criteria inside each
   task's prompt. Preserve project-specific instructions. YAML comments are not
   durable task instructions: they never reach the session, and rewriting the
   file may discard them.
5. Write `prompt.md` from the prompt template. Replace every `GENERATE_*` token
   with project content and actual paths, and omit the template's generator
   guidance. Keep every execution section so the generated pair works without
   this skill installed.
6. Check YAML syntax and the field rules in step 3 for every task, including
   completed and abandoned ones. An empty `tasks` sequence is an error. Check
   paths, runnable verification commands, safe cleanup instructions, and
   preservation of project policies. Confirm no `GENERATE_*` token remains in
   either file.
7. Report both output paths and the checks performed. Do not launch the loop
   merely to validate generated files.

## Task scope

Each task is a complete unit of work that can be implemented, tested, and
committed on its own. The session that runs it sees only the shared prompt and
that one task, so a task must not refer to future tasks, leave work for a
later task to finish, or depend on anything a later task will add. It may
build on what earlier tasks delivered, because that work is already in the
repository; order tasks so that holds. Keep task prompts focused and split
independent goals. Verification commands must fit the target repository; do
not invent passes or require source tests for documentation-only changes.

Honor project policies for source commits, and never instruct the session to
edit or commit `tasks.yaml`. Preserve existing progress and history files.

## Explicit migration

Installing this skill does not migrate existing projects. Migrate only when
requested. Inventory the old input before editing: written task IDs, sequence,
full instructions, policies, and states. Map Markdown `[ ]` to `pending`,
`[x]`/`[X]` to `completed`, and `[~]` to `abandoned`. Preserve existing YAML
states, IDs, order, and prompts.

Report ambiguous or duplicate IDs and conflicting old/new pairs; stop that
migration instead of silently renumbering or choosing one file as
authoritative. Do not infer completion from logs or prose. Compare source and
target inventories before updating links: every task must retain its identity,
meaning, and state. Retain the original Markdown task history and existing
progress/history files. Update the shared prompt and invocation examples
together only after reconciliation.
