# `--commit` Flag Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** With `--commit`, gralph commits each task itself after the session reports `completed` and every gate passes, so sessions no longer need a commit rule.

**Architecture:** A new `internal/looper/commit.go` holds a `Repo` type: `OpenRepo` finds the git work tree and enforces the clean-tree rule at startup, and `(*Repo).commit` stages and commits after the gates. A `*Repo` (nil means "never call git") is threaded through `Run` → `runLoop` → `runTask` and `tui.Run`. `Start` and `DryRun` take a `commit bool`; `cmd/main` adds the flag.

**Tech Stack:** Go 1.27, the `git` CLI via `os/exec`, testify, the existing fake `claude` executables.

**Spec:** `docs/architecture/02_architectural_decisions.md`, ADR-015. Read it before starting any task.

## Global Constraints

- The Go module is in `src/`. Run go commands from `src/` or use `make test` / `make analyze` from the repository root.
- Never add `// #nosec` or `//nolint`. Git commands are built by `(*Repo).git`, which creates the command with the constant `"git"` and appends arguments to `cmd.Args` (the same shape `claudeCmd` uses); do not pass variables to `exec.Command` directly.
- Code style applies to every line written or moved: return early, blank line after every `if` block (except before a closing brace or `else`), no long inline callbacks, comments say why.
- Tests use `github.com/stretchr/testify` (`require` for preconditions, `assert` for checks).
- `looper` never imports `tui` or Bubble Tea. Unix only; no Windows branches.
- Without `--commit` gralph never runs git. The stdin contract (`prompt\n\n<task>\n`) does not change.
- Exact strings from the spec:
  - `commit: not a git repository, nothing will be committed`
  - `--commit needs a clean work tree; commit, stash, or remove:` (printed by `cmd/main` after `error: `)
  - `commit failed: <exit error>`
  - plain mode `commit: <name>`; stream path Activity `→ commit <name>` (first line of the name only)
  - dry run `commit: <work tree root>`
- Gralph never pushes, never passes `--no-verify`, never resets, stashes, or cleans.
- YAML files use `.yaml`. No retry, no provider abstraction, no default for the permission flags.
- Each task ends with `make test` and `make analyze` passing before its commit.

## Review Focus

1. **Task file outside the repository** (for example `/tmp/run/tasks.yaml`): git rejects a pathspec that leaves the tree, so the run must still start and commit normally. Test in Task 1 (`TestOpenRepo_TaskFileOutsideTheTree`) and Task 2 (`TestRun_CommitWithTaskFileOutsideTheTree`).
2. **Gralph started from a subdirectory of the repository**: the clean check and the commit must cover the whole work tree, not the subdirectory. Tests in Task 1 (`TestOpenRepo_FromSubdirectoryCoversWholeTree`) and Task 2 (`TestRun_CommitFromSubdirectoryCommitsWholeTree`).
3. **Task file already staged or tracked**: it must stay out of every commit. Test in Task 2 (`TestRun_CommitLeavesStagedTaskFileOut`).
4. **Task file alone in an untracked directory**: must count as clean. Test in Task 1 (`TestOpenRepo_TaskFileNeverCountsAsDirty`).
5. **Odd task names** (multi-line, leading dash): the commit must succeed and messages must show only the first line. Test in Task 2 (`TestRun_CommitOddTaskNames`).

## File Structure

| File | Change | Responsibility |
| --- | --- | --- |
| `src/internal/looper/commit.go` | create | `Repo`, `OpenRepo`, `repoFor`, the commit step |
| `src/internal/looper/commit_test.go` | create | unit tests for all of the above |
| `src/internal/looper/gates.go` | modify | rename `runGateStream` → `runCmdStream` (now shared) |
| `src/internal/looper/looper.go` | modify | thread `*Repo` through `Run`/`runLoop`/`runTask`; `commit bool` on `Start`/`DryRun` |
| `src/internal/tui/run.go` | modify | `Run` takes and forwards `*looper.Repo` |
| `src/cmd/main/main.go` | modify | `--commit` flag and wiring |
| `tests/e2e/claude_helpers_test.go` | modify | `runGralphIn` (working directory) |
| `tests/e2e/commit_test.go` | create | black-box tests |
| `src/skills/gralph-docs-writer/**` | modify | template and skill rules |
| `README.md`, `CLAUDE.md`, `docs/architecture/0{0,1,3}_*.md` | modify | documentation |

---

### Task 0: Branch and commit the design

**Files:** already modified in the working tree: `docs/architecture/02_architectural_decisions.md`, `src/skills/gralph-docs-writer/templates/prompt_template.md`, `.gitignore`; plus this plan.

- [ ] **Step 1: Create the branch**

```bash
git switch -c commit-flag
```

- [ ] **Step 2: Commit**

```bash
git add .gitignore docs/architecture/02_architectural_decisions.md docs/superpowers/plans/2026-09-30-commit-flag.md src/skills/gralph-docs-writer/templates/prompt_template.md
git commit -m "ADR-015: gralph commits a completed task on request"
```

---

### Task 1: `Repo` and the startup check

**Files:**
- Create: `src/internal/looper/commit.go`
- Test: `src/internal/looper/commit_test.go`

**Interfaces:**
- Consumes: `configureProcessTree(cmd *exec.Cmd)` from `process_tree_unix.go`.
- Produces:
  - `type Repo struct` (unexported fields `root string`, `paths []string`)
  - `func OpenRepo(tasksFile string) (*Repo, error)`: `nil, nil` outside a work tree or when git cannot run; an error when the tree is dirty.
  - `func (r *Repo) Root() string`
  - `func (r *Repo) git(ctx context.Context, args ...string) *exec.Cmd`: appends `r.paths` after `args`.
  - Test helpers `initRepo(t) string`, `gitRun(t, dir, args...) string`, `writeFile(t, path, content)`.

- [ ] **Step 1: Write the failing tests**

Create `src/internal/looper/commit_test.go`:

```go
package looper

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateGit keeps a developer's own git setup (hooks, signing, identity)
// out of the tests, and stops git from finding a repository above dir.
func isolateGit(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
}

// tempDir is t.TempDir with symlinks resolved, so it compares equal to the
// path git reports.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// initRepo makes a git repository holding one commit and moves the test into
// it. It uses t.Setenv and t.Chdir, so the calling test must not be parallel.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := tempDir(t)
	isolateGit(t, dir)
	t.Chdir(dir)
	gitRun(t, dir, "init", "-q")
	writeFile(t, filepath.Join(dir, "seed.txt"), "seed\n")
	gitRun(t, dir, "add", "seed.txt")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestOpenRepo_OutsideARepositoryIsNil(t *testing.T) {
	dir := tempDir(t)
	isolateGit(t, dir)
	t.Chdir(dir)

	repo, err := OpenRepo(filepath.Join(dir, "tasks.yaml"))
	require.NoError(t, err)
	assert.Nil(t, repo)
}

func TestOpenRepo_GitNotInstalledIsNil(t *testing.T) {
	dir := initRepo(t)
	t.Setenv("PATH", t.TempDir())

	repo, err := OpenRepo(filepath.Join(dir, "tasks.yaml"))
	require.NoError(t, err)
	assert.Nil(t, repo)
}

func TestOpenRepo_CleanTree(t *testing.T) {
	dir := initRepo(t)

	repo, err := OpenRepo(filepath.Join(dir, "tasks.yaml"))
	require.NoError(t, err)
	require.NotNil(t, repo)
	assert.Equal(t, dir, repo.Root())
}

func TestOpenRepo_DirtyTreeIsRefused(t *testing.T) {
	tests := []struct {
		name     string
		dirty    func(t *testing.T, dir string)
		wantLine string
	}{
		{name: "modified", wantLine: " M seed.txt", dirty: func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "seed.txt"), "changed\n")
		}},
		{name: "staged", wantLine: "A  new.txt", dirty: func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "new.txt"), "new\n")
			gitRun(t, dir, "add", "new.txt")
		}},
		{name: "untracked", wantLine: "?? stray.txt", dirty: func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
		}},
		{name: "untracked beside the task file", wantLine: "?? run/prompt.md", dirty: func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "run", "prompt.md"), "prompt\n")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			tasksPath := filepath.Join(dir, "run", "tasks.yaml")
			writeFile(t, tasksPath, "tasks: []\n")
			tt.dirty(t, dir)

			repo, err := OpenRepo(tasksPath)
			assert.Nil(t, repo)
			require.EqualError(t, err, "--commit needs a clean work tree; commit, stash, or remove:\n"+tt.wantLine)
		})
	}
}

func TestOpenRepo_TaskFileNeverCountsAsDirty(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir, tasksPath string)
	}{
		{name: "untracked in the root", setup: func(*testing.T, string, string) {}},
		{name: "tracked and modified", setup: func(t *testing.T, dir, tasksPath string) {
			gitRun(t, dir, "add", tasksPath)
			gitRun(t, dir, "commit", "-q", "-m", "track tasks")
			writeFile(t, tasksPath, "tasks: [changed]\n")
		}},
		{name: "staged", setup: func(t *testing.T, dir, tasksPath string) {
			gitRun(t, dir, "add", tasksPath)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			tasksPath := filepath.Join(dir, "tasks.yaml")
			writeFile(t, tasksPath, "tasks: []\n")
			tt.setup(t, dir, tasksPath)

			repo, err := OpenRepo(tasksPath)
			require.NoError(t, err)
			assert.NotNil(t, repo)
		})
	}
}

func TestOpenRepo_TaskFileAloneInUntrackedDirIsClean(t *testing.T) {
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "run", "tasks.yaml")
	writeFile(t, tasksPath, "tasks: []\n")

	repo, err := OpenRepo(tasksPath)
	require.NoError(t, err)
	assert.NotNil(t, repo)
}

func TestOpenRepo_RelativeTaskPathIsExcluded(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "run", "tasks.yaml"), "tasks: []\n")

	repo, err := OpenRepo(filepath.Join("run", "tasks.yaml"))
	require.NoError(t, err)
	assert.NotNil(t, repo)
}

func TestOpenRepo_IgnoredFilesAreClean(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"), "run/\n")
	gitRun(t, dir, "add", ".gitignore")
	gitRun(t, dir, "commit", "-q", "-m", "ignore run")
	writeFile(t, filepath.Join(dir, "run", "prompt.md"), "prompt\n")
	tasksPath := filepath.Join(dir, "run", "tasks.yaml")
	writeFile(t, tasksPath, "tasks: []\n")

	repo, err := OpenRepo(tasksPath)
	require.NoError(t, err)
	assert.NotNil(t, repo)
}

func TestOpenRepo_TaskFileOutsideTheTree(t *testing.T) {
	dir := initRepo(t)
	outside := filepath.Join(tempDir(t), "tasks.yaml")
	writeFile(t, outside, "tasks: []\n")

	repo, err := OpenRepo(outside)
	require.NoError(t, err)
	require.NotNil(t, repo)
	assert.Equal(t, dir, repo.Root())
}

func TestOpenRepo_FromSubdirectoryCoversWholeTree(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o750))
	writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
	t.Chdir(sub)

	repo, err := OpenRepo(filepath.Join(dir, "tasks.yaml"))
	assert.Nil(t, repo)
	require.ErrorContains(t, err, "?? stray.txt")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd src && go test ./internal/looper -run TestOpenRepo -count=1`
Expected: build failure, `undefined: OpenRepo`.

- [ ] **Step 3: Write the implementation**

Create `src/internal/looper/commit.go`:

```go
package looper

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is the git work tree a --commit run commits into (ADR-015). A nil
// *Repo means the run never calls git.
type Repo struct {
	root string
	// paths limits every git call to the whole work tree minus the task
	// file, which gralph rewrites after each task and never commits.
	paths []string
}

// Root returns the work tree's top-level directory.
func (r *Repo) Root() string {
	return r.root
}

// OpenRepo finds the git work tree around the working directory for a
// --commit run. Outside a work tree, or when git cannot be run, it returns
// nil and no error: there is nothing to commit, and gralph does not require a
// repository. Inside one, the tree must be clean apart from tasksFile, so
// that each commit holds exactly one task's work.
func OpenRepo(tasksFile string) (*Repo, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, nil
	}

	r := &Repo{root: strings.TrimSpace(string(out)), paths: []string{"--", "."}}
	if rel, ok := r.relative(tasksFile); ok {
		r.paths = append(r.paths, ":(exclude,literal)"+rel)
	}

	// Every untracked file is listed by name; the default collapses a new
	// directory to one entry, which would hide the task file inside it.
	dirty, err := r.git(context.Background(), "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil, fmt.Errorf("--commit: git status: %w", err)
	}

	if len(bytes.TrimSpace(dirty)) > 0 {
		return nil, fmt.Errorf("--commit needs a clean work tree; commit, stash, or remove:\n%s", strings.TrimRight(string(dirty), "\n"))
	}

	return r, nil
}

// relative returns path relative to the work tree's root, or false when it
// lies outside the tree: git rejects a pathspec that leaves the tree, and a
// file out there can never be staged anyway.
func (r *Repo) relative(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}

	// git reports the root with symlinks resolved, so the path must be too.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	rel, err := filepath.Rel(r.root, abs)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}

	return rel, true
}

// git builds a git command over the Repo's paths, run from the root so the
// pathspec means the same wherever gralph was started. Like a gate it gets
// no stdin and its own process group, so a cancel also kills any hook.
func (r *Repo) git(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git")
	cmd.Args = append(cmd.Args, args...)
	cmd.Args = append(cmd.Args, r.paths...)
	cmd.Dir = r.root
	configureProcessTree(cmd)
	return cmd
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd src && go test ./internal/looper -run TestOpenRepo -count=1`
Expected: PASS.

- [ ] **Step 5: Run the project checks**

Run: `make test && make analyze`
Expected: both pass; gosec reports no G204 for `commit.go`.

- [ ] **Step 6: Commit**

```bash
git add src/internal/looper/commit.go src/internal/looper/commit_test.go
git commit -m "looper: find the work tree and require it clean for --commit"
```

---

### Task 2: The commit step, wired into the loop

**Files:**
- Modify: `src/internal/looper/commit.go`
- Modify: `src/internal/looper/gates.go` (rename `runGateStream` → `runCmdStream`, definition near line 120 and its one call near line 108)
- Modify: `src/internal/looper/looper.go` (`Run`, `runLoop`, `runTask`, and `Start`'s call to `Run`)
- Modify: `src/internal/tui/run.go`
- Modify: `src/cmd/main/main.go` (the `tui.Run` call only)
- Modify (call sites only): every `_test.go` under `src/internal/looper` and `src/internal/tui/run_test.go`
- Test: `src/internal/looper/commit_test.go`

**Interfaces:**
- Consumes: `Repo`, `(*Repo).git`, `initRepo`, `gitRun`, `writeFile` from Task 1; `firstLine`, `runCmdStream` from `gates.go`; test helpers `useFakeClaude`, `bypass`, `readSavedTasks`, `recorder`, `activityLines`, `eventKinds`.
- Produces:
  - `func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) error`
  - `func runLoop(ctx context.Context, p string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) error`
  - `func (r *Repo) commit(ctx context.Context, task tasks.Task, report func(Event)) (state, errMsg string, err error)`
  - `tui.Run(ctx, prompt, tl, tasksFile, gateTimeout, sessionArgs, repo *looper.Repo, opts ...tea.ProgramOption)`

- [ ] **Step 1: Change the signatures and fix every call site (no behavior yet)**

In `src/internal/looper/gates.go`, rename `runGateStream` to `runCmdStream` in its definition, its doc comment, and its call in `runGateCmd`. The doc comment becomes:

```go
// runCmdStream runs cmd with each stdout and stderr line reported as an
// Activity event, so nothing reaches gralph's own stdout or stderr. Both
// pipes are drained before Wait, as runTaskStream does. Gates and the commit
// step share it.
```

In `src/internal/looper/looper.go`:

```go
// Run runs the loop over tl. When report is non-nil it receives a copy of
// each task's progress and, last, a RunDone event carrying Run's error;
// claude then runs with stream-json output and its activity is reported
// live. report may be called from more than one goroutine. gateTimeout, when
// not empty, replaces every gate's timeout for this run; it is never saved.
// sessionArgs is as for Start. repo, when not nil, is where each completed
// task is committed.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) error {
	err := runLoop(ctx, prompt, tl, tasksFile, gateTimeout, sessionArgs, repo, report)
```

`runLoop` gains the same `repo *Repo` parameter before `report` and passes it on: `runTask(ctx, p, *task, gateTimeout, sessionArgs, repo, report)`. `runTask` gains `repo *Repo` before `report` (unused until Step 4; name it `_ *Repo` for now if the compiler or linter complains, and restore the name in Step 4). In `Start`, the call becomes `Run(ctx, prompt, tasklist, tasksFile, gateTimeout, sessionArgs, nil, nil)`.

In `src/internal/tui/run.go`:

```go
// Run runs the loop over tl inside the run view and returns the run's exit
// code and one-line summary. ctx is the outside SIGINT/SIGTERM context.
// sessionArgs and repo are as for looper.Run. opts are extra program options,
// for tests.
func Run(ctx context.Context, prompt string, tl *tasks.TaskList, tasksFile, gateTimeout string, sessionArgs []string, repo *looper.Repo, opts ...tea.ProgramOption) (exitCode int, summary string, err error) {
```

and inside it `looper.Run(runCtx, prompt, tl, tasksFile, gateTimeout, sessionArgs, repo, func(e looper.Event) { p.Send(e) })`.

In `src/cmd/main/main.go`, `tui.Run(ctx, prompt, tasklist, tasksPath, *gateTimeoutFlag, session, nil)`.

Fix the test call sites. Every existing call passes no repo, so insert `nil` before the last argument:

```bash
cd src
# runLoop(..., bypass, nil) and Run(..., bypass, <report>)
sed -i -E 's/((runLoop|Run)\(.*), (nil|report|rec\.report)\)/\1, nil, \3)/' internal/looper/*_test.go
go build ./... && go vet ./internal/looper ./internal/tui
```

`go vet` lists any call the pattern missed (a call split across lines, or one whose session argument is not `bypass`); fix those by hand the same way. In `internal/tui/run_test.go` the `Run(ctx, "prompt", &tl, tasksPath, "", looper.BypassArgs(),` call continues on the next line with program options: add `nil,` after `looper.BypassArgs(),`.

- [ ] **Step 2: Verify nothing changed**

Run: `make test`
Expected: PASS, same tests as before.

- [ ] **Step 3: Write the failing tests**

Append to `src/internal/looper/commit_test.go` (add `bytes`, `context`, `io`, `strings`, `time`, and `github.com/twistingmercury/gralph/internal/tasks` to the imports):

```go
// workGate stands in for a session's work: the fake claude changes no files,
// so a gate writes the file the commit should pick up.
func workGate(name string) tasks.Gate {
	return tasks.Gate{Cmd: "echo work > " + name}
}

// openRepo opens the test's repository the way a --commit run does.
func openRepo(t *testing.T, tasksPath string) *Repo {
	t.Helper()
	repo, err := OpenRepo(tasksPath)
	require.NoError(t, err)
	require.NotNil(t, repo)
	return repo
}

// subjects lists the commit subjects, newest first.
func subjects(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Split(strings.TrimSpace(gitRun(t, dir, "log", "--format=%s")), "\n")
}

// headFiles lists the files the newest commit changed.
func headFiles(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Fields(gitRun(t, dir, "show", "--name-only", "--format=", "HEAD"))
}

// installHook writes an executable pre-commit hook.
func installHook(t *testing.T, dir, script string) {
	t.Helper()
	path := filepath.Join(dir, ".git", "hooks", "pre-commit")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700))
}

func TestRun_CommitsOneCommitPerCompletedTask(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "plain"
		if stream {
			name = "stream"
		}

		t.Run(name, func(t *testing.T) {
			useFakeClaude(t)
			dir := initRepo(t)
			tasksPath := filepath.Join(dir, "tasks.yaml")
			tl := &tasks.TaskList{Tasks: []tasks.Task{
				{ID: 1, Name: "First", Prompt: "p1", Gates: []tasks.Gate{workGate("one.txt")}},
				{ID: 2, Name: "Second", Prompt: "p2", Gates: []tasks.Gate{workGate("two.txt")}},
			}}

			var rec recorder
			var report func(Event)
			if stream {
				report = rec.report
			}

			require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "", bypass, openRepo(t, tasksPath), report))

			assert.Equal(t, []string{"Second", "First", "init"}, subjects(t, dir))
			assert.Equal(t, []string{"two.txt"}, headFiles(t, dir))
			assert.Equal(t, "?? tasks.yaml\n", gitRun(t, dir, "status", "--porcelain"), "only the task file may be left uncommitted")

			saved := readSavedTasks(t, tasksPath)
			assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
			assert.Equal(t, tasks.CompletedState, saved.Tasks[1].State)
		})
	}
}

func TestRun_NilRepoNeverCommits(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	tl := gatedTask(workGate("one.txt"))

	require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "", bypass, nil, nil))

	assert.Equal(t, []string{"init"}, subjects(t, dir))
	assert.FileExists(t, filepath.Join(dir, "one.txt"))
}

func TestRun_NothingToCommitStillCompletes(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	tl := gatedTask()

	require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.Equal(t, []string{"init"}, subjects(t, dir), "no empty commit")
	assert.Equal(t, tasks.CompletedState, readSavedTasks(t, tasksPath).Tasks[0].State)
}

func TestRun_FailedGateLeavesChangesUncommitted(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	tl := gatedTask(tasks.Gate{Cmd: "echo work > one.txt; exit 1"})

	err := Run(context.Background(), "prompt", tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil)
	require.ErrorContains(t, err, `gate "echo work > one.txt; exit 1" failed`)

	assert.Equal(t, []string{"init"}, subjects(t, dir))
	assert.FileExists(t, filepath.Join(dir, "one.txt"), "a failed task's changes stay in the work tree")
}

func TestRun_SessionFailureSkipsCommit(t *testing.T) {
	useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"nope"}`)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	repo := openRepo(t, tasksPath)
	writeFile(t, filepath.Join(dir, "left.txt"), "left by the session\n")

	err := Run(context.Background(), "prompt", gatedTask(), tasksPath, "", bypass, repo, nil)
	require.ErrorContains(t, err, "nope")
	assert.Equal(t, []string{"init"}, subjects(t, dir))
}

func TestRun_CommitFailureFailsTaskAndStopsRun(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	installHook(t, dir, "echo hook-says-no >&2\nexit 1")
	recordPath := filepath.Join(tempDir(t), "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	tl := &tasks.TaskList{Tasks: []tasks.Task{
		{ID: 1, Name: "First", Prompt: "p1", Gates: []tasks.Gate{workGate("one.txt")}},
		{ID: 2, Name: "Second", Prompt: "p2"},
	}}

	const wantErr = "commit failed: exit status 1"
	err := Run(context.Background(), "prompt", tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil)
	require.EqualError(t, err, "task 1: First failed: "+wantErr)

	assert.Equal(t, []string{"init"}, subjects(t, dir))
	assert.Len(t, readFakeClaudeRecords(t, recordPath), 1, "the second task must never run")

	saved := readSavedTasks(t, tasksPath)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, wantErr, saved.Tasks[0].Error)
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State)
}

func TestRun_CommitLeavesStagedTaskFileOut(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	writeFile(t, tasksPath, "tasks: []\n")
	gitRun(t, dir, "add", "tasks.yaml")

	require.NoError(t, Run(context.Background(), "prompt", gatedTask(workGate("one.txt")), tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.Equal(t, []string{"one.txt"}, headFiles(t, dir), "the task file must stay out of the commit even when staged")
}

func TestRun_CommitWithTaskFileOutsideTheTree(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(tempDir(t), "tasks.yaml")

	require.NoError(t, Run(context.Background(), "prompt", gatedTask(workGate("one.txt")), tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.Equal(t, []string{"First", "init"}, subjects(t, dir))
	assert.Empty(t, gitRun(t, dir, "status", "--porcelain"))
}

func TestRun_CommitFromSubdirectoryCommitsWholeTree(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o750))
	writeFile(t, filepath.Join(sub, "keep.txt"), "keep\n")
	gitRun(t, dir, "add", "sub/keep.txt")
	gitRun(t, dir, "commit", "-q", "-m", "add sub")
	t.Chdir(sub)
	tasksPath := filepath.Join(dir, "tasks.yaml")

	// The gate runs in sub and writes one file there and one in the root.
	tl := gatedTask(tasks.Gate{Cmd: "echo a > here.txt; echo b > ../top.txt"})
	require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.ElementsMatch(t, []string{"sub/here.txt", "top.txt"}, headFiles(t, dir))
}

func TestRun_CommitOddTaskNames(t *testing.T) {
	tests := []struct {
		name        string
		taskName    string
		wantSubject string
		wantLine    string
	}{
		{name: "multi-line", taskName: "Subject line\n\nBody text", wantSubject: "Subject line", wantLine: "→ commit Subject line"},
		{name: "leading dash", taskName: "--amend the widget", wantSubject: "--amend the widget", wantLine: "→ commit --amend the widget"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			dir := initRepo(t)
			tasksPath := filepath.Join(dir, "tasks.yaml")
			tl := &tasks.TaskList{Tasks: []tasks.Task{{ID: 1, Name: tt.taskName, Prompt: "p", Gates: []tasks.Gate{workGate("one.txt")}}}}

			var rec recorder
			require.NoError(t, Run(context.Background(), "prompt", tl, tasksPath, "", bypass, openRepo(t, tasksPath), rec.report))

			assert.Equal(t, []string{tt.wantSubject, "init"}, subjects(t, dir))
			assert.Contains(t, activityLines(rec.snapshot()), tt.wantLine)
		})
	}
}

func TestRun_PlainPrintsCommitLineAndGitOutput(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	repo := openRepo(t, tasksPath)

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := Run(context.Background(), "prompt", gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, nil)

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	require.NoError(t, runErr)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "commit: First\n")
	assert.Contains(t, buf.String(), "1 file changed", "git's own output passes through")
}

func TestRun_StreamReportsCommitAsActivity(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(dir, "tasks.yaml")
	repo := openRepo(t, tasksPath)

	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	outCh := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		outCh <- string(out)
	}()

	var rec recorder
	runErr := Run(context.Background(), "prompt", gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, rec.report)
	os.Stdout = orig
	require.NoError(t, w.Close())
	require.NoError(t, runErr)
	assert.Empty(t, <-outCh, "the stream path must write nothing to gralph's stdout, the commit included")

	events := rec.snapshot()
	assert.Contains(t, activityLines(events), "→ commit First")
	assert.Contains(t, strings.Join(activityLines(events), "\n"), "1 file changed")

	kinds := eventKinds(events)
	require.GreaterOrEqual(t, len(kinds), 2)
	assert.Equal(t, []EventKind{TaskFinished, RunDone}, kinds[len(kinds)-2:], "TaskFinished must follow the commit's activity")
}

func TestRun_CancelDuringCommitLeavesFileUntouched(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	readyPath := filepath.Join(tempDir(t), "ready")
	installHook(t, dir, "touch '"+readyPath+"'\nsleep 60")
	tasksPath := filepath.Join(dir, "tasks.yaml")
	tasksYAML := "tasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	writeFile(t, tasksPath, tasksYAML)
	repo := openRepo(t, tasksPath)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, "prompt", gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, nil)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the commit hook never started")

	cancel()

	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "task 1: First failed: commit")
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a cancel during the commit; git's process group was not killed")
	}

	got, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, tasksYAML, string(got), "the tasks file must be byte-for-byte unchanged after a cancel during the commit")
	assert.Equal(t, []string{"init"}, subjects(t, dir))
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd src && go test ./internal/looper -run 'TestRun_.*Commit|TestRun_NothingToCommit|TestRun_NilRepo' -count=1`
Expected: FAIL. `TestRun_CommitsOneCommitPerCompletedTask` reports subjects `[init]`; `TestRun_NilRepoNeverCommits` passes already.

- [ ] **Step 5: Write the implementation**

Append to `src/internal/looper/commit.go` (add `errors`, `os`, and `github.com/twistingmercury/gralph/internal/tasks` to the imports):

```go
// commit commits what task's session and gates left in the work tree, with
// the task's name as the message, and returns the task's final outcome. err
// is non-nil only when ctx was cancelled, which must leave the task's state
// and the tasks file untouched.
func (r *Repo) commit(ctx context.Context, task tasks.Task, report func(Event)) (state, errMsg string, err error) {
	runErr := r.commitChanges(ctx, task, report)
	if runErr == nil {
		return tasks.CompletedState, "", nil
	}

	if ctx.Err() != nil {
		return "", "", fmt.Errorf("task %d: %s failed: commit: %w", task.ID, task.Name, runErr)
	}

	return tasks.FailedState, fmt.Sprintf("commit failed: %s", runErr), nil
}

// commitChanges stages everything and commits it. A task that changed
// nothing is not an error and gets no empty commit; that also covers a
// session that committed by itself.
func (r *Repo) commitChanges(ctx context.Context, task tasks.Task, report func(Event)) error {
	if err := runGit(r.git(ctx, "add", "-A"), task, report); err != nil {
		return err
	}

	staged, err := r.staged(ctx)
	if err != nil || !staged {
		return err
	}

	announceCommit(task, report)
	return runGit(r.git(ctx, "commit", "-m", task.Name), task, report)
}

// staged reports whether the index holds anything to commit. git diff
// --quiet exits 1 to say "yes", which is an answer and not a failure.
func (r *Repo) staged(ctx context.Context) (bool, error) {
	err := r.git(ctx, "diff", "--cached", "--quiet").Run()

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return true, nil
	}

	return false, err
}

// announceCommit names the commit where the mode shows progress. Only the
// name's first line, so a multi-line name stays one line.
func announceCommit(task tasks.Task, report func(Event)) {
	name := firstLine(task.Name)
	if report != nil {
		report(Event{Kind: Activity, Task: task, Line: "→ commit " + name})
		return
	}

	fmt.Printf("commit: %s\n", name)
}

// runGit runs cmd with its output where the mode puts a gate's: straight
// through in plain mode, as Activity lines on the stream path.
func runGit(cmd *exec.Cmd, task tasks.Task, report func(Event)) error {
	if report != nil {
		return runCmdStream(cmd, task, report)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

In `src/internal/looper/looper.go`, replace `runTask`:

```go
// runTask runs one task's session on the path report selects, then, only
// while the task is still completed, its gates and the commit into repo. It
// returns the task's outcome, or an error when ctx was cancelled.
func runTask(ctx context.Context, p string, task tasks.Task, gateTimeout string, sessionArgs []string, repo *Repo, report func(Event)) (state, errMsg string, err error) {
	if report == nil {
		state, errMsg, err = runTaskPlain(ctx, p, task, sessionArgs)
	} else {
		state, errMsg, err = runTaskStream(ctx, p, task, sessionArgs, report)
	}

	if err != nil || state != tasks.CompletedState {
		return state, errMsg, err
	}

	state, errMsg, err = runGates(ctx, task, gateTimeout, report)
	if err != nil || state != tasks.CompletedState || repo == nil {
		return state, errMsg, err
	}

	return repo.commit(ctx, task, report)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd src && go test ./internal/looper ./internal/tui ./cmd/... -count=1`
Expected: PASS.

- [ ] **Step 7: Run the project checks**

Run: `make test && make analyze`
Expected: both pass.

- [ ] **Step 8: Commit**

```bash
git add src/internal/looper src/internal/tui src/cmd/main/main.go
git commit -m "looper: commit each completed task into the run's work tree"
```

---

### Task 3: The `--commit` flag, `Start`, and `DryRun`

**Files:**
- Modify: `src/internal/looper/commit.go` (add `repoFor`)
- Modify: `src/internal/looper/looper.go` (`Start`, `DryRun`)
- Modify: `src/cmd/main/main.go`
- Modify (call sites): `src/internal/looper/looper_test.go`
- Test: `src/internal/looper/commit_test.go`

**Interfaces:**
- Consumes: `OpenRepo`, `(*Repo).Root`, `Run` with `repo` from Tasks 1 and 2.
- Produces:
  - `func Start(ctx context.Context, promptFile, tasksFile, gateTimeout string, sessionArgs []string, commit bool) error`
  - `func DryRun(w io.Writer, tasksFile, gateTimeout, sandboxFile string, commit bool) error`
  - `func repoFor(w io.Writer, tasksFile string, commit bool) (*Repo, error)`
  - CLI flag `--commit`

- [ ] **Step 1: Change the signatures and fix the call sites**

Give `Start` a trailing `commit bool` and `DryRun` a trailing `commit bool` (unused for now), update `main.go`'s two calls to pass `false`, then:

```bash
cd src
sed -i -E 's/(Start\(.*), bypass\)/\1, bypass, false)/; s/(DryRun\(&out, .*)\)\)/\1, false))/; s/(:= DryRun\(&out, .*)\)$/\1, false)/' internal/looper/looper_test.go
go vet ./...
```

Fix by hand anything `go vet` still reports. Run `make test`; expected PASS.

- [ ] **Step 2: Write the failing tests**

Append to `src/internal/looper/commit_test.go`:

```go
const validTasks = "tasks:\n  - {id: 1, name: First, prompt: p1}\n"

func TestStart_CommitRefusesDirtyTreeBeforeAnySession(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	recordPath := filepath.Join(tempDir(t), "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
	run := tempDir(t)
	promptPath, tasksPath := filepath.Join(run, "prompt.md"), filepath.Join(run, "tasks.yaml")
	writeFile(t, promptPath, "prompt\n")
	writeFile(t, tasksPath, validTasks)

	err := Start(context.Background(), promptPath, tasksPath, "", bypass, true)
	require.EqualError(t, err, "--commit needs a clean work tree; commit, stash, or remove:\n?? stray.txt")

	assert.Empty(t, readFakeClaudeRecords(t, recordPath), "claude must not run")
	got, readErr := os.ReadFile(tasksPath)
	require.NoError(t, readErr)
	assert.Equal(t, validTasks, string(got), "the tasks file must be untouched")
}

func TestStart_CommitFlagCommitsTheTask(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	run := tempDir(t)
	promptPath, tasksPath := filepath.Join(run, "prompt.md"), filepath.Join(run, "tasks.yaml")
	writeFile(t, promptPath, "prompt\n")
	writeFile(t, tasksPath, "tasks:\n  - id: 1\n    name: First\n    prompt: p1\n    gates:\n      - cmd: echo work > one.txt\n")

	require.NoError(t, Start(context.Background(), promptPath, tasksPath, "", bypass, true))
	assert.Equal(t, []string{"First", "init"}, subjects(t, dir))
}

func TestStart_WithoutCommitFlagIgnoresADirtyTree(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
	run := tempDir(t)
	promptPath, tasksPath := filepath.Join(run, "prompt.md"), filepath.Join(run, "tasks.yaml")
	writeFile(t, promptPath, "prompt\n")
	writeFile(t, tasksPath, validTasks)

	require.NoError(t, Start(context.Background(), promptPath, tasksPath, "", bypass, false))
	assert.Equal(t, []string{"init"}, subjects(t, dir))
}

func TestDryRun_Commit(t *testing.T) {
	t.Run("clean repository names the work tree", func(t *testing.T) {
		dir := initRepo(t)
		tasksPath := filepath.Join(tempDir(t), "tasks.yaml")
		writeFile(t, tasksPath, validTasks)

		var out bytes.Buffer
		require.NoError(t, DryRun(&out, tasksPath, "", "", true))
		assert.True(t, strings.HasSuffix(out.String(), "commit: "+dir+"\n"+tasksPath+" is valid\n"), out.String())
	})

	t.Run("dirty repository is an error", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
		tasksPath := filepath.Join(tempDir(t), "tasks.yaml")
		writeFile(t, tasksPath, validTasks)

		var out bytes.Buffer
		err := DryRun(&out, tasksPath, "", "", true)
		require.ErrorContains(t, err, "--commit needs a clean work tree")
		assert.NotContains(t, out.String(), "is valid")
	})

	t.Run("outside a repository says nothing will be committed", func(t *testing.T) {
		dir := tempDir(t)
		isolateGit(t, dir)
		t.Chdir(dir)
		tasksPath := filepath.Join(dir, "tasks.yaml")
		writeFile(t, tasksPath, validTasks)

		var out bytes.Buffer
		require.NoError(t, DryRun(&out, tasksPath, "", "", true))
		assert.True(t, strings.HasSuffix(out.String(), "commit: not a git repository, nothing will be committed\n"+tasksPath+" is valid\n"), out.String())
	})

	t.Run("without the flag git is never consulted", func(t *testing.T) {
		dir := initRepo(t)
		writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
		tasksPath := filepath.Join(tempDir(t), "tasks.yaml")
		writeFile(t, tasksPath, validTasks)

		var out bytes.Buffer
		require.NoError(t, DryRun(&out, tasksPath, "", "", false))
		assert.NotContains(t, out.String(), "commit:")
	})
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd src && go test ./internal/looper -run 'TestStart_Commit|TestStart_WithoutCommit|TestDryRun_Commit' -count=1`
Expected: FAIL (`TestStart_CommitRefusesDirtyTreeBeforeAnySession` gets no error; the dry-run output has no `commit:` line).

- [ ] **Step 4: Write the implementation**

Append to `src/internal/looper/commit.go` (add `io` to the imports):

```go
// repoFor opens the work tree for a run started with --commit; without the
// flag it returns nil. When there is no repository it says so on w, so a run
// that commits nothing is not mistaken for one that did.
func repoFor(w io.Writer, tasksFile string, commit bool) (*Repo, error) {
	if !commit {
		return nil, nil
	}

	repo, err := OpenRepo(tasksFile)
	if err == nil && repo == nil {
		_, _ = fmt.Fprintln(w, "commit: not a git repository, nothing will be committed")
	}

	return repo, err
}
```

In `src/internal/looper/looper.go`, `Start` becomes:

```go
// Start runs the loop in plain mode. gateTimeout is the --gate-timeout value as
// given, or "" when the flag was not passed. sessionArgs is the claude flags
// that set what a session may do: SandboxArgs or BypassArgs. commit is the
// --commit flag: each completed task is committed when gralph runs in a git
// work tree, which must then be clean.
func Start(ctx context.Context, promptFile, tasksFile, gateTimeout string, sessionArgs []string, commit bool) error {
	prompt, err := LoadPrompt(promptFile)
	if err != nil {
		return fmt.Errorf("failed to start loop runner: %w", err)
	}

	tasklist, err := LoadTasksReport(os.Stdout, tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		return err
	}

	if err != nil {
		return fmt.Errorf("failed to start loop runner: %w", err)
	}

	repo, err := repoFor(os.Stdout, tasksFile, commit)
	if err != nil {
		return err
	}

	if err := Run(ctx, prompt, tasklist, tasksFile, gateTimeout, sessionArgs, repo, nil); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}
```

`DryRun` becomes:

```go
// DryRun validates tasksFile with the same checks Start uses and reports on
// it to w without launching claude or writing any file. It also lists the
// timeout each gate would run under, given gateTimeout as for Start, names
// sandboxFile, when not empty, as the settings a run would use, and, when
// commit is set, applies Start's work tree check and names the tree.
func DryRun(w io.Writer, tasksFile, gateTimeout, sandboxFile string, commit bool) error {
	tasklist, err := LoadTasksReport(w, tasksFile)
	if errors.Is(err, ErrFailedTasks) {
		return nil
	}

	if err != nil {
		return err
	}

	PrintTasks(w, tasklist)
	printGateLimits(w, tasklist, gateTimeout)
	if sandboxFile != "" {
		_, _ = fmt.Fprintf(w, "sandbox settings: %s\n", sandboxFile)
	}

	repo, err := repoFor(w, tasksFile, commit)
	if err != nil {
		return err
	}

	if repo != nil {
		_, _ = fmt.Fprintf(w, "commit: %s\n", repo.Root())
	}

	_, _ = fmt.Fprintf(w, "%s is valid\n", tasksFile)
	return nil
}
```

In `src/cmd/main/main.go`:

Add to the flag block:

```go
	commitFlag      = pflag.Bool("commit", false, "Commit each completed task with git after its gates pass, with the task name as the message; in a repository, the work tree must be clean")
```

Pass the flag: `looper.DryRun(os.Stdout, *tasksFlag, *gateTimeoutFlag, *sandboxFlag, *commitFlag)` and `looper.Start(ctx, *promptFlag, *tasksFlag, *gateTimeoutFlag, session, *commitFlag)`.

In `runTUI`, between the setup block and the `tui.Run` call, add:

```go
	// The view has nowhere to print plain mode's "not a git repository"
	// notice, so the work tree is opened without it.
	var repo *looper.Repo
	if *commitFlag {
		var err error
		if repo, err = looper.OpenRepo(tasksPath); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}
```

and change the call to `tui.Run(ctx, prompt, tasklist, tasksPath, *gateTimeoutFlag, session, repo)`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd src && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 6: Try the binary**

```bash
make local
.bin/local/gralph --help 2>&1 | grep -- --commit
```

Expected: the `--commit` line is listed.

- [ ] **Step 7: Run the project checks**

Run: `make test && make analyze`
Expected: both pass.

- [ ] **Step 8: Commit**

```bash
git add src/internal/looper src/cmd/main/main.go
git commit -m "Add --commit: plain mode, the TUI, and --dry-run"
```

---

### Task 4: End-to-end tests

**Files:**
- Modify: `tests/e2e/claude_helpers_test.go` (`runGralph` → delegates to new `runGralphIn`)
- Create: `tests/e2e/commit_test.go`

**Interfaces:**
- Consumes: the built binary with `--commit`; helpers `gralphEnv`, `fakeClaudeDir`, `writePrompt`, `writeTasksYAML`, `readTasksYAML`, `taskStates`, `taskErrors`, `countAttempts`, `runGralph`.
- Produces: `runGralphIn(t, dir, timeout, args, env) gralphResult`.

- [ ] **Step 1: Add the working-directory helper**

In `tests/e2e/claude_helpers_test.go`, rename the body of `runGralph` to `runGralphIn` with a `dir string` parameter after `t`, set `cmd.Dir = dir` right after `cmd.Env = env`, and keep `runGralph` as:

```go
// runGralph runs the gralph binary to completion with the given args and
// environment, enforcing timeout as a hang guard.
func runGralph(t *testing.T, timeout time.Duration, args []string, env []string) gralphResult {
	t.Helper()
	return runGralphIn(t, "", timeout, args, env)
}

// runGralphIn is runGralph started from dir; "" keeps the test process's
// working directory.
func runGralphIn(t *testing.T, dir string, timeout time.Duration, args []string, env []string) gralphResult {
```

- [ ] **Step 2: Write the tests**

Create `tests/e2e/commit_test.go`:

```go
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers --commit (ADR-015): gralph commits each task itself after
// the session reports completed and every gate passes. The fake claude
// changes no files, so a gate does the "work" a commit picks up.

const dirtyTreeError = "error: --commit needs a clean work tree; commit, stash, or remove:"

// gitEnv keeps the host's git setup out of the test and stops git from
// finding a repository above dir.
func gitEnv(dir string) map[string]string {
	return map[string]string{
		"GIT_CONFIG_GLOBAL":       os.DevNull,
		"GIT_CONFIG_NOSYSTEM":     "1",
		"GIT_AUTHOR_NAME":         "Test",
		"GIT_AUTHOR_EMAIL":        "test@example.com",
		"GIT_COMMITTER_NAME":      "Test",
		"GIT_COMMITTER_EMAIL":     "test@example.com",
		"GIT_CEILING_DIRECTORIES": filepath.Dir(dir),
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range gitEnv(dir) {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// commitRepo is a git repository with one commit that ignores run/, where
// the prompt and task file live, as the README recommends.
type commitRepo struct {
	dir        string
	promptPath string
	tasksPath  string
	attemptLog string
	env        []string
}

func newCommitRepo(t *testing.T, tasksYAML string) commitRepo {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	gitIn(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("run/\n"), 0o600))
	gitIn(t, dir, "add", ".gitignore")
	gitIn(t, dir, "commit", "-q", "-m", "init")

	run := filepath.Join(dir, "run")
	require.NoError(t, os.Mkdir(run, 0o750))
	repo := commitRepo{
		dir:        dir,
		promptPath: writePrompt(t, run, "Body.\n"),
		tasksPath:  writeTasksYAML(t, run, tasksYAML),
		attemptLog: filepath.Join(run, "attempts.log"),
	}

	extra := gitEnv(dir)
	extra["FAKECLAUDE_ATTEMPT_LOG_FILE"] = repo.attemptLog
	repo.env = gralphEnv(fakeClaudeDir, extra)
	return repo
}

func (r commitRepo) run(t *testing.T, flags ...string) gralphResult {
	t.Helper()
	args := append([]string{"--skip-permissions", "--prompt=" + r.promptPath, "--tasks=" + r.tasksPath}, flags...)
	return runGralphIn(t, r.dir, 20*time.Second, args, r.env)
}

func (r commitRepo) subjects(t *testing.T) []string {
	t.Helper()
	return strings.Split(strings.TrimSpace(gitIn(t, r.dir, "log", "--format=%s")), "\n")
}

const twoWorkTasks = `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: echo one > one.txt
  - id: 2
    name: Second task
    prompt: Do the second thing.
    gates:
      - cmd: echo two > two.txt
`

func TestCommit_OneCommitPerCompletedTask(t *testing.T) {
	t.Parallel()
	repo := newCommitRepo(t, twoWorkTasks)

	res := repo.run(t, "--commit")

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, []string{"Second task", "First task", "init"}, repo.subjects(t))
	assert.Equal(t, "two.txt\n", gitIn(t, repo.dir, "show", "--name-only", "--format=", "HEAD"))
	assert.Empty(t, gitIn(t, repo.dir, "status", "--porcelain"), "nothing may be left uncommitted")
	assert.Contains(t, res.stdout, "commit: First task\n")
	assert.Contains(t, res.stdout, "commit: Second task\n")

	after := readTasksYAML(t, repo.tasksPath)
	assert.Equal(t, "completed", taskStates(after)[1])
	assert.Equal(t, "completed", taskStates(after)[2])
}

func TestCommit_WithoutTheFlagGitIsNeverTouched(t *testing.T) {
	t.Parallel()
	repo := newCommitRepo(t, twoWorkTasks)
	require.NoError(t, os.WriteFile(filepath.Join(repo.dir, "stray.txt"), []byte("stray\n"), 0o600))

	res := repo.run(t)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, []string{"init"}, repo.subjects(t))
	assert.NotContains(t, res.stdout, "commit:")
}

func TestCommit_DirtyTreeRefusesToStart(t *testing.T) {
	t.Parallel()
	repo := newCommitRepo(t, twoWorkTasks)
	require.NoError(t, os.WriteFile(filepath.Join(repo.dir, "stray.txt"), []byte("stray\n"), 0o600))

	res := repo.run(t, "--commit")

	require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, dirtyTreeError+"\n?? stray.txt\n")
	assert.Equal(t, 0, countAttempts(t, repo.attemptLog), "claude must not run")

	raw, err := os.ReadFile(repo.tasksPath)
	require.NoError(t, err)
	assert.Equal(t, twoWorkTasks, string(raw), "the tasks file must be untouched")
}

func TestCommit_FailedGateLeavesNoCommitAndBlocksTheRerun(t *testing.T) {
	t.Parallel()
	tasksYAML := `tasks:
  - id: 1
    name: First task
    prompt: Do the first thing.
    gates:
      - cmd: echo one > one.txt; exit 1
`
	repo := newCommitRepo(t, tasksYAML)

	res := repo.run(t, "--commit")

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, []string{"init"}, repo.subjects(t))
	assert.FileExists(t, filepath.Join(repo.dir, "one.txt"), "the failed task's work stays in the tree")
	assert.Equal(t, "failed", taskStates(readTasksYAML(t, repo.tasksPath))[1])

	// A person resets the task to pending without cleaning up: the next
	// --commit run must refuse the leftovers.
	require.NoError(t, os.WriteFile(repo.tasksPath, []byte(tasksYAML), 0o600))
	again := repo.run(t, "--commit")

	require.Equal(t, 1, again.exitCode, "stdout:\n%s\nstderr:\n%s", again.stdout, again.stderr)
	assert.Contains(t, again.stderr, dirtyTreeError+"\n?? one.txt\n")
	assert.Equal(t, 1, countAttempts(t, repo.attemptLog), "the refused run must not invoke claude")
}

func TestCommit_RejectedCommitFailsTheTask(t *testing.T) {
	t.Parallel()
	repo := newCommitRepo(t, twoWorkTasks)
	hook := filepath.Join(repo.dir, ".git", "hooks", "pre-commit")
	require.NoError(t, os.MkdirAll(filepath.Dir(hook), 0o750))
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho hook-says-no >&2\nexit 1\n"), 0o700))

	res := repo.run(t, "--commit")

	require.NotEqual(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "hook-says-no")
	assert.Contains(t, res.stderr, "task 1: First task failed: commit failed: exit status 1")
	assert.Equal(t, []string{"init"}, repo.subjects(t))

	after := readTasksYAML(t, repo.tasksPath)
	assert.Equal(t, "commit failed: exit status 1", taskErrors(after)[1])
	assert.Equal(t, "pending", taskStates(after)[2])
	assert.Equal(t, 1, countAttempts(t, repo.attemptLog), "the run must stop before the second task")
}

func TestCommit_OutsideARepositoryRunsWithoutCommitting(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	promptPath := writePrompt(t, dir, "Body.\n")
	tasksPath := writeTasksYAML(t, dir, twoWorkTasks)
	env := gralphEnv(fakeClaudeDir, gitEnv(dir))

	res := runGralphIn(t, dir, 20*time.Second, []string{"--skip-permissions", "--commit", "--prompt=" + promptPath, "--tasks=" + tasksPath}, env)

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, "commit: not a git repository, nothing will be committed\n")
	assert.NoDirExists(t, filepath.Join(dir, ".git"))
	assert.Equal(t, "completed", taskStates(readTasksYAML(t, tasksPath))[2])
}

func TestCommit_DryRun(t *testing.T) {
	t.Parallel()

	t.Run("clean", func(t *testing.T) {
		t.Parallel()
		repo := newCommitRepo(t, twoWorkTasks)

		res := runGralphIn(t, repo.dir, 15*time.Second, []string{"-t", repo.tasksPath, "--dry-run", "--commit"}, repo.env)

		require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
		assert.True(t, strings.HasSuffix(res.stdout, "commit: "+repo.dir+"\n"+repo.tasksPath+" is valid\n"), res.stdout)
		assert.Equal(t, []string{"init"}, repo.subjects(t))
	})

	t.Run("dirty", func(t *testing.T) {
		t.Parallel()
		repo := newCommitRepo(t, twoWorkTasks)
		require.NoError(t, os.WriteFile(filepath.Join(repo.dir, "stray.txt"), []byte("stray\n"), 0o600))

		res := runGralphIn(t, repo.dir, 15*time.Second, []string{"-t", repo.tasksPath, "--dry-run", "--commit"}, repo.env)

		require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
		assert.Contains(t, res.stderr, dirtyTreeError+"\n?? stray.txt\n")
		assert.NotContains(t, res.stdout, "is valid")
	})
}
```

- [ ] **Step 3: Run the new tests natively (debugging shortcut)**

```bash
make local && cd tests/e2e && GRALPH_BINARY=$PWD/../../.bin/local/gralph go test -run 'TestCommit_' -count=1 .
```

Expected: PASS.

- [ ] **Step 4: Run the whole build, which runs the e2e suite in its container**

Run: `make build`
Expected: lint, scanners, unit tests, and the e2e suite all pass.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/claude_helpers_test.go tests/e2e/commit_test.go
git commit -m "e2e: cover --commit"
```

---

### Task 5: The skill and its prompt template

**Files:**
- Modify: `src/skills/gralph-docs-writer/templates/prompt_template.md`
- Modify: `src/skills/gralph-docs-writer/SKILL.md`
- Modify: `src/skills/gralph-docs-writer/templates/tasks_template.yaml` (header comment only)

**Interfaces:**
- Consumes: the behavior from Tasks 1–3.
- Produces: a template with no commit-ordering rule, and skill steps that ask about `--commit`.

- [ ] **Step 1: Edit the prompt template**

In `prompt_template.md`, rename the commit token and drop the ordering bullet. The `## Project` list's last line becomes:

```markdown
- Commits: GENERATE_COMMIT_RULES_OR_DO_NOT_COMMIT
```

The `## Rules` list becomes:

```markdown
- Do only this task, within its stated scope.
- Never edit or commit `tasks.yaml`.
- Preserve unrelated changes in the workspace and Git.
- Clean up anything you created that the task does not keep.
```

In `## Finish`, replace "and what was committed" with "and anything you committed", and replace "and any required commit all succeeded" with "and any commit the rules above require all succeeded".

- [ ] **Step 2: Edit SKILL.md**

In "How Gralph uses the pair", add this paragraph after the gates paragraph:

```markdown
With the `--commit` flag, Gralph also commits each task itself: once the
session reported `completed` and every gate passed, it commits everything the
task changed except `tasks.yaml`, with the task's `name` as the commit message.
A failed task is not committed; its changes stay in the work tree for a person
to sort out. Such a run needs a clean Git work tree to start, so the pair must
be ignored by Git or kept outside the repository. Without the flag, Gralph
never touches Git.
```

In "Generate the pair", extend step 3 with these sentences at its end:

```markdown
   Then ask whether the run will use `gralph --commit`. If it will, the
   session must not commit: the `Commits` line in `prompt.md` reads "Do not
   commit. Leave your changes in the work tree.", task names are written as
   commit subjects (imperative, under about 70 characters), and no task prompt
   mentions committing.
```

In step 7, replace the sentence "Keep commits conditional on the project's authorization." with:

```markdown
   For a `--commit` run the `Commits` line tells the session not to commit;
   otherwise keep commits conditional on the project's authorization.
```

Extend step 9 with:

```markdown
   For a `--commit` run, also tell the user the `.gitignore` line that keeps
   the pair out of Git (the output directory, or `tasks.yaml` and `prompt.md`),
   unless the files are outside the repository or already ignored. Do not edit
   `.gitignore` yourself unless asked.
```

In "Task scope", replace "Honor project policies for source commits, and never instruct the session to edit or commit `tasks.yaml`." with:

```markdown
Honor project policies for source commits unless the run uses `--commit`, where
Gralph does the committing, and never instruct the session to edit or commit
`tasks.yaml`.
```

- [ ] **Step 3: Check the tasks template header still matches**

In `tasks_template.yaml`, the header line "the shared prompt.md already covers scope, verification, cleanup, commit, and task-file rules" stays true (the `Commits` line and the Finish section cover commits). Change nothing unless the wording no longer matches the template after Step 1.

- [ ] **Step 4: Verify**

```bash
grep -rn "GENERATE_AUTHORIZED_COMMIT_RULES\|after the task's verification passes" src/skills && echo "STALE" || echo "clean"
make test
```

Expected: `clean`, and tests pass (the skill hash changes; `skillinstall` tests compute it from the embedded files).

- [ ] **Step 5: Commit**

```bash
git add src/skills/gralph-docs-writer
git commit -m "skill: gralph commits on --commit runs; drop the session's commit-order rule"
```

---

### Task 6: Documentation

Hand this task to the technical writer. Edit architecture docs in place and bump their `Version`/`Date`/`Notes`; do not create `_vNN` copies.

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/architecture/00_overview.md`, `01_requirements.md`, `03_system_architecture.md`

**Interfaces:**
- Consumes: the shipped behavior and ADR-015. Describe only what the code does.

- [ ] **Step 1: README**

Add a row to the flag table after `--gate-timeout`:

```markdown
| `--commit`           | No                                                                      | Commit each completed task with git, after its gates pass                             |
```

Add a section after "Gates" and before "The full-screen view":

```markdown
### Committing tasks

Pass `--commit` and gralph commits each task for you. The order for a task is
session, gates, commit: the commit happens only after the session reported
`completed` and every gate passed. That's why the session shouldn't commit
its own work: it would be committing before the gates ran.

- The commit holds everything the task changed, except the task file. The
  message is the task's `name`.
- A task that changed nothing gets no commit and is still `completed`.
- If git refuses the commit (a pre-commit hook fails, say), the task is
  `failed` with `commit failed: <exit status>` and the run stops. Hooks always
  run.
- A failed task is never committed. Its changes stay in the work tree for you
  to look at.
- Gralph never pushes.

**The work tree has to be clean when the run starts.** Otherwise gralph exits 1
with `error: --commit needs a clean work tree; commit, stash, or remove:` and
the files in the way. The task file itself doesn't count. The simplest setup is
to ignore the run's files. In the project's `.gitignore`:

    tasks.yaml
    prompt.md

or ignore the directory you keep them in. A sandbox settings file kept in the
project needs the same treatment.

After a failed task, the tree is dirty with that task's leftovers, so the next
`--commit` run is refused too. Either throw the leftovers away (`git restore`,
`git clean`, or `git stash`) and set the task back to `pending`, or finish the
work yourself, commit it, and set the task to `completed`.

Outside a git repository `--commit` does nothing: the run goes ahead, and plain
mode prints `commit: not a git repository, nothing will be committed`.

In plain mode gralph prints `commit: <name>` and git's output passes through.
The full-screen view shows `→ commit <name>` in the Claude activity pane.
`--dry-run --commit` applies the same clean-tree check and prints
`commit: <work tree root>`.

Tell Claude not to commit in your shared prompt when you use `--commit`. The
`gralph-docs-writer` skill does that for you when you say the run will use it.
```

In "Checking a task file first", add after the sandbox sentence: "With `--commit`, it also checks the work tree the way a real run would and prints `commit: <work tree root>`."

In "Key Considerations", extend the "Only run task files you trust" bullet's last sentence to: "Gate commands and, with `--commit`, git and the repository's hooks are run by gralph itself, with no sandbox either way."

In "What the sandbox doesn't cover", add a bullet: "**Commits.** With `--commit`, gralph runs git, and so the repository's hooks, itself, with no sandbox."

- [ ] **Step 2: CLAUDE.md**

In "What this is", nothing changes. In the Architecture section:

- `cmd/main` bullet: add "`--commit` is passed to `looper.Start`/`looper.DryRun`; `runTUI` calls `looper.OpenRepo` itself, after the setup screen, and hands the `*looper.Repo` to `tui.Run`."
- `Start` bullet: the call becomes `Run(ctx, prompt, tl, tasksFile, gateTimeout, sessionArgs, repo, nil)` and add "`repo` comes from `repoFor`: nil without `--commit` or outside a git work tree (plain mode then prints `commit: not a git repository, nothing will be committed`); a dirty work tree returns the `--commit needs a clean work tree` error before any session."
- Add a bullet after the Gates bullet:

```markdown
  - Commit (ADR-015): `commit.go`. `OpenRepo(tasksFile)` returns nil outside a git work tree (or when git cannot run) and an error unless the tree is clean apart from the task file. `runTask` calls `repo.commit` only after the session and every gate returned `completed`: `git add -A`, then `git commit -m <task name>` when anything is staged, both over the whole work tree minus the task file and run from its root. Nothing staged means no commit and the task stays `completed`; a non-zero exit makes it `failed` with `commit failed: <exit error>`. Git runs in its own process group with no stdin, like a gate; plain mode prints `commit: <name>`, the stream path reports `→ commit <name>`. Git commands are built by `(*Repo).git`, which appends arguments to a constant `git` command; keep it that way so gosec stays quiet without a suppression. Gralph never pushes, resets, stashes, or passes `--no-verify`.
```

- `DryRun` bullet: add "With `--commit` it applies the same work tree check and prints `commit: <root>`."
- Cancellation sentence "whether a session or a gate was running" becomes "whether a session, a gate, or the commit was running".
- Conventions: add "**No git without `--commit`.** Do not make committing a default, add a task-file key for it, or have gralph clean, reset, or stash a work tree (ADR-015)."

- [ ] **Step 3: Architecture docs**

- `00_overview.md`: after step 4 of the numbered flow (gates), add a step: "With `--commit`, committing the task's changes with git once its gates pass, under the task's name". Add a "Committer" row to the component table ("With `--commit`, commits a completed task's changes after its gates; requires a clean work tree at startup") and a key-principle item ("**Gralph commits, not the session** — with `--commit`, a commit is made only after the gates pass; a failed task is never committed"). Bump to v06 with a note naming ADR-015.
- `01_requirements.md`: add a primary goal ("Commit a completed task's work itself, on request (`--commit`), only after its gates pass, one commit per task"), non-goals ("Pushing, writing commit bodies, or cleaning up a failed task's work tree", "Requiring a git repository"), and a success-criteria row ("Commit on request | One commit per completed task, named after it; none for a failed task; a dirty tree refuses to start | Unit and e2e tests run real git in temporary repositories"). Bump to v07.
- `03_system_architecture.md`: add a "Committer (internal/looper/commit.go)" component section after the Gate Runner, with responsibilities matching the CLAUDE.md bullet above; update the Looper's Core Function row to the new `Run`/`Start` signatures; add to the happy-path sequence diagram, after the gates `opt` block, `opt --commit and inside a git work tree` / `Looper->>Git: add -A, commit -m <task name>` / `Git-->>Looper: exit code (non-zero fails the task)`, with a `participant Git`; add git to the trust-boundary section as something gralph runs unsandboxed. Bump to v09.

- [ ] **Step 4: Verify**

```bash
grep -n "commit" README.md | head -40
grep -rn "ADR-015" docs/architecture CLAUDE.md
```

Expected: the README section is present; ADR-015 is referenced from CLAUDE.md and each bumped doc.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md docs/architecture
git commit -m "docs: --commit"
```

---

### Task 7: Final verification

- [ ] **Step 1: Full build**

Run: `make build`
Expected: PASS (lint, govulncheck, gosec, unit tests, cross-compile, e2e in the container).

- [ ] **Step 2: Manual run in a scratch repository**

```bash
make local && .bin/local/gralph --install-skill
tmp=$(mktemp -d) && cd "$tmp" && git init -q && printf 'run/\n' > .gitignore && git add .gitignore && git commit -qm init
mkdir run && printf 'Reply with the JSON line.\n' > run/prompt.md
printf 'tasks:\n  - id: 1\n    name: Add a note\n    prompt: p\n    gates:\n      - cmd: echo note > note.txt\n' > run/tasks.yaml
/home/jeremy/dev/gralph/.bin/local/gralph -t run/tasks.yaml --dry-run --commit
```

Expected: the task table, a gate line, `commit: <tmp path>`, and `run/tasks.yaml is valid`.

- [ ] **Step 3: Report**

Summarize what was built, the test results from Step 1, and anything left open. Do not push; the PR targets `develop`.
