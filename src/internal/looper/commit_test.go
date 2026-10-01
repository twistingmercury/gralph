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
