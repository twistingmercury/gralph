# Agent-Agnostic Runtime — Atomic Implementation Tasks

**Goal:** Let users run gralph with any compatible noninteractive AI agent CLI by supplying an executable, an argument vector, and a prompt transport mode. No provider-specific executable, arguments, or permission policy should exist in the core loop.

Each task below is intended to be one small change that leaves the repository buildable and testable. Tasks are ordered by dependency, but every task has its own observable completion condition.

## Tasks

- [x] **Task 1 — Define the provider-neutral command specification**
  - Add an `AgentCommand` value type containing `Executable`, `Args`, and `PromptMode`.
  - Define prompt modes for `stdin` and `arg`; do not execute anything yet.
  - Keep argument boundaries as `[]string`; do not accept a shell command string.
  - Test: `go test ./internal/agent -run TestAgentCommand -v`
  - Done: the types compile, supported prompt modes are enumerated, and no field or default refers to Claude, Codex, Grok, or another provider.

- [x] **Task 2 — Validate agent command configuration**
  - Reject a blank executable, unsupported prompt mode, and invalid prompt placeholders.
  - For `arg` mode, require exactly one whole argument equal to `{prompt}`; reject embedded interpolation such as `--prompt={prompt}`.
  - Permit spaces and shell metacharacters inside individual arguments because they will remain literal arguments.
  - Test: `go test ./internal/agent -run TestAgentCommandValidate -v`
  - Done: table-driven tests cover every accepted and rejected configuration without starting a process.

- [x] **Task 3 — Implement generic stdin-based command execution**
  - Add a runner that calls `exec.CommandContext(ctx, spec.Executable, spec.Args...)` directly.
  - Send the assembled prompt through stdin when `PromptMode` is `stdin`.
  - Preserve the existing temporary-output behavior initially so this task changes execution selection, not output policy.
  - Test: `go test ./internal/agent -run TestCommandRunnerStdin -v`
  - Done: a fake executable receives the expected arguments and exact stdin, including paths and arguments containing spaces or metacharacters, with no shell interpretation.

- [x] **Task 4 — Implement argument-based prompt transport**
  - Replace the whole `{prompt}` argument with the exact prompt text when `PromptMode` is `arg`.
  - Do not perform substring interpolation or shell expansion.
  - Test: `go test ./internal/agent -run TestCommandRunnerArg -v`
  - Done: the fake executable receives the prompt as one argument, and surrounding arguments retain their original boundaries.

- [x] **Task 5 — Classify invocation outcomes**
  - Introduce explicit result/error categories for configuration failure, setup/I/O failure, process start failure, cancellation, and a process that started but exited non-zero.
  - Preserve wrapped underlying errors so callers can use `errors.Is` and `errors.As`.
  - Do not change retry behavior in this task.
  - Test: `go test ./internal/agent -run TestCommandRunnerErrors -v`
  - Done: missing executable, permission denial, cancellation, and exit status 1 produce distinguishable outcomes; `make build` succeeds.

- [x] **Task 6 — Replace positional loop startup arguments with configuration**
  - Introduce a validated `looper.Config` containing prompt, PRD, progress, maximum attempts, and `AgentCommand`.
  - Change `looper.Start` to accept the configuration and inject the generic runner.
  - Update unit-test callers without changing loop behavior.
  - Test: `go test ./internal/looper -run 'TestStart|TestRunLoop' -v`
  - Done: the loop receives its agent command exclusively through configuration; no production path selects a provider implicitly; `make build` succeeds.

- [x] **Task 7 — Make fatal invocation failures non-mutating**
  - Retry only when an agent process actually started and exited with a retryable non-zero status.
  - Return configuration, prompt-read, output-file, executable lookup/start, and cancellation failures immediately.
  - Assert that fatal failures do not create an abandonment marker and cause a non-zero CLI exit.
  - Test: `go test ./internal/looper -run TestRunLoopFatalInvocationPreservesPRD -v`
  - Done: a missing configured executable leaves the PRD byte-for-byte unchanged and returns a wrapped fatal error after one attempted start; `make build` succeeds.

- [x] **Task 8 — Validate input files before loop side effects**
  - Validate that prompt and PRD paths identify readable regular files before creating the progress file or invoking an agent.
  - Explicitly define whether symlink inputs are accepted or rejected.
  - Test: `go test ./internal/looper -run TestStartInputValidation -v`
  - Done: missing, directory, unreadable, and unsupported symlink inputs fail before progress creation and PRD mutation; `make build` succeeds.

- [x] **Task 9 — Add provider-neutral CLI flags**
  - Add required `--agent-exec`, repeatable `--agent-arg`, and `--prompt-mode stdin|arg` flags.
  - Convert parsed flags into `looper.Config` and validate them before starting the loop.
  - Do not add implicit permission-bypass arguments or a default provider.
  - Test from `tests/e2e`: `go test -run 'TestAgentFlags|TestMissingAgentExecutable|TestInvalidPromptMode' -v .`
  - Done: CLI help documents the generic contract, missing/invalid configuration exits non-zero, and repeated arguments retain order; `make build` succeeds.

- [ ] **Task 10 — Remove provider-specific names from core code**
  - Rename `commandRunner`, `defaultClaudeRunner`, `invokeClaude`, `claudeOutputPath`, and related comments to provider-neutral terminology.
  - Restrict provider names to explicit examples, optional profiles, or migration documentation.
  - Test: `go test ./...` and `rg -n 'Claude|claude' cmd internal`
  - Done: all root-module tests pass and the search returns no provider-specific identifiers in production core code; `make build` succeeds.

- [ ] **Task 11 — Remove provider-specific output labels**
  - Replace `Files Changed` with a label that accurately describes arbitrary agent output.
  - Leave output routing and buffering unchanged in this task.
  - Test: `go test ./internal/looper -run TestRunLoopLogsHumanReadableItemLabels -v`
  - Done: no output label assumes a provider-specific response format; `make build` succeeds.

- [ ] **Task 12 — Inject loop output writers**
  - Add output writers to `looper.Config` instead of writing directly to global `os.Stdout`.
  - Update tests to use buffers without swapping process-global file descriptors.
  - Test: `go test ./internal/looper -run TestRunLoopOutputWriter -v`
  - Done: loop tests can run in parallel without mutating `os.Stdout`; `make build` succeeds.

- [ ] **Task 13 — Define agent output capture and cleanup**
  - Capture stdout and stderr separately while preserving their ordering limitations in documentation.
  - Define which attempts are printed and ensure temporary files are removed on success, retry, fatal error, and cancellation.
  - Test: `go test ./internal/agent -run 'TestCommandRunnerOutput|TestCommandRunnerCleanup' -v`
  - Done: each output stream and every cleanup path has a focused test; `make build` succeeds.

- [ ] **Task 14 — Add a successful fake-agent end-to-end test**
  - Build a deterministic local fake agent during e2e setup.
  - Run gralph with the fake agent in stdin mode and have it complete one PRD item.
  - Assert prompt contents, runtime paths, output, PRD transition, and exit code.
  - Test from `tests/e2e`: `go test -run TestAgentLoopSuccess -v .`
  - Done: a complete loop is tested through the compiled gralph binary without network access or a real AI provider; `make build` succeeds.

- [ ] **Task 15 — Add failure-policy fake-agent end-to-end tests**
  - Cover a started agent exiting non-zero, a missing executable, a non-executable file, and an invalid prompt configuration.
  - Verify retry counts only for the started/non-zero case.
  - Verify fatal cases preserve the PRD and exit non-zero immediately.
  - Test from `tests/e2e`: `go test -run 'TestAgentRetry|TestAgentStartupFailure' -v .`
  - Done: the destructive missing-agent behavior from the 2026-08-10 review is prevented by black-box regression tests; `make build` succeeds.

- [ ] **Task 16 — Correct e2e timeout classification**
  - Check `ctx.Err()` before treating `*exec.ExitError` as an ordinary CLI result.
  - Add a deliberately blocking fixture that exceeds the test deadline.
  - Test from `tests/e2e`: `go test -run TestRunCLITimeout -v .`
  - Done: a timed-out process always fails the test instead of satisfying a generic non-zero-exit assertion; `make build` succeeds.

- [ ] **Task 17 — Add SIGTERM cancellation**
  - Register SIGTERM in addition to SIGINT and preserve cancellation through the generic runner.
  - Verify direct child termination and PRD preservation; leave descendant process groups to the next task.
  - Test from `tests/e2e`: `go test -run 'TestAgentSIGINT|TestAgentSIGTERM' -v .`
  - Done: either signal stops the fake agent, gralph exits non-zero, and the PRD remains unchanged; `make build` succeeds.

- [ ] **Task 18 — Terminate agent descendant processes**
  - Add platform-specific process-tree termination behind small build-tagged helpers.
  - Begin with Unix process groups; add or explicitly document the Windows job/process strategy before claiming Windows support for this behavior.
  - Test: `go test ./internal/agent -run TestTerminateProcessTree -v`; from `tests/e2e`, `go test -run TestAgentDescendantTermination -v .`
  - Done: cancellation leaves no fake-agent descendant running on every platform for which the behavior is documented as supported; `make build` succeeds.

- [ ] **Task 19 — Add optional provider profiles without weakening the generic path**
  - Add a small profile registry only for providers whose current noninteractive invocation contract is verified.
  - Profiles may supply executable, arguments, and prompt mode; explicit CLI values must be able to override or bypass them.
  - Keep permission-bypass behavior opt-in and visible.
  - Test: `go test ./internal/agent -run TestProfiles -v`
  - Done: each profile expands to an ordinary validated `AgentCommand`, and an arbitrary unregistered executable remains fully supported; `make build` succeeds.

- [ ] **Task 20 — Replace Claude-specific repository automation**
  - Retire or replace `.github/workflows/claude.yml` and `.github/workflows/claude-code-review.yml` so repository automation does not require Claude credentials.
  - If a replacement workflow posts PR comments, grant only the required write permission.
  - Keep runtime provider choice separate from repository-review automation.
  - Test: `actionlint .github/workflows/*.yml`
  - Done: no required GitHub workflow depends on Anthropic actions or `CLAUDE_CODE_OAUTH_TOKEN`; `make build` succeeds.

- [ ] **Task 21 — Update the user and architecture contracts**
  - Update README usage, requirements, ADRs, system architecture, deployment architecture, and output examples.
  - Supersede the Claude-specific execution ADR with the generic command specification, prompt transport, error policy, and security boundary.
  - Document the compatibility contract: noninteractive one-shot execution, configured prompt transport, meaningful exit status, and process termination.
  - Test: add and run `make docs-check` to compare captured `--help` output with documented flags and validate Markdown links.
  - Done: the documented default path requires no named provider, and Claude, Codex, and Grok appear only as optional examples or verified profiles; `make build` succeeds.

- [ ] **Task 22 — Add the final provider-agnostic acceptance gate**
  - Add one aggregate target that runs root unit tests, root race tests, e2e tests, e2e race tests, vet, lint, security checks, and provider-neutral identifier checks.
  - Assert the required release artifacts rather than checking for only one binary.
  - Test: `make verify`
  - Done: `make verify` passes from a clean checkout, fake-agent tests cover both prompt modes, `rg -n 'defaultClaudeRunner|invokeClaude|CLAUDE_CODE_OAUTH_TOKEN' cmd internal .github/workflows` returns no matches, and `make build` succeeds.

## Recommended Delivery Boundaries

1. **Safe generic runtime:** Tasks 1–9.
2. **Neutral output and executable coverage:** Tasks 10–18.
3. **Profiles, automation, and documentation:** Tasks 19–21.
4. **Release gate:** Task 22.
