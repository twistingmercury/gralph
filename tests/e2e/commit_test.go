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
// the prompt and task file live, as the HOWTO recommends.
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

func TestCommit_TaskFileGitCanSeeIsRefused(t *testing.T) {
	t.Parallel()
	repo := newCommitRepo(t, twoWorkTasks)
	visible := filepath.Join(repo.dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(visible, []byte(twoWorkTasks), 0o600))
	repo.tasksPath = visible

	res := repo.run(t, "--commit")

	require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stderr, "error: --commit needs the task file ignored by git or outside the repository: tasks.yaml\n")
	assert.Equal(t, 0, countAttempts(t, repo.attemptLog), "claude must not run")
	assert.Equal(t, []string{"init"}, repo.subjects(t))
}

func TestCommit_BrokenRepositoryIsAnError(t *testing.T) {
	t.Parallel()
	const wantError = "error: --commit: git rev-parse:"

	t.Run("run", func(t *testing.T) {
		t.Parallel()
		repo := newCommitRepo(t, twoWorkTasks)
		breakRepo(t, repo.dir)

		res := repo.run(t, "--commit")

		require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
		assert.Contains(t, res.stderr, wantError)
		assert.Equal(t, 0, countAttempts(t, repo.attemptLog), "claude must not run")
	})

	t.Run("dry run", func(t *testing.T) {
		t.Parallel()
		repo := newCommitRepo(t, twoWorkTasks)
		breakRepo(t, repo.dir)

		res := runGralphIn(t, repo.dir, 15*time.Second, []string{"-t", repo.tasksPath, "--dry-run", "--commit"}, repo.env)

		require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
		assert.Contains(t, res.stderr, wantError)
		assert.NotContains(t, res.stdout, "is valid")
	})
}

// breakRepo makes git refuse to read the repository without making it look
// like "not a repository".
func breakRepo(t *testing.T, dir string) {
	t.Helper()
	config, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = config.WriteString("[broken\n")
	require.NoError(t, err)
	require.NoError(t, config.Close())
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
