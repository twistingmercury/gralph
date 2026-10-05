package looper

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/gralph/internal/tasks"
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
// it. The commit ignores run/, where tests keep the task file as a --commit
// run requires, and the directory exists so Run can save the file. It uses
// t.Setenv and t.Chdir, so the calling test must not be parallel.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := tempDir(t)
	isolateGit(t, dir)
	t.Chdir(dir)
	gitRun(t, dir, "init", "-q")
	writeFile(t, filepath.Join(dir, "seed.txt"), "seed\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), "run/\n")
	gitRun(t, dir, "add", "seed.txt", ".gitignore")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "run"), 0o750))
	return dir
}

// ignoreAndCommit replaces .gitignore with patterns and commits it, so the
// tree stays clean.
func ignoreAndCommit(t *testing.T, dir string, patterns ...string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, ".gitignore"), strings.Join(patterns, "\n")+"\n")
	gitRun(t, dir, "add", ".gitignore")
	gitRun(t, dir, "commit", "-q", "-m", "ignore")
}

// taskFile is where tests keep the task file: inside the ignored run/.
func taskFile(dir string) string {
	return filepath.Join(dir, "run", "tasks.yaml")
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

	repo, err := OpenRepo(taskFile(dir))
	require.NoError(t, err)
	assert.Nil(t, repo)
}

func TestOpenRepo_GitNotInstalledIsNil(t *testing.T) {
	dir := initRepo(t)
	t.Setenv("PATH", t.TempDir())

	repo, err := OpenRepo(taskFile(dir))
	require.NoError(t, err)
	assert.Nil(t, repo)
}

func TestOpenRepo_CleanTree(t *testing.T) {
	dir := initRepo(t)

	repo, err := OpenRepo(taskFile(dir))
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
		{name: "untracked file in a new directory is named", wantLine: "?? docs/notes.md", dirty: func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "docs", "notes.md"), "notes\n")
		}},
		{name: "non-ASCII path is readable", wantLine: "?? café.txt", dirty: func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "café.txt"), "latte\n")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			tasksPath := taskFile(dir)
			writeFile(t, tasksPath, "tasks: []\n")
			tt.dirty(t, dir)

			repo, err := OpenRepo(tasksPath)
			assert.Nil(t, repo)
			require.EqualError(t, err, "--commit needs a clean work tree; commit, stash, or remove:\n"+tt.wantLine)
		})
	}
}

func TestOpenRepo_IgnoredFilesAreClean(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "run", "notes.md"), "notes\n")
	tasksPath := taskFile(dir)
	writeFile(t, tasksPath, "tasks: []\n")

	repo, err := OpenRepo(tasksPath)
	require.NoError(t, err)
	assert.NotNil(t, repo)
}

func TestOpenRepo_TaskFileMustBeIgnoredOrOutside(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string) string
		wantRel string
	}{
		{name: "untracked and not ignored", wantRel: "tasks.yaml", setup: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "tasks.yaml")
			writeFile(t, path, "tasks: []\n")
			return path
		}},
		{name: "tracked", wantRel: "tasks.yaml", setup: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "tasks.yaml")
			writeFile(t, path, "tasks: []\n")
			gitRun(t, dir, "add", "tasks.yaml")
			gitRun(t, dir, "commit", "-q", "-m", "track tasks")
			return path
		}},
		{name: "staged", wantRel: "tasks.yaml", setup: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "tasks.yaml")
			writeFile(t, path, "tasks: []\n")
			gitRun(t, dir, "add", "tasks.yaml")
			return path
		}},
		{name: "tracked but matching an ignore pattern", wantRel: "tasks.yaml", setup: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "tasks.yaml")
			writeFile(t, path, "tasks: []\n")
			gitRun(t, dir, "add", "tasks.yaml")
			gitRun(t, dir, "commit", "-q", "-m", "track tasks")
			ignoreAndCommit(t, dir, "run/", "tasks.yaml")
			return path
		}},
		{name: "relative path", wantRel: "docs/tasks.yaml", setup: func(t *testing.T, dir string) string {
			writeFile(t, filepath.Join(dir, "docs", "tasks.yaml"), "tasks: []\n")
			return filepath.Join("docs", "tasks.yaml")
		}},
		{name: "untracked symlink to a file outside the tree", wantRel: "link.yaml", setup: func(t *testing.T, dir string) string {
			target := filepath.Join(tempDir(t), "tasks.yaml")
			writeFile(t, target, "tasks: []\n")
			link := filepath.Join(dir, "link.yaml")
			require.NoError(t, os.Symlink(target, link))
			return link
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			path := tt.setup(t, dir)

			repo, err := OpenRepo(path)
			assert.Nil(t, repo)
			require.EqualError(t, err, "--commit needs the task file ignored by git or outside the repository: "+tt.wantRel)
		})
	}
}

func TestOpenRepo_TaskFileIgnoredOrOutsideIsAccepted(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) string
	}{
		{name: "ignored by a directory pattern", setup: func(t *testing.T, dir string) string {
			path := taskFile(dir)
			writeFile(t, path, "tasks: []\n")
			return path
		}},
		{name: "ignored by a pattern naming the file", setup: func(t *testing.T, dir string) string {
			ignoreAndCommit(t, dir, "tasks.yaml")
			path := filepath.Join(dir, "tasks.yaml")
			writeFile(t, path, "tasks: []\n")
			return path
		}},
		{name: "outside the tree", setup: func(t *testing.T, _ string) string {
			path := filepath.Join(tempDir(t), "tasks.yaml")
			writeFile(t, path, "tasks: []\n")
			return path
		}},
		{name: "not yet created in an ignored directory", setup: func(_ *testing.T, dir string) string {
			return taskFile(dir)
		}},
		{name: "not yet created in a directory that does not exist", setup: func(_ *testing.T, dir string) string {
			return filepath.Join(dir, "run", "later", "tasks.yaml")
		}},
		{name: "relative and ignored", setup: func(t *testing.T, dir string) string {
			writeFile(t, taskFile(dir), "tasks: []\n")
			return filepath.Join("run", "tasks.yaml")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)

			repo, err := OpenRepo(tt.setup(t, dir))
			require.NoError(t, err)
			require.NotNil(t, repo)
			assert.Equal(t, dir, repo.Root())
		})
	}
}

func TestOpenRepo_TaskFileReachedThroughASymlinkedDirectory(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, taskFile(dir), "tasks: []\n")
	alias := filepath.Join(tempDir(t), "alias")
	require.NoError(t, os.Symlink(filepath.Join(dir, "run"), alias))

	repo, err := OpenRepo(filepath.Join(alias, "tasks.yaml"))
	require.NoError(t, err)
	assert.NotNil(t, repo, "the parent directory resolves into the tree, where run/ is ignored")
}

func TestOpenRepo_FromSubdirectoryCoversWholeTree(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o750))
	writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
	t.Chdir(sub)

	repo, err := OpenRepo(taskFile(dir))
	assert.Nil(t, repo)
	require.ErrorContains(t, err, "?? stray.txt")
}

func TestOpenRepo_FromSubdirectoryChecksTheTaskFileFromTheRoot(t *testing.T) {
	dir := initRepo(t)
	// Anchored to the root: initRepo's bare "run/" also matches sub/run/, which
	// is where a git started in sub would look for the root-relative path.
	ignoreAndCommit(t, dir, "/run/")
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o750))
	t.Chdir(sub)

	repo, err := OpenRepo(taskFile(dir))
	require.NoError(t, err)
	require.NotNil(t, repo)
	assert.Equal(t, dir, repo.Root())
}

func TestOpenRepo_BrokenRepositoryIsAnError(t *testing.T) {
	dir := initRepo(t)
	config, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = config.WriteString("[broken\n")
	require.NoError(t, err)
	require.NoError(t, config.Close())

	repo, err := OpenRepo(taskFile(dir))
	assert.Nil(t, repo)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "--commit: git rev-parse: "), err.Error())
	assert.NotContains(t, err.Error(), "fatal:", "git's own prefix is dropped")
	assert.NotContains(t, err.Error(), "\n", "only git's first line")
}

func TestOpenRepo_StatusFailureCarriesGitsMessage(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("garbage"), 0o600))

	// Outside the tree, so the task file check does not touch the index first.
	repo, err := OpenRepo(filepath.Join(tempDir(t), "tasks.yaml"))
	assert.Nil(t, repo)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "--commit: git status: "), err.Error())
	assert.NotContains(t, err.Error(), "exit status", "git's message replaces the bare exit status")
}

// workGate stands in for a session's work: the fake claude changes no files,
// so a gate writes the file the commit should pick up.
func workGate(name string) tasks.Gate {
	return tasks.Gate{Cmd: "echo work > " + name}
}

// numberedWorkGate is workGate for a file whose gates run after every task:
// each run writes the next free work-<n>.txt, so each task's commit holds a
// file of its own.
func numberedWorkGate() tasks.Gate {
	return tasks.Gate{Cmd: "n=1; while [ -e work-$n.txt ]; do n=$((n+1)); done; echo work > work-$n.txt"}
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
			tasksPath := taskFile(dir)
			tl := &tasks.TaskList{
				Shared: tasks.Shared{Prompt: "shared prompt", Gates: &[]tasks.Gate{numberedWorkGate()}},
				Tasks: []tasks.Task{
					{ID: 1, Name: "First", Prompt: "p1"},
					{ID: 2, Name: "Second", Prompt: "p2"},
				},
			}

			var rec recorder
			var report func(Event)
			if stream {
				report = rec.report
			}

			require.NoError(t, Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), report))

			assert.Equal(t, []string{"Second", "First", "init"}, subjects(t, dir))
			assert.Equal(t, []string{"work-2.txt"}, headFiles(t, dir))
			assert.Empty(t, gitRun(t, dir, "status", "--porcelain"), "nothing may be left uncommitted")

			saved := readSavedTasks(t, tasksPath)
			assert.Equal(t, tasks.CompletedState, saved.Tasks[0].State)
			assert.Equal(t, tasks.CompletedState, saved.Tasks[1].State)
		})
	}
}

func TestRun_NilRepoNeverCommits(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tl := gatedTask(workGate("one.txt"))

	require.NoError(t, Run(context.Background(), tl, tasksPath, "", bypass, nil, nil))

	assert.Equal(t, []string{"init"}, subjects(t, dir))
	assert.FileExists(t, filepath.Join(dir, "one.txt"))
}

func TestRun_NothingToCommitStillCompletes(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tl := gatedTask()
	repo := openRepo(t, tasksPath)

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := Run(context.Background(), tl, tasksPath, "", bypass, repo, nil)

	require.NoError(t, w.Close())
	os.Stdout = origStdout
	require.NoError(t, runErr)

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)

	// A commit line with no commit behind it would tell the user about a
	// commit git never made.
	assert.NotContains(t, buf.String(), "commit:", "nothing staged must announce nothing")
	assert.Equal(t, []string{"init"}, subjects(t, dir), "no empty commit")
	assert.Equal(t, tasks.CompletedState, readSavedTasks(t, tasksPath).Tasks[0].State)
}

func TestRun_FailedGateLeavesChangesUncommitted(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tl := gatedTask(tasks.Gate{Cmd: "echo work > one.txt; exit 1"})

	err := Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil)
	require.ErrorContains(t, err, `gate "echo work > one.txt; exit 1" failed`)

	assert.Equal(t, []string{"init"}, subjects(t, dir))
	assert.FileExists(t, filepath.Join(dir, "one.txt"), "a failed task's changes stay in the work tree")
}

func TestRun_SessionFailureSkipsCommit(t *testing.T) {
	useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_OUTPUT", `{"state":"failed","error":"nope"}`)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	repo := openRepo(t, tasksPath)
	writeFile(t, filepath.Join(dir, "left.txt"), "left by the session\n")

	err := Run(context.Background(), gatedTask(), tasksPath, "", bypass, repo, nil)
	require.ErrorContains(t, err, "nope")
	assert.Equal(t, []string{"init"}, subjects(t, dir))
}

func TestRun_CommitFailureFailsTaskAndStopsRun(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	installHook(t, dir, "echo hook-says-no >&2\nexit 1")
	recordPath := filepath.Join(tempDir(t), "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	tasksPath := taskFile(dir)
	tl := &tasks.TaskList{
		Shared: tasks.Shared{Prompt: "shared prompt", Gates: &[]tasks.Gate{workGate("one.txt")}},
		Tasks: []tasks.Task{
			{ID: 1, Name: "First", Prompt: "p1"},
			{ID: 2, Name: "Second", Prompt: "p2"},
		},
	}

	const wantErr = "commit failed: exit status 1"
	err := Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil)
	require.EqualError(t, err, "task 1: First failed: "+wantErr)

	assert.Equal(t, []string{"init"}, subjects(t, dir))
	assert.Len(t, readFakeClaudeRecords(t, recordPath), 1, "the second task must never run")

	saved := readSavedTasks(t, tasksPath)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, wantErr, saved.Tasks[0].Error)
	assert.Equal(t, tasks.PendingState, saved.Tasks[1].State)
}

// The task file is rewritten after every task and git must never see it, so
// a gate that appends its own ignore pattern or edits .gitignore must not
// disturb the commit: the edit is simply part of the task's work.
func TestRun_CommitSurvivesAGateEditingGitignore(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tl := gatedTask(
		tasks.Gate{Cmd: "echo 'run/' >> .gitignore"},
		tasks.Gate{Cmd: "echo '*.log' >> .gitignore"},
		workGate("one.txt"),
	)

	require.NoError(t, Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.Equal(t, []string{"First", "init"}, subjects(t, dir))
	assert.ElementsMatch(t, []string{".gitignore", "one.txt"}, headFiles(t, dir))
	assert.Empty(t, gitRun(t, dir, "status", "--porcelain"))
	assert.Equal(t, tasks.CompletedState, readSavedTasks(t, tasksPath).Tasks[0].State)
}

func TestRun_CommitAfterTheSessionCommittedItself(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tl := gatedTask(tasks.Gate{Cmd: "echo work > one.txt && git add -A && git commit -q -m own"})

	require.NoError(t, Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.Equal(t, []string{"own", "init"}, subjects(t, dir), "gralph must add no commit of its own")
	assert.Equal(t, tasks.CompletedState, readSavedTasks(t, tasksPath).Tasks[0].State)
}

// An index.lock left in the repository makes git add fail before anything is
// staged, which is the other way the commit step can fail.
func TestRun_FailedAddFailsTheTask(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tl := gatedTask(workGate("one.txt"), tasks.Gate{Cmd: "touch .git/index.lock"})

	const wantErr = "commit failed: exit status 128"
	err := Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil)
	require.EqualError(t, err, "task 1: First failed: "+wantErr)

	saved := readSavedTasks(t, tasksPath)
	assert.Equal(t, tasks.FailedState, saved.Tasks[0].State)
	assert.Equal(t, wantErr, saved.Tasks[0].Error)
	assert.Equal(t, []string{"init"}, subjects(t, dir))
}

func TestRun_CommitWithTaskFileOutsideTheTree(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := filepath.Join(tempDir(t), "tasks.yaml")

	require.NoError(t, Run(context.Background(), gatedTask(workGate("one.txt")), tasksPath, "", bypass, openRepo(t, tasksPath), nil))

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
	tasksPath := taskFile(dir)

	// The gate runs in sub and writes one file there and one in the root.
	tl := gatedTask(tasks.Gate{Cmd: "echo a > here.txt; echo b > ../top.txt"})
	require.NoError(t, Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), nil))

	assert.ElementsMatch(t, []string{"sub/here.txt", "top.txt"}, headFiles(t, dir))
}

func TestRun_CommitOddTaskNames(t *testing.T) {
	tests := []struct {
		name        string
		taskName    string
		wantSubject string
		wantMessage string
		wantLine    string
	}{
		{name: "multi-line", taskName: "Subject line\n\nBody text", wantSubject: "Subject line", wantMessage: "Subject line\n\nBody text", wantLine: "→ commit Subject line"},
		{name: "leading dash", taskName: "--amend the widget", wantSubject: "--amend the widget", wantMessage: "--amend the widget", wantLine: "→ commit --amend the widget"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			dir := initRepo(t)
			tasksPath := taskFile(dir)
			tl := &tasks.TaskList{Shared: tasks.Shared{Prompt: "shared prompt", Gates: &[]tasks.Gate{workGate("one.txt")}}, Tasks: []tasks.Task{{ID: 1, Name: tt.taskName, Prompt: "p"}}}

			var rec recorder
			require.NoError(t, Run(context.Background(), tl, tasksPath, "", bypass, openRepo(t, tasksPath), rec.report))

			assert.Equal(t, []string{tt.wantSubject, "init"}, subjects(t, dir))

			// The subject alone cannot tell a whole name from its first line:
			// only the announced line is cut, the message keeps the body.
			message := gitRun(t, dir, "log", "-1", "--format=%B")
			assert.Equal(t, tt.wantMessage, strings.TrimRight(message, "\n"))
			assert.Contains(t, activityLines(rec.snapshot()), tt.wantLine)
		})
	}
}

func TestRun_PlainPrintsCommitLineAndGitOutput(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	repo := openRepo(t, tasksPath)

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := Run(context.Background(), gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, nil)

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
	tasksPath := taskFile(dir)
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
	runErr := Run(context.Background(), gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, rec.report)
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

func TestRun_ReportsCommittedWithTheNewHash(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	repo := openRepo(t, tasksPath)

	var rec recorder
	require.NoError(t, Run(context.Background(), gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, rec.report))

	events := rec.snapshot()
	commits := eventsOfKind(events, Committed)
	require.Len(t, commits, 1)
	head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	assert.Equal(t, head, commits[0].Hash)
	assert.Equal(t, int16(1), commits[0].Task.ID)

	kinds := eventKinds(events)
	assert.Equal(t, []EventKind{Committed, TaskFinished, RunDone}, kinds[len(kinds)-3:], "Committed comes after git's output and before TaskFinished")
}

func TestRun_NothingStagedReportsNoCommitted(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	repo := openRepo(t, tasksPath)

	var rec recorder
	require.NoError(t, Run(context.Background(), gatedTask(), tasksPath, "", bypass, repo, rec.report))

	assert.Empty(t, eventsOfKind(rec.snapshot(), Committed))
}

// cancelDuringCommit runs one task whose pre-commit hook is script, cancels
// the context once the hook has started, and returns how long Run took to
// return after the cancel and its error.
func cancelDuringCommit(t *testing.T, dir, tasksPath, script string) (time.Duration, error) {
	t.Helper()
	readyPath := filepath.Join(tempDir(t), "ready")
	installHook(t, dir, strings.ReplaceAll(script, "READY", "'"+readyPath+"'"))
	repo := openRepo(t, tasksPath)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, gatedTask(workGate("one.txt")), tasksPath, "", bypass, repo, nil)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the commit hook never started")

	start := time.Now()
	cancel()

	select {
	case err := <-errCh:
		return time.Since(start), err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after a cancel during the commit; git's process group was not stopped")
		return 0, nil
	}
}

func TestRun_CancelDuringCommitLeavesFileUntouched(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	tasksYAML := "shared:\n  prompt: shared prompt\ntasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n"
	writeFile(t, tasksPath, tasksYAML)

	_, err := cancelDuringCommit(t, dir, tasksPath, "touch READY\nsleep 60")
	require.ErrorContains(t, err, "task 1: First failed: commit")

	got, readErr := os.ReadFile(tasksPath)
	require.NoError(t, readErr)
	assert.Equal(t, tasksYAML, string(got), "the tasks file must be byte-for-byte unchanged after a cancel during the commit")
	assert.Equal(t, []string{"init"}, subjects(t, dir))

	locks, globErr := filepath.Glob(filepath.Join(dir, ".git", "*.lock"))
	require.NoError(t, globErr)
	assert.Empty(t, locks, "git must be stopped politely so it removes its own lock")
}

// A hook that ignores SIGTERM must not hang gralph: after the grace period the
// whole group is killed.
func TestRun_CancelDuringCommitKillsAHookThatIgnoresSIGTERM(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	tasksPath := taskFile(dir)
	writeFile(t, tasksPath, "shared:\n  prompt: shared prompt\ntasks:\n  - {id: 1, name: First, prompt: p1, state: pending}\n")

	took, err := cancelDuringCommit(t, dir, tasksPath, "trap '' TERM\ntouch READY\nsleep 60")
	require.ErrorContains(t, err, "task 1: First failed: commit")

	assert.Less(t, took, 6*time.Second, "SIGKILL must follow the grace period")
	assert.GreaterOrEqual(t, took, gitStopGrace/2, "SIGTERM must come first and be given time")
	assert.Equal(t, []string{"init"}, subjects(t, dir))
}

const validTasks = "shared:\n  prompt: shared prompt\ntasks:\n  - {id: 1, name: First, prompt: p1}\n"

func TestStart_CommitRefusesDirtyTreeBeforeAnySession(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	recordPath := filepath.Join(tempDir(t), "record.log")
	t.Setenv("FAKE_CLAUDE_RECORD", recordPath)
	writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
	run := tempDir(t)
	tasksPath := filepath.Join(run, "tasks.yaml")
	writeFile(t, tasksPath, validTasks)

	err := Start(context.Background(), tasksPath, "", bypass, true)
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
	tasksPath := filepath.Join(run, "tasks.yaml")
	writeFile(t, tasksPath, "shared:\n  prompt: shared prompt\n  gates:\n    - cmd: echo work > one.txt\ntasks:\n  - id: 1\n    name: First\n    prompt: p1\n")

	require.NoError(t, Start(context.Background(), tasksPath, "", bypass, true))
	assert.Equal(t, []string{"First", "init"}, subjects(t, dir))
}

func TestStart_WithoutCommitFlagIgnoresADirtyTree(t *testing.T) {
	useFakeClaude(t)
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "stray.txt"), "stray\n")
	run := tempDir(t)
	tasksPath := filepath.Join(run, "tasks.yaml")
	writeFile(t, tasksPath, validTasks)

	require.NoError(t, Start(context.Background(), tasksPath, "", bypass, false))
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

	t.Run("broken repository is an error", func(t *testing.T) {
		dir := initRepo(t)
		config, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		_, err = config.WriteString("[broken\n")
		require.NoError(t, err)
		require.NoError(t, config.Close())
		tasksPath := filepath.Join(tempDir(t), "tasks.yaml")
		writeFile(t, tasksPath, validTasks)

		var out bytes.Buffer
		err = DryRun(&out, tasksPath, "", "", true)
		require.ErrorContains(t, err, "--commit: git rev-parse: ")
		assert.NotContains(t, out.String(), "is valid")
	})

	t.Run("outside a repository says nothing will be committed", func(t *testing.T) {
		dir := tempDir(t)
		isolateGit(t, dir)
		t.Chdir(dir)
		tasksPath := taskFile(dir)
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

func TestCheckLogDir_NilRepoChecksNothing(t *testing.T) {
	var repo *Repo

	assert.NoError(t, repo.CheckLogDir(filepath.Join("logs", "20261001T140211")))
}

func TestCheckLogDir_InsideTheTreeMustBeIgnored(t *testing.T) {
	dir := initRepo(t)
	repo := openRepo(t, taskFile(dir))

	err := repo.CheckLogDir(filepath.Join(dir, "logs", "20261001T140211"))

	require.EqualError(t, err, "--commit needs the log directory ignored by git or outside the repository: logs")
}

// The log directory does not exist yet and the rule is a directory pattern,
// which git only matches for a path it can tell is inside that directory.
func TestCheckLogDir_IgnoredByADirectoryPatternBeforeItExists(t *testing.T) {
	dir := initRepo(t)
	ignoreAndCommit(t, dir, "run/", "logs/")
	repo := openRepo(t, taskFile(dir))

	assert.NoError(t, repo.CheckLogDir(filepath.Join(dir, "logs", "20261001T140211")))
	assert.NoDirExists(t, filepath.Join(dir, "logs"), "the check creates nothing")
}

func TestCheckLogDir_RelativePathFromASubdirectory(t *testing.T) {
	dir := initRepo(t)
	ignoreAndCommit(t, dir, "run/", "logs/")
	repo := openRepo(t, taskFile(dir))
	sub := filepath.Join(dir, "run", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	t.Chdir(sub)

	assert.NoError(t, repo.CheckLogDir(filepath.Join("..", "..", "logs", "20261001T140211")))

	err := repo.CheckLogDir(filepath.Join("..", "..", "elsewhere", "20261001T140211"))
	require.EqualError(t, err, "--commit needs the log directory ignored by git or outside the repository: elsewhere")
}

func TestCheckLogDir_OutsideTheTreeNeedsNoRule(t *testing.T) {
	dir := initRepo(t)
	repo := openRepo(t, taskFile(dir))
	outside := tempDir(t)

	assert.NoError(t, repo.CheckLogDir(filepath.Join(outside, "logs", "20261001T140211")))
}
