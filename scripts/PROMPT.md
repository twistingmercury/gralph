You are executing exactly one iteration of a Ralph loop for `gralph`.

## Required behavior for this run

1. Read the file at the path given by **PRD** in the Runtime paths section below.
2. Identify the first unchecked checklist item (`- [ ]`) under `## Implementation Plan`.
3. Read the file at the path given by **Progress** in the Runtime paths section below.
4. Search the codebase before implementing. Do not assume anything is missing or unimplemented.
5. Parse the selected checklist item and extract:
   - item title
   - `Agent:`
   - `Files:`
   - `Steps:`
   - `Verify:`
6. Delegate implementation to the sub-agent named in `Agent:`.
   - Pass the item title, Files, Steps, and Verify content as the full task.
   - Require the sub-agent to make the minimal correct change set.
   - Require the sub-agent to execute the item Verify command and fix issues until it exits 0.
7. Create or update automated tests for any behavior changed in this iteration.
   - Prefer unit tests close to the changed package.
   - Add integration/e2e coverage only when unit tests cannot prove behavior.
8. Run tests for this item and fix failures:
   - run targeted tests for changed packages first
   - then run `go test ./...`
9. Run these quality gates and fix findings:
   - `goimports -w .`
   - `golangci-lint run ./...`
   - `govulncheck ./...`
   - `gosec ./...`
10. Re-run the item Verify command and `go test ./...` after quality-gate fixes.
11. Mark the selected PRD item as complete (`- [x]`) only when all checks pass.
12. Append a status entry to the Progress file with:
    - timestamp
    - item title
    - outcome (`done` or `blocked`)
    - files changed
    - tests added/updated
    - verify command + result
    - `go test ./...` result
    - quality gate results
    - concise notes
13. Stop immediately after this single item. Do not start the next checklist item.

## Failure handling

- If any command fails, keep working until it passes.
- If blocked by missing tools, environment, or permissions:
  - do not mark the PRD item complete
  - append a `blocked` entry with exact blocker and next action
  - stop

## Constraints

- Keep behavior aligned with `scripts/ralph.sh` semantics.
- Keep changes scoped to the selected item only.
- Do not perform unrelated refactors.
- Prefer concrete logs and actionable errors.

## Output format at end of run

Print exactly:
- completed item title (or blocked item title)
- test summary (targeted tests + `go test ./...`)
- verify command result
- quality gate summary
- short note on residual risk or blocker
