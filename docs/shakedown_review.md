# Shakedown review

Date: 2026-10-01, against `develop` at `fb47f35` (v0.9.2).

The last four items of the shakedown: whether the tests assert things that
matter, docs drift, stale leftovers, and the sandbox's placeholder dotfiles.
The findings below are as they were found. What was done about them since is
in "Outcome".

How it was done: four independent reviewers (looper unit tests, the other unit
tests, the e2e suite, the docs), plus a direct check of the sandbox behaviour.
The two unit-test reviewers broke production code in a scratch copy and ran
the tests, so their findings are marked:

- **survived**: the code was broken and no test failed.
- **killed**: the code was broken and a test failed.
- **read**: judged from reading only, not run.

## Summary

| Area              | Verdict                                                                                   |
| ----------------- | ----------------------------------------------------------------------------------------- |
| Tests             | Core contract is pinned hard. The holes are at the edges; 11 are worth a test (section 1) |
| Docs              | The PR #22 renames left one stale name. Older drift is real, mostly in `03` (section 2)   |
| Sandbox dotfiles  | A real hazard for `--commit`, not seen to bite yet (section 3)                            |
| Stale leftovers   | Dealt with (section 4)                                                                    |

## Outcome

Applied on branch `maint/shakedown-leftovers`. The test findings were closed by
a 10-task gralph loop run with `--commit`, one commit per task. Every task
changed test files only, and each new or tightened test was shown to fail with
the behaviour broken before the production code was restored.

### Tests

| Finding                                   | Result                                                                                   |
| ----------------------------------------- | ---------------------------------------------------------------------------------------- |
| T1                                        | Closed: a failed save stops the run, after a completed task and after a failed one       |
| T2                                        | Closed: a failed save leaves the existing file untouched; the 2-space indent is pinned   |
| T3                                        | Closed: `q` then `y` against a real blocked session, at the `tui.Run` level              |
| T4                                        | Closed: `--iterations` must be rejected as an unknown flag                               |
| T5                                        | Closed: the full commit message is asserted                                              |
| T6                                        | Closed: a root-anchored ignore rule checked from a subdirectory                          |
| T7                                        | Closed: a gate's child process is gone after a timeout and after a cancel                |
| T8                                        | Closed: a gate's stderr reaches gralph's stderr in plain mode                            |
| T9                                        | Closed: a bad or empty `--gate-timeout` is reported before any other flag error          |
| T10                                       | Closed: the three confirm-prompt cases                                                   |
| T11                                       | Not done, on purpose: `runTUI` reads the global flags and needs a terminal, so testing it means changing production code first |
| `WithoutSignalHandler`, git's `WaitDelay` | Accepted as is                                                                           |

From "Tests that prove less than their name says":

| Test                                                         | Result                                           |
| ------------------------------------------------------------ | ------------------------------------------------ |
| `TestMissingPrompt`, `TestMissingTasks`, the dry-run half    | Closed: they match the error line, not the usage |
| `TestHelpFlag`                                               | Closed: `--gate-timeout` and `--commit` added    |
| `TestNonexistentFiles`                                       | Closed: the tasks-file error is asserted too     |
| Gate "stdin is empty, not inherited"                         | Closed: replaced by a test with a real stdin     |
| Commit "from a subdirectory"                                 | Closed by T6                                     |
| `TestRun_NothingToCommitStillCompletes`                      | Closed: no `commit:` line when nothing is staged |
| "StylePerState", the rejected setup entry, the signal summary | Closed                                          |
| The skill hash test, e2e "git is never touched"              | Not done                                         |
| Loose `NotEqual(0)` exit codes                               | Tightened only in the tests the loop touched     |

Not done: everything under "Helpers" (including the environment leak),
"Brittle", "Redundant", and "Other gaps".

One thing the loop got wrong and that was fixed by hand afterwards: tests that
forced a failed save with a read-only directory. The release build runs the
unit tests as root, which ignores directory permissions, so T1's tests failed
there and T2's skipped. Both now fail the save in a way that holds for any
user. `TestSaveTasks_TargetDirNotWritable`, which predates this work, still
skips as root.

### Docs

D1 to D14 and the cosmetic list are fixed, in place, on the same branch. The
five architecture docs that changed had their Version, Date, and Notes bumped.

- The older ADRs (001, 003, 009, 011, 013) keep their original text and carry
  an "Amended by" note pointing at ADR-010 or ADR-014.
- `03` now threads `--commit` through its flag list, diagrams, component table,
  and process control section.
- The `make install` warning is gone from `CLAUDE.md`, the README, and `05`.

### Sandbox dotfiles

Deferred on 2026-10-01: nothing changes until it causes a real problem. The
loop's shared prompt told sessions to leave the
placeholder files alone and not report them (option 2, for that run only).

## 1. Do the tests assert things that matter?

Mostly yes. Looper: 26 breaks tried, 15 killed, 11 survived. The rules gralph
stands on are all pinned (see "Solid" at the end of this section).

### Worth a test

Ordered by how much damage the unpinned behaviour could do.

| #   | What is unpinned                                                 | Where                                           | Evidence |
| --- | ---------------------------------------------------------------- | ----------------------------------------------- | -------- |
| T1  | A failed save of the task file is swallowed and the run goes on  | `internal/looper/looper.go:232-234, 244-246`    | survived |
| T2  | `SaveTasks` writes atomically (temp file, then rename)           | `internal/tasks/task.go:381-386`                | survived |
| T3  | Pressing `y` in the view really stops the run                    | `internal/tui/run.go:20`                        | survived |
| T4  | `--iterations` stays rejected (the "no retry" pin)               | `tests/e2e/claude_loop_test.go:226-231`         | read     |
| T5  | The commit message is the whole task name, not its first line    | `internal/looper/commit_test.go:550`            | survived |
| T6  | Git runs from the work tree's root                               | `internal/looper/commit.go:204`                 | survived |
| T7  | A gate runs in its own process group                             | `internal/looper/gates.go:110`                  | survived |
| T8  | A gate's stderr reaches the terminal in plain mode               | `internal/looper/gates_test.go:171`             | survived |
| T9  | `--gate-timeout` is checked before everything else               | `tests/e2e/gates_test.go:227-266`               | read     |
| T10 | Three confirm-prompt edge cases in the view                      | `internal/tui/model.go:104-110, 214`            | survived |
| T11 | `runTUI`'s setup merge, "setup cancelled", the `--no-tui` hint   | `cmd/main/main.go:113-119`                      | read     |

Notes on each:

- **T1.** No test makes the task directory unwritable. "State saved after
  every task" could fail silently.
- **T2.** Writing straight to the target passes the tasks, tui, and looper
  suites. The tests only check that no `.tmp` is left behind, which a
  non-atomic write also satisfies. The 2-space indent is unpinned in those
  suites too (`SetIndent(4)` survived; not tried against e2e).
- **T3.** A do-nothing cancel passes. `model_test.go:295-310` proves the model
  calls a fake cancel; no `Run`-level test presses `q` then `y`. E2E has no
  terminal, so nothing else covers it.
- **T4.** The test runs only `--iterations=3` and asserts a non-zero exit and
  the word "iterations" in stderr. If the flag came back, the run would still
  exit 1 for the missing `--prompt`, and the usage text would list the flag.
  Assert `unknown flag: --iterations` instead.
- **T5.** The multi-line-name test checks only the subject (`%s`). Committing
  the first line alone passes. Assert `%B`.
- **T6.** Removing `cmd.Dir = r.root` passes, because `git status` and
  `git add -A` cover the whole tree from any directory. The case that would
  bite is `git check-ignore` against a root-anchored pattern from a
  subdirectory (not tried).
- **T7.** Removing `configureProcessTree` from the gate passes. The cancel and
  timeout tests use `sleep`, which the shell execs in place, so there is no
  descendant to orphan.
- **T8.** The session's stdout, stderr, and prompt echo are also loose in the
  unit tests, but e2e covers those three. A gate writing to stderr is covered
  nowhere.
- **T9.** The e2e test always passes valid other flags, so moving the check
  later would pass. An explicit empty `--gate-timeout=` has no e2e case either.
- **T10.** A signal after the run is done; the run ending while the confirm is
  open; a signal while the confirm is open. `model_test.go:312` never opens
  the prompt before the interrupt.

Possibly accept as is: `tea.WithoutSignalHandler` (`tui/run.go:21`, survived)
needs a pty to test. Git's `WaitDelay` backstop (`process_tree_unix.go:71`,
survived) may do nothing on the commit path today.

### Tests that prove less than their name says

- `tests/e2e/gralph_test.go:188-201` `TestMissingPrompt`/`TestMissingTasks`:
  `Contains(stderr, "--prompt")` is satisfied by the usage dump on any error.
  `TestMissingBothRequiredFlags` (`:204`) does the real check and covers both.
  `dry_run_test.go:106` has the same weak half.
- `gralph_test.go:182` `TestHelpFlag`: says "every documented flag", omits
  `--gate-timeout` and `--commit`.
- `gralph_test.go:215-225` `TestNonexistentFiles`: named for prompt and tasks,
  asserts only the prompt error. The tasks error is unit-only.
- `internal/looper/gates_test.go:125` "stdin is empty, not inherited": stdin
  is already empty under `go test`, so inheriting it passes (survived).
- `internal/looper/commit_test.go:278, 525` "from a subdirectory": see T6.
- `internal/looper/commit_test.go:400`: checks no commit was made, not that no
  `commit: <name>` line was printed (survived).
- `internal/tui/model_test.go:48-50, 87` "StylePerState": builds its
  expectation from the same `rowStyles` map, so swapped colours pass
  (survived).
- `internal/tui/setup_test.go:66-68`: a rejected entry leaving the model
  unchanged is not asserted (survived).
- `internal/tui/run_test.go:145-150`: a dropped signal message is caught only
  by the 10s timeout, not by an assertion.
- `internal/skillinstall/skillinstall_test.go:93-112`: re-implements the hash
  line for line. Nothing shows the hash changes when the content does.
- `tests/e2e/commit_test.go:126`: "git is never touched" only checks for no
  new commit. A stray `git add` would pass.
- Many e2e tests assert `NotEqual(0)` where the README promises exit 1
  (`claude_loop_test.go:157, 218, 280, 312`, most of `claude_result_test.go`,
  `gates_test.go:83, 152, 184, 258, 289`, `signal_unix_test.go:72`). A
  signal-killed gralph maps to -1, which also passes. The stderr assertions
  mostly save these.

### Helpers

- `tests/e2e/claude_helpers_test.go:49-58`: the environment helper copies the
  outer environment and strips only `PATH` and `HOME`. With
  `FAKECLAUDE_EXIT_CODE=1` exported, two tests fail (run and confirmed). Low
  risk in the container. Strip `FAKECLAUDE_*`, `FAKE_CLAUDE_*`, and `GIT_*`.
- `claude_helpers_test.go:220-227, 248-255`: a missing record file reads as
  "claude never ran". Every caller also asserts an early error, so this is
  theoretical today.
- `claude_helpers_test.go:152`: cleanup kills only gralph, so a failed signal
  test leaves the fake behind (already noted in `CLAUDE.md`).

### Brittle

Would break on a harmless change:

- Byte-exact tables and ANSI escapes: `tests/e2e/claude_loop_test.go:115-116`,
  `dry_run_test.go:44-48, 80-85, 95-99`, `sandbox_test.go:190-195`. The unit
  tests pin the same bytes, so one format change breaks both suites.
- Merged sandbox settings as an exact JSON string (key order):
  `tests/e2e/sandbox_test.go:71, 104`, `internal/looper/sandbox_test.go:33-49`.
- Git's English output with no `LC_ALL` set:
  `internal/looper/commit_test.go:591, 621`; git's `exit status 128` at `:504`.
- `internal/tui/model_test.go:169-173` hardcodes `"\x1b[94m┌"`.
- `internal/tui/setup_test.go:51-55` asserts looper's error wording.
- About 30 call sites pass `runLoop`'s eight positional arguments.

### Redundant

Low cost; listed so nobody adds more of the same:

- `tests/e2e/claude_result_test.go` mirrors the unit table
  `TestRunLoop_ResultLineOutcomes` nearly case for case. One e2e smoke per
  outcome would do.
- `internal/looper/result_test.go:146` and `runloop_result_test.go:18` repeat
  one table at two levels; the second is the more valuable.
- `internal/looper/gates_test.go:398, 402`; `looper_test.go:147-175`.
- `internal/tasks/task_test.go:128`; `cmd/main/main_test.go:132-155` (repeats
  `TestParseTimeout`); `internal/tui/model_test.go:115, 392`.

### Other gaps

- No e2e signal test during a gate or a commit. Unit tests cover the context
  cancel; the signal-to-context wiring (`cmd/main/main.go:62`) is exercised
  only for a session.
- The e2e sandbox fixture overrides only `enabled`. The other two forced keys
  and non-`sandbox` keys passing through are unit-only.
- Tasks: dropping `!!timestamp`/`!!merge` from the allowed tags survived;
  alias handling inside gates has no test; some error rows match short
  substrings, so a wrong `tasks[i] (id n)` prefix would slip through on those.

### Timing

Low flake risk. Waits are ready-file polls with 5s ceilings; the 100-200ms
gate timeouts race `sleep 30`. `go test -count=15 -race ./internal/tui`
passed. Watch points: `commit_test.go:693-694` bounds a 2s grace between 1s
and 6s; `event_test.go:21-24` appends to a slice with no lock;
`setup_test.go:157` relies on the input parser's escape timeout.

### Solid

Killed by a break, or pinned with exact assertions:

- Outcome rule: non-zero exit with a `completed` line, and exit 0 with no
  result line.
- Cancellation leaves the file untouched: during a session (both paths), a
  gate, and the commit.
- State saved after each task; a failure stops the run; completed tasks are
  skipped.
- Gate timeout precedence (flag, gate, default) and the deadline killing the
  gate; the gate unknown-key rule.
- Commit only after the gates pass; git gets SIGTERM before SIGKILL.
- Forced sandbox keys; exact claude argv for both permission flags, on every
  task; the neither/both flag errors.
- The stdin wire contract, including a literal golden at
  `internal/looper/gates_test.go:114`.
- Failed-task refusal: byte-identical file, no session, exit 1.
- Only `y` confirms a stop; `in progress` cannot reach the file; a failed run
  holds the view open.
- Task validation: full error text per rule, check order, state defaulting, no
  trimming or case folding, name and prompt verbatim, gates round-trip.
- The skill hash check.

## 2. Docs drift

The code is the truth. Every link and anchor resolves, and every error string
and output line the README quotes matches the code.

| #   | Doc                                                  | Drift                                                                                         | Kind       |
| --- | ---------------------------------------------------- | --------------------------------------------------------------------------------------------- | ---------- |
| D1  | `CLAUDE.md:32`, `README.md:519-520`, `05:57`         | Say `make install` only copies and can install a stale binary. `Makefile:26` is `install: local`, so it always rebuilds | Wrong |
| D2  | `03:137`                                             | A gate is "a mapping whose only key is `cmd`". `task.go:284` also allows `timeout`            | Wrong      |
| D3  | `03:210`                                             | `Timeout` is a "duration". It is a string (`task.go:27`), parsed at run time                  | Wrong      |
| D4  | `03:60`                                              | The flag list leaves out `--commit`                                                           | Missing    |
| D5  | `03:261, 309, 341`                                   | Sequence diagrams show `Start` and `Run` without the commit/repo argument                     | Stale      |
| D6  | `03:346`, `03:303`                                   | `printTasks` is `PrintTasks`; the CLI calls `LoadTasksReport` via `loadGiven`, not `LoadTasks` | Stale name |
| D7  | `03:33-53, 114, 150-163, 294-327, 356-366`           | `--commit` half threaded: no git node, no commit step in the TUI sequence, no Looper → Git row, "kill" only for process trees | Missing |
| D8  | `01:95`, ADR-012 (`02:400`)                          | Bubble Tea "imported only by `internal/tui` and `cmd/main`". `cmd/main` imports none          | Wrong      |
| D9  | ADR-001, 003, 011 (`02:57, 122, 367-368`), ADR-013 (`02:470`) | Argv given as always `--dangerously-skip-permissions`, with no "amended by ADR-014" note | Contradiction |
| D10 | ADR-009 (`02:313`)                                   | "A stale install is not detected during normal runs". ADR-010 does exactly that               | Contradiction |
| D11 | `05:148-154, 160-167`                                | Test lists predate the sandbox flags, `--commit`, and the skill check                         | Missing    |
| D12 | `gralph-concept.md:12-18`                            | No gates, no `--commit`                                                                       | Missing    |
| D13 | `00:27`                                              | Prompt and task list "both YAML"; the prompt is Markdown                                      | Wrong      |
| D14 | `00:21`                                              | "Production-ready", against the README's Emerging label                                       | Contradiction |

Cosmetic: `05:80` omits `-race`; `05:146` names the wrong fake env var;
`CLAUDE.md`'s unit-fake list omits `FAKE_CLAUDE_BLOCK_ON`; ADR-013 and ADR-014
titles differ between the summary table and their headings; `03:133` omits the
whitespace trim in name uniqueness; `CLAUDE.md`'s `cmd/main` bullet predates
`loadGiven` and `fatal`; the README lists Versioning as a top-level section;
`dependency_graph.md:6` mentions a build-output arrow that is not drawn.

Per doc: `03` has the most drift. `02`'s newest ADRs (013-015) match the code;
the older ones need amend notes. `dependency_graph.md`, `SKILL.md`, and the
templates are clean.

Only D1 was re-checked by hand. The rest are the reviewer's, each with a file
and line on both sides.

## 3. Sandbox placeholder dotfiles

Sessions in the 13-task `--commit` run kept reporting untracked `.bashrc`,
`.gitconfig`, `.mcp.json`, `.claude/`, `src/.claude/`, and similar.

What they are: Claude Code's sandbox mounts `/dev/null` over sensitive paths
so a session cannot write them. Inside the sandbox they are device files owned
by `nobody`.

They reach the real disk. Reproduced with bubblewrap 0.12.0 directly: to mount
over a path that does not exist, it first creates an empty file (or directory)
on the host, and leaves it there when the sandbox exits. A host-side
`git status` then shows `?? .bashrc` and `?? .claude/`.

Why the run was clean: Claude Code appears to remove the stubs after each
command. What is left in this repository is two empty directories,
`.claude/.cc-writes/` and `src/.claude/.cc-writes/`, which git does not see.
None of the 13 commits contain a stub.

Where it can bite `--commit`:

- A session killed mid-command (Ctrl-C kills the whole group) may skip that
  cleanup. The next `--commit` run is then refused as a dirty tree, over files
  the user never made. Loud and safe, but confusing. Not reproduced.
- A session that exits `completed` with stubs still on disk would have them
  committed by `git add -A`. Not seen, and not confirmed possible.
- Neither `.claude/` nor `src/.claude/` is git-ignored here. Anything Claude
  Code leaves in `.cc-writes/` would be committed.

Cost today: 12 of 13 sessions spent output flagging these files; one asked for
them to be checked "before the commit step".

Options, none taken:

1. Document it: a README note under "Committing tasks" naming the files and
   suggesting the ignore lines.
2. Say it in the prompt template, so sessions stop reporting them.
3. Have gralph refuse or skip zero-byte stubs at commit time. This adds an
   exclusion to a commit path that was deliberately left with none (ADR-015),
   so it needs a decision first.

## 4. Stale leftovers

Done on 2026-10-01, uncommitted:

| Item                    | What happened                                                                 |
| ----------------------- | ----------------------------------------------------------------------------- |
| `scripts/ralph.sh`      | Moved to `.archive/ralph.sh`, still tracked. `.archive/README.md` says the directory is historical and not maintained |
| `docs/tui_mock_up.txt`  | Deleted, and added to `.gitignore`. The comment in `internal/tui/model.go` that pointed at it was reworded |
| `docs/superpowers/`     | Deleted (three plans, one spec, all for shipped features), and added to `.gitignore` |
| `docs/gralph-concept.md` | Kept. The README links to it. See D12                                        |
