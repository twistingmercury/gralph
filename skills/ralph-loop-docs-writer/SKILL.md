---
name: ralph-loop-docs-writer
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
each `pending` or `failed` task it combines the shared prompt with that task's
`prompt` and hands the result to a new Claude Code session. Every session starts
with no memory of earlier tasks, so the pair must carry everything the session
needs.

Each task gets one session. There is no retry and no `--iterations` flag, so
scope every task to what a single session can finish and verify.

Gralph owns `state` and writes it back to `tasks.yaml`. The session never edits
the file. To remove a task without running it, delete it from the file; there is
no `abandoned` state. Gralph decides the outcome from the JSON result line the
shared prompt asks for at the end of the session's output. A missing or invalid
result line, or a non-zero exit, marks the task `failed`. Running Gralph again
re-runs `failed` tasks, so fixing a failed task means editing its `prompt`.

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
3. Write `tasks.yaml` with a top-level `tasks` sequence holding at least one
   task. Each task has a stable positive integer `id` (at most 32767), a
   nonblank `name`, a nonblank block-scalar `prompt`, and an optional `state`.
   IDs are unique and independent of order; Gralph runs tasks in file order.
   Names are unique ignoring case and surrounding whitespace. `state` is
   `pending`, `completed`, or `failed`; leave it empty for new work, which
   Gralph reads as `pending`. The optional `error` field is gralph-only: never
   write it; preserve it if present when updating an existing file. Write no
   other keys: Gralph ignores them when reading and drops them the first time
   it saves the file.
4. Put scope, steps, runnable verification, and completion criteria inside each
   task's prompt. Preserve project-specific instructions. YAML comments are not
   durable task instructions: they never reach the session. Gralph rewrites the
   task file with every state change, so comments and custom formatting are not
   preserved.
5. Write `prompt.md` from the prompt template. Replace every `GENERATE_*` token
   with project content and actual paths: full build/test commands and the
   project's commit policy, since the session sees only this prompt and one
   task. Keep commits conditional on the project's authorization. Keep the
   Rules and Finish sections as written so the pair works without this skill
   installed. Keep generic rules out of task prompts; they live once, in
   `prompt.md`.
6. Check YAML syntax and the field rules in step 3 for every task, including
   completed and failed ones. An empty `tasks` sequence is an error. Check
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
edit or commit `tasks.yaml`.
