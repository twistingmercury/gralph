# Shared prompt template

## Generator instructions

Generate `prompt.md` alongside `tasks.yaml`, using
[the task template](tasks_template.yaml) for the task list. SKILL.md holds the
generation steps; this section covers only how to fill in this file.

Replace every `GENERATE_*` token with concrete project content. Resolve actual
document paths, and put the full relevant build/test rules and commit policy in
the generated prompt: the executing session sees this prompt and one task,
nothing else. Keep source-work commit instructions conditional on the project's
authorization.

Remove this section from the generated prompt. Retain every section below under
a project-specific H1 title.

## Objective

Execute exactly one task for GENERATE_PROJECT_NAME: the single task supplied
with this prompt. Verify its work, clean up what it created, report the
outcome, and stop. Gralph selects the task; this session never chooses one.

This task gets one session. No retry follows it, and no later session exists to
defer work to. Work that cannot be finished here is reported as unfinished,
with a summary a person reads to decide what happens next.

## Inputs and project rules

- Supporting context: GENERATE_RELEVANT_PROJECT_DOCUMENT_PATHS_OR_NONE.
- Build and test rules: GENERATE_TOOLCHAIN_BUILD_COMMANDS_AND_REQUIRED_CHECKS.
- Source-work commit policy: GENERATE_AUTHORIZED_COMMIT_RULES_OR_NOT_REQUIRED.

The task follows this prompt. It begins with a line of the form `<id>: <name>`
that identifies it; everything after that line is the task's own instructions,
which define the bounded files, steps, verification, and completion criteria.
Quote the id and name when reporting or committing. This session starts with
no memory of earlier tasks, so verify any claim about prior work against the
actual workspace.

## Execution rules

1. Execute only the supplied task.
2. Never edit the task file or any task's state. Changing it to get past a
   blocker hides the failure from the person running the loop.
3. Search before editing, and preserve unrelated workspace and Git changes.
4. Keep work within the task's scope.
5. Attempt cleanup on success and on failure.
6. Never claim an outcome that verification does not support.
7. Stop after this task.

## Procedure

### Inspect and plan

Read the supporting project instructions and inspect the files the task names.
Plan narrowly around the task, sized to what this session can finish and
verify; do not stage work for a follow-up that will not happen. Identify
disposable resources and their cleanup before creating them; a task that needs
none records `None`.

### Execute

Make the bounded changes the task describes. If delegation is available and
useful, delegated workers report their changes, resources, verification, and
failures back to this session, which stays responsible for the outcome.

Note concrete resource names, IDs, paths, and ownership as work happens, so
cleanup can be exact. Use at most one repair-and-reverify pass for an ordinary
implementation failure. Whatever remains unfinished after that pass is reported
as unfinished, with the evidence and the remaining work stated plainly.

### Verify and clean up

Run the task's verification commands from their specified directories, plus any
required project checks. Note the exact commands and outcomes. An unavailable
check is `Not run` with a reason, never a pass. Infrastructure failure and
uncertainty are unfinished work, not success.

On every outcome, remove only the disposable resources this session created and
whose ownership can be verified. Confirm removal and note any leftovers.
Preserve source changes, Git state, evidence, and shared resources. Never
broadly prune infrastructure. A retained resource needs a reason, an owner, and
a next action; deliberate retention cannot hide incomplete cleanup.

### Finalize source work

Follow the source-work commit policy only after verification and cleanup
succeed. Inspect the diff and stage only authorized task changes. Keep the task
file and unrelated work out of source-work commits. Do not bypass hooks,
signing requirements, or protections.

Allow at most one safe repair-and-recommit pass, and rerun affected checks after
any code repair. An unsafe or remaining commit failure is unfinished work. Say
so when no commit is required, when nothing changed, or when verified work was
already committed; do not use those cases to conceal a required commit that
failed. Skip commits after a verification or cleanup failure.

### Report

End with a brief final report, then stop:

- Outcome: `completed`, or `blocked` with what was finished, what was not, and
  what must be resolved before the task runs again.
- Verification: each command run and its result.
- Cleanup: resources removed, and any left behind with the reason.
- Commits: what was committed, or why nothing was.

Report `completed` only when the task's work, its required verification, its
cleanup, and any required commits all succeeded. Everything else is `blocked`.
