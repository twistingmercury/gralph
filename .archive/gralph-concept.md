# The Ralph Loop Concept

A Ralph loop is a structured workflow for complex, multi-step AI tasks. It combines a shared prompt with a sequence of individual task prompts, running each task in isolation with no context carryover from prior steps.

## How Gralph Implements It

Gralph reads two files:

- **prompt.md**: A shared prompt that sets context, instructions, and success criteria
- **tasks.yaml**: An ordered list of tasks, each with a unique id, name, and task-specific prompt

For each pending task, gralph:

1. Combines the shared prompt with the task's prompt
2. Spawns a fresh `claude --print` session with the combined prompt on stdin. The session runs in Claude Code's sandbox, set up by the file you pass with `--sandbox-settings`; it runs without one only if you ask for that by name with `--skip-permissions`
3. Reads Claude's output, looks for a JSON result line, and determines success/failure
4. If the session succeeded, runs the task's `gates` (commands gralph runs itself to verify the work); the task is `completed` only when every gate exits zero
5. If `--commit` is set and the gates passed, commits the task's changes with git under the task's name
6. Updates task state (pending, completed, failed) and writes it back to tasks.yaml atomically
7. Blocks on the first failure until manually addressed

This ensures each task starts fresh (no context bleed) while tracking progress across multiple runs.

## Learn More

- [README.md](../README.md) — Usage and overview
- [docs/architecture/00_overview.md](./architecture/00_overview.md) — Full architecture
