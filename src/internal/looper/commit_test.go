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

// A git-ignored task file can never be staged, and git add refuses a
// pathspec that names an ignored path, so it must get no exclude.
func TestRun_CommitWithIgnoredTaskFile(t *testing.T) {
	tests := []struct {
		name     string
		ignore   string
		taskFile string
	}{
		{name: "directory pattern", ignore: "run/\n", taskFile: "run/tasks.yaml"},
		{name: "pattern names the file", ignore: "tasks.yaml\n", taskFile: "tasks.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeClaude(t)
			dir := initRepo(t)
			writeFile(t, filepath.Join(dir, ".gitignore"), tt.ignore)
			gitRun(t, dir, "add", ".gitignore")
			gitRun(t, dir, "commit", "-q", "-m", "ignore task file")
			tasksPath := filepath.Join(dir, filepath.FromSlash(tt.taskFile))
			writeFile(t, tasksPath, "tasks: []\n")

			require.NoError(t, Run(context.Background(), "prompt", gatedTask(workGate("one.txt")), tasksPath, "", bypass, openRepo(t, tasksPath), nil))

			assert.Equal(t, []string{"First", "ignore task file", "init"}, subjects(t, dir))
			assert.Equal(t, []string{"one.txt"}, headFiles(t, dir))
			assert.Empty(t, gitRun(t, dir, "status", "--porcelain"))
		})
	}
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
