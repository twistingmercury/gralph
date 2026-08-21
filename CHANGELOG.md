# Changelog

All notable changes to this project are documented in this file.

The entries below were reconstructed from the repository's annotated tags and
their associated commits. Versions follow [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [0.5.22] - 2026-08-21

### Added

- Added the optional, safe `claude-code` profile (`claude --print {prompt}`)
  alongside the existing Codex profile.
- Added copy-pasteable README examples for both registered profiles.

### Changed

- Clarified that profiles are optional conveniences, explicit agent flags
  override them, and neither profile enables a permission bypass.

## [0.5.21] - 2026-08-21

### Added

- Added `make verify`, the aggregate provider-agnostic acceptance gate for
  unit, race, end-to-end, lint, vet, security, artifact, and identifier checks.
- Added end-to-end coverage for both configured prompt transport modes.

## [0.5.20] - 2026-08-21

### Changed

- Rewrote the user and architecture documentation around the generic agent
  command contract, error policy, prompt transport, and security boundary.
- Added `make docs-check` to keep documented CLI help and Markdown links
  current.

## [0.5.19] - 2026-08-21

### Removed

- Retired Claude-specific GitHub Actions workflows and their Anthropic OAuth
  credential requirement.

### Changed

- Made the retained CI workflow pass actionlint shell-safety checks.

## [0.5.18] - 2026-08-21

### Added

- Added a registry for optional verified agent profiles, initially including
  `codex` (`codex exec {prompt}`).
- Added `--agent-profile`; explicit command flags can override each profile
  value.

## [0.5.17] - 2026-08-21

### Added

- Added platform-specific process-tree termination so cancellation cleans up
  agent descendants on supported platforms.

## [0.5.16] - 2026-08-21

### Added

- Added SIGTERM cancellation support alongside SIGINT, preserving the PRD on
  cancellation.

## [0.5.15] - 2026-08-21

### Fixed

- Corrected end-to-end timeout classification so a timed-out CLI process
  cannot be mistaken for an ordinary non-zero exit.

## [0.5.14] - 2026-08-21

### Added

- Added black-box fake-agent tests for retry behavior, startup failures, and
  fatal-error PRD preservation.

## [0.5.13] - 2026-08-21

### Added

- Added a successful fake-agent end-to-end loop test with no network or real
  AI-provider dependency.

## [0.5.12] - 2026-08-21

### Changed

- Injected loop output writers so tests no longer need to mutate global
  standard output.

## [0.5.11] - 2026-08-21

### Changed

- Replaced provider-specific output labels with agent-neutral terminology.

## [0.5.10] - 2026-08-21

### Changed

- Removed provider-specific identifiers from the production core runtime.

## [0.5.9] - 2026-08-21

### Added

- Added provider-neutral `--agent-exec`, repeatable `--agent-arg`, and
  `--prompt-mode` CLI flags.

## [0.5.8] - 2026-08-21

### Added

- Validated prompt and PRD input files before progress-file creation, agent
  invocation, or PRD mutation.

## [0.5.7] - 2026-08-21

### Fixed

- Made fatal invocation failures return immediately without abandoning or
  modifying the PRD.

## [0.5.6] - 2026-08-21

### Changed

- Replaced positional loop startup parameters with validated configuration
  containing the generic agent command.

## [0.5.5] - 2026-08-21

### Added

- Classified configuration, setup, start, cancellation, and non-zero process
  exit outcomes while preserving wrapped errors.

## [0.5.4] - 2026-08-21

### Added

- Added argument-based prompt transport using one whole `{prompt}` argument.

## [0.5.3] - 2026-08-21

### Added

- Added generic stdin-based agent execution without shell interpretation.

## [0.5.2] - 2026-08-20

### Added

- Added validation for executable configuration, prompt modes, and prompt
  placeholders.

## [0.5.1] - 2026-08-20

### Added

- Introduced the provider-neutral `AgentCommand` specification: executable,
  argument vector, and prompt transport mode.

## [0.5.0] - 2026-08-10

### Added

- Added the agent-agnostic runtime implementation plan.

## Earlier releases

| Version | Date | Summary |
| --- | --- | --- |
| 0.4.0 | 2026-03-25 | Added defaults for prompt, PRD, and progress paths. |
| 0.3.1 | 2026-03-25 | Added Ctrl+C cancellation behavior. |
| 0.3.0 | 2026-03-24 | Simplified CLI flags. |
| 0.2.0 | 2026-03-23 | Renamed CLI flags. |
| 0.1.0 | 2026-03-06 | Released the initial actively developed CLI functionality. |
| 0.0.3 | 2026-03-06 | Recorded the first self-hosted gralph build. |
| 0.0.2 | 2026-03-06 | Recorded the first Ralph-loop iteration. |
| 0.0.1 | 2026-03-05 | Initial commit. |
