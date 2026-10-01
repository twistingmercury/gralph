# GENERATE_PROJECT_NAME

You are running one task. It follows this prompt, starting with a line
`<id>: <name>`; everything after that line is the task's own instructions. You
have no memory of earlier tasks, so check any claim about prior work against
the repository.

## Project

- Context documents: GENERATE_RELEVANT_PROJECT_DOCUMENT_PATHS_OR_NONE
- Build and test: GENERATE_TOOLCHAIN_BUILD_AND_TEST_COMMANDS
- Commits: GENERATE_AUTHORIZED_COMMIT_RULES_OR_NOT_REQUIRED

## Rules

- Do only this task, within its stated scope.
- Never edit or commit `tasks.yaml`.
- Preserve unrelated changes in the workspace and Git.
- Clean up anything you created that the task does not keep.
- Commit only after the task's verification passes.

## Finish

End with a short report of what changed, each verification command and its
result, and what was committed. The last non-blank line of your output must be
this JSON object, on a single line, with nothing else on that line:

{"state": "completed", "error": ""}

`state` is `completed` only when the work, its verification, cleanup, and any
required commit all succeeded; otherwise it is `failed`, with a one-line
`error`.
