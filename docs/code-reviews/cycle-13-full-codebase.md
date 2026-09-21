# Code Review: Cycle 13 — Full Codebase

**Review Date:** 2026-03-05
**Reviewers:** code-reviewer (tactical), solutions-architect (strategic), go-idiom (synthesizer)
**Phase:** Post-implementation (all 13 PRD cycles complete)

## Files Reviewed

### Source Files

- `cmd/main/main.go` — CLI entrypoint, pflag-based
- `internal/looper/looper.go` — core loop logic
- `internal/version/version.go` — version/build info
- `internal/tooling.go` — blank imports for go.mod
- `build/Dockerfile` — Docker build pipeline
- `build/build.sh` — build entrypoint
- `Makefile` — local build targets
- `scripts/ralph.sh` — behavior reference

### Test Files

- `internal/looper/looper_test.go`
- `tests/e2e/gralph_test.go`
- `tests/e2e/gralph-tests.go`

## Validation Results

| Tool          | Result                                      |
| ------------- | ------------------------------------------- |
| go build ./...| exit 0                                      |
| go test ./... | ok internal/looper (22 tests); exit 0       |
| golangci-lint | 0 issues                                    |
| govulncheck   | no vulnerabilities                          |
| gosec         | 0 issues (9 nosec annotations)              |

## Design Compliance

Implementation satisfies all 13 PRD behavioral requirements. Loop semantics match `scripts/ralph.sh`: first-open-item detection, attempt counter reset on item change, Claude non-zero exit as non-fatal failed attempt, abandon mutation on limit, loop termination when no `- [ ]` items remain.

### Behavioral Requirements Verified

- First `- [ ]` detection by line prefix ✓
- Attempt counter reset when item changes ✓
- Completion detected by PRD re-read after invocation ✓
- Abandon replaces only the first `- [ ]` with `- [~]` ✓
- Claude invoked with `--print --dangerously-skip-permissions` ✓
- Prompt + runtime paths injected as stdin ✓
- Progress file created at derived or explicit path ✓
- All flags: `--prompt`, `--prd`, `--progress`, `--iterations/-i`, `--version` ✓

### Design Doc Divergences

None identified.

## Findings

### HIGH Priority

| ID | Source                   | Finding                                                                                                                                                                                                                                                                                    | Resolution                                                                                                                                                                                                                                                                |
| -- | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H1 | tactical + architectural | **Progress file opened but never written to.** `looper.go:38–44`: `Start` opens progress file with `O_APPEND\|O_CREATE\|O_WRONLY` and holds the handle for the entire run, but `runLoop` never uses it — all logs go to `fmt.Printf`. The `file` var is dead code beyond its `defer Close`. The file is created as a side effect of `OpenFile`, which is the only useful behavior. | Replace the open-handle pattern with a named `ensureProgressFileExists` helper that opens and immediately closes the file (or use `os.WriteFile(path, nil, 0o600)` as a touch). Thread the path string (not a handle) to `runLoop` as it already is. |
| H2 | tactical + architectural | **`claudeRunner` package-level var blocks parallelism and has no cancellation exit.** The global var requires `claudeRunner = stub; defer restore` in every test, preventing `t.Parallel()` system-wide. Additionally, after `invokeClaude` returns a `context.Canceled` error, the loop continues reading the PRD and retrying — ctx is never checked. | (a) Pass `commandRunner` as a parameter to `runLoop` and `invokeClaude`; `Start` supplies `defaultClaudeRunner`. (b) Add `if err := ctx.Err(); err != nil { return err }` after the `invokeClaude` call and after each `getFirstOpenItem` call. |
| H3 | tactical                 | **`--iterations=0` or negative causes every item to be abandoned immediately.** `looper.go:98`: `if attempt >= maxAttempts` triggers on attempt=1 when maxAttempts=0 (1 >= 0 is true). The bash reference hardcodes the limit; this CLI accepts user input without validation. | Add `if *iterationsFlag < 1` guard in `validateRequiredFlags` (or in `Start`) returning an actionable error. |

### MEDIUM Priority

| ID | Source       | Finding                                                                                                                                                                                                                                                            | Resolution                                                                                                                                                                                                                                                        |
| -- | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1 | architectural| **`go mod tidy` in Dockerfile mutates go.sum silently.** `build/Dockerfile:14`: `go mod tidy` runs inside the container and silently writes a different `go.sum` if the committed one diverges. The committed file is never validated — it is overwritten. | Replace `go mod tidy` in Dockerfile with `go mod verify`. Move `go mod tidy` to a developer-facing `make tidy` target. |
| M2 | tactical + architectural | **`internal/tooling.go` blank imports have no doc comment and will be removed by `go mod tidy`.** The file imports cobra, viper, and testify with `_` to keep them in `go.mod`, but there is no comment in the file explaining this. A routine dep-hygiene pass will silently drop these. | Add a file-level doc comment: `// Package internal retains go.mod entries for packages used in future cycles. Remove once adopted.` and list each import with its intended consumer. Since all 13 PRD cycles are now complete and none use cobra or viper, evaluate removing this file entirely and tidying go.mod. |
| M3 | tactical     | **Ignored errors in test assertions.** `looper_test.go:125,155,179,340,373,410`: `os.ReadFile` errors are silently discarded with `_, _` or `data, _`. A bad temp dir or permissions issue silently produces an empty string, causing assertions to pass vacuously. | Replace all `after, _ := os.ReadFile(path)` patterns with `after, err := os.ReadFile(path); if err != nil { t.Fatal(err) }`. |
| M4 | tactical     | **`TestStart_DerivedProgressPath` and `TestStart_ExplicitProgressPath` ignore the `Start` return value.** `looper_test.go:52,72`: `_ = Start(...)` — if `Start` returns an error, the test only checks file existence. The progress file is created by `os.OpenFile` before `runLoop` runs, so the existence check can pass while masking a real error. | Change `_ = Start(...)` to `if err := Start(...); err != nil { t.Fatalf(...) }` in both tests. |
| M5 | architectural| **E2e stage recompiles binary from source.** `tests/e2e/Dockerfile` copies all source and `TestMain` runs `go build ./cmd/main` again, duplicating the builder stage compile. | Copy the already-exported binary from `.bin/` into the e2e container instead of recompiling. |
| M6 | architectural| **`make test` excludes e2e tests.** `Makefile:33`: `go test -v ./internal/...` never touches `tests/e2e`. Contributors relying on `make test` as the pre-push gate miss all e2e coverage. | Add a `make e2e` target (runs `./build/build.sh` or `cd tests/e2e && go test .`) and document in the README that `make test` is unit-only. |
| M7 | tactical     | **`TestMain` temp dir not cleaned up if panic occurs before cleanup.** `tests/e2e/gralph_test.go:55`: `os.RemoveAll(tmpDir)` is called before `os.Exit(code)`, but if the binary build panics after `tmpDir` is created, cleanup is skipped. | Factor the build+run logic into a helper that returns the exit code, then call cleanup before `os.Exit` in `TestMain`. (Note: `defer` does not run before `os.Exit`.) |

### LOW Priority

| ID | Source       | Finding                                                                                                                                                             | Resolution                                                                                               |
| -- | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| L1 | tactical     | **Duplicate word in log line.** `looper.go:77`: `[gralph] item item=%q` — the token `item` is repeated.                                                            | Change to `[gralph] attempt item=%q attempt=%d max=%d` or `[gralph] working item=%q attempt=%d max=%d`. |
| L2 | tactical     | **`version.go` vars default to empty strings.** When built without `-ldflags`, `--version` prints blank values for version, date, and commit.                      | Default to `"dev"`, `"unknown"`, `"unknown"` and add inline comments with the full ldflags symbol path.  |
| L3 | tactical     | **Makefile `local` target builds a single file.** `Makefile:21`: `./cmd/main/main.go` — fragile if a second file is added to the package.                          | Change to `./cmd/main` (the package path).                                                               |
| L4 | architectural| **`docker system prune -f` in `make build` removes unrelated images.** Affects all dangling images system-wide.                                                    | Replace with `docker image prune -f` scoped to gralph images, or remove the prune step entirely.        |
| L5 | architectural| **`gralph-tests.go` is a dead file.** Contains only a package declaration; all tests live in `gralph_test.go`.                                                     | Delete the file.                                                                                         |
| L6 | tactical     | **Non-idiomatic boolean negation in tests.** `looper_test.go:101,104`: `if strings.Contains(...) == false` — idiomatic Go is `if !strings.Contains(...)`.          | Replace with `!strings.Contains(...)`.                                                                   |
| L7 | tactical     | **Duplicate `build` in `.PHONY`.** `Makefile:1`: `build` appears twice in the `.PHONY` declaration.                                                                | Remove the duplicate.                                                                                    |

## Patterns to Document

1. **`commandRunner` function-variable injection** (`looper.go`): a lightweight, interface-free seam for single-function dependencies in Go CLIs. Recommend parameterizing it (pass to `runLoop`) rather than using a package-level var.
2. **Temp-file-rename for safe in-place file mutation** (`abandonFirstOpenItem`): canonical pattern for atomic file writes. Temp file lands in the same directory as target to guarantee same filesystem.
3. **`TestMain` binary-build-once pattern** (`tests/e2e/gralph_test.go`): build the CLI binary once in `TestMain`, share across all tests via a package-level path. Avoids redundant builds per test.

## Notes for Future Phases

**Pre-1.0 release**: Address H1, H2, H3 before tagging. H1 and H3 are small; H2 requires threading `commandRunner` as a parameter.

**Dependency hygiene**: Evaluate removing `internal/tooling.go` entirely now that all 13 PRD cycles are complete. Cobra and Viper were retained for future use but neither is planned in the current scope.

**Build pipeline**: M1 (`go mod verify` in Dockerfile) and M5 (avoid recompile in e2e) are one-line fixes each and should be addressed before CI is trusted as a quality gate.
