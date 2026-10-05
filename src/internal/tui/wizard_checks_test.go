package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFileIn writes one file into dir, for building run folders.
func writeFileIn(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// ignoreAndCommit replaces .gitignore with patterns and commits it, so the
// tree stays clean.
func ignoreAndCommit(t *testing.T, dir string, patterns ...string) {
	t.Helper()
	content := strings.Join(patterns, "\n") + "\n"
	writeFileIn(t, dir, ".gitignore", content)
	for _, args := range [][]string{{"add", ".gitignore"}, {"commit", "-q", "-m", "ignore"}} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
}

func TestCheckFolder(t *testing.T) {
	good := t.TempDir()
	writeFileIn(t, good, "tasks.yaml", validTasks)
	writeFileIn(t, good, "prompt.md", "do the work")
	noTasks := t.TempDir()
	writeFileIn(t, noTasks, "prompt.md", "do the work")
	noPrompt := t.TempDir()
	writeFileIn(t, noPrompt, "tasks.yaml", validTasks)
	badYAML := t.TempDir()
	writeFileIn(t, badYAML, "tasks.yaml", "tasks: [\n")
	writeFileIn(t, badYAML, "prompt.md", "do the work")
	failed := t.TempDir()
	writeFileIn(t, failed, "tasks.yaml", "shared:\n  prompt: shared prompt\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n    state: failed\n")
	writeFileIn(t, failed, "prompt.md", "do the work")
	blankPrompt := t.TempDir()
	writeFileIn(t, blankPrompt, "tasks.yaml", validTasks)
	writeFileIn(t, blankPrompt, "prompt.md", " \n")

	cases := map[string]struct{ dir, wantErr string }{
		"good folder":   {good, ""},
		"no tasks.yaml": {noTasks, "no tasks.yaml in " + noTasks},
		"no prompt.md":  {noPrompt, "no prompt.md in " + noPrompt},
		"bad yaml":      {badYAML, "failed to parse tasks yaml"},
		"failed task":   {failed, "gralph -d " + failed + " --dry-run"},
		"blank prompt":  {blankPrompt, "just whitespace"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWizard(Settings{}, Given{})
			err := w.checkFolder(tc.dir)
			if tc.wantErr == "" {
				require.NoError(t, err)
				wantTasks := filepath.Join(tc.dir, "tasks.yaml")
				wantPrompt := filepath.Join(tc.dir, "prompt.md")
				assert.Equal(t, wantTasks, w.s.TasksPath)
				assert.Equal(t, wantPrompt, w.s.PromptPath)
				assert.Equal(t, tc.dir, w.s.Dir)
				assert.Equal(t, "do the work", w.s.Prompt)
				assert.NotNil(t, w.s.Tasks)
				return
			}

			assert.ErrorContains(t, err, tc.wantErr)
			assert.Empty(t, w.s.Dir, "a rejected folder must leave the settings as they were")
			assert.Empty(t, w.s.TasksPath)
			assert.Empty(t, w.s.PromptPath)
			assert.Nil(t, w.s.Tasks)
		})
	}
}

func TestCheckFolder_FlagFileWins(t *testing.T) {
	flagTasks := filepath.Join(t.TempDir(), "v2.yaml")
	require.NoError(t, os.WriteFile(flagTasks, []byte(validTasks), 0o600))
	dir := t.TempDir()
	// The folder's own tasks.yaml would fail to parse, so a pass proves it was
	// never loaded.
	writeFileIn(t, dir, "tasks.yaml", "tasks: [\n")
	writeFileIn(t, dir, "prompt.md", "do the work")

	w := newWizard(Settings{TasksPath: flagTasks}, Given{})
	err := w.checkFolder(dir)
	require.NoError(t, err)
	wantPrompt := filepath.Join(dir, "prompt.md")
	assert.Equal(t, flagTasks, w.s.TasksPath)
	assert.Equal(t, wantPrompt, w.s.PromptPath)
	assert.Equal(t, dir, w.s.Dir)
	require.NotNil(t, w.s.Tasks)
	assert.Equal(t, "First", w.s.Tasks.Tasks[0].Name)
}

func TestCheckFolder_FlagFileNeedNotBeInTheFolder(t *testing.T) {
	flagPrompt := filepath.Join(t.TempDir(), "p.md")
	require.NoError(t, os.WriteFile(flagPrompt, []byte("flag prompt"), 0o600))
	dir := t.TempDir()
	writeFileIn(t, dir, "tasks.yaml", validTasks)

	w := newWizard(Settings{PromptPath: flagPrompt}, Given{})
	err := w.checkFolder(dir)
	require.NoError(t, err)
	assert.Equal(t, flagPrompt, w.s.PromptPath)
	assert.Equal(t, "flag prompt", w.s.Prompt)
}

func TestCheckPermission(t *testing.T) {
	assert.EqualError(t, checkPermission(""), "choose how sessions run")
	assert.NoError(t, checkPermission(permSandbox))
	assert.NoError(t, checkPermission(permSkip))
}

func TestCheckSandboxFile(t *testing.T) {
	dir := t.TempDir()
	writeFileIn(t, dir, "good.json", `{"permissions": {}}`)
	writeFileIn(t, dir, "array.json", "[1]")
	missing := filepath.Join(dir, "missing.json")

	good := filepath.Join(dir, "good.json")
	assert.NoError(t, checkSandboxFile(good))
	assert.ErrorContains(t, checkSandboxFile(missing), "is not accessible")
	array := filepath.Join(dir, "array.json")
	assert.ErrorContains(t, checkSandboxFile(array), "must be a JSON object")
}

func TestCheckGateTimeout(t *testing.T) {
	assert.EqualError(t, checkCustomTimeout(""), "enter a timeout such as 90s or 10m")
	assert.NoError(t, checkCustomTimeout("90s"))
	assert.EqualError(t, checkCustomTimeout("30"), "must be a duration string such as 90s or 10m")
	assert.EqualError(t, checkCustomTimeout("0s"), "must be greater than zero")
}

// commitWizard is a wizard for a task file at tasksPath, as the folder step
// leaves it.
func commitWizard(tasksPath string) *wizard {
	dir := filepath.Dir(tasksPath)
	return newWizard(Settings{Dir: dir, TasksPath: tasksPath}, Given{})
}

func TestCheckCommit(t *testing.T) {
	t.Run("clean tree and an ignored task file", func(t *testing.T) {
		dir := initGitRepo(t)
		tasksPath := filepath.Join(dir, "run", "tasks.yaml")
		w := commitWizard(tasksPath)
		assert.NoError(t, w.checkCommit(true))
	})

	t.Run("untracked file", func(t *testing.T) {
		dir := initGitRepo(t)
		writeFileIn(t, dir, "stray.txt", "stray\n")
		tasksPath := filepath.Join(dir, "run", "tasks.yaml")
		w := commitWizard(tasksPath)
		err := w.checkCommit(true)
		assert.ErrorContains(t, err, "--commit needs a clean work tree")
		assert.ErrorContains(t, err, "stray.txt")
	})

	t.Run("task file git can see", func(t *testing.T) {
		dir := initGitRepo(t)
		tasksPath := filepath.Join(dir, "f", "tasks.yaml")
		w := commitWizard(tasksPath)
		assert.ErrorContains(t, w.checkCommit(true), "--commit needs the task file ignored by git")
	})

	t.Run("no runs no git", func(t *testing.T) {
		dir := initGitRepo(t)
		writeFileIn(t, dir, "stray.txt", "stray\n")
		tasksPath := filepath.Join(dir, "f", "tasks.yaml")
		w := commitWizard(tasksPath)
		// A dirty tree and a visible task file would fail any git check.
		require.Error(t, w.checkCommit(true))
		assert.NoError(t, w.checkCommit(false))
	})
}

// loggingWizard is a wizard for folder f in a fresh repository ignoring
// patterns. The folder is not ignored as a whole, so whether f/logs is ignored
// depends on patterns alone.
func loggingWizard(t *testing.T, patterns ...string) *wizard {
	t.Helper()
	dir := initGitRepo(t)
	ignoreAndCommit(t, dir, patterns...)
	tasksPath := filepath.Join(dir, "f", "tasks.yaml")
	return commitWizard(tasksPath)
}

func TestCheckLogging(t *testing.T) {
	t.Run("commit chosen and logs not ignored", func(t *testing.T) {
		w := loggingWizard(t, "f/tasks.yaml")
		w.commit = true
		err := w.checkLogging(true)
		assert.ErrorContains(t, err, "--commit needs the log directory ignored by git")
		assert.ErrorContains(t, err, "f/logs")
		assert.Empty(t, w.s.LogDir)
	})

	t.Run("commit from the flag and logs not ignored", func(t *testing.T) {
		w := loggingWizard(t, "f/tasks.yaml")
		w.s.Commit, w.g.Commit = true, true
		assert.ErrorContains(t, w.checkLogging(true), "--commit needs the log directory ignored by git")
	})

	t.Run("commit chosen and logs ignored", func(t *testing.T) {
		w := loggingWizard(t, "f/tasks.yaml", "logs/")
		w.commit = true
		require.NoError(t, w.checkLogging(true))
		wantLogDir := filepath.Join(w.s.Dir, "logs")
		assert.Equal(t, wantLogDir, w.s.LogDir)
	})

	t.Run("no commit needs no git check", func(t *testing.T) {
		w := loggingWizard(t, "f/tasks.yaml")
		// f/logs is not ignored, so a git check would have failed.
		require.NoError(t, w.checkLogging(true))
		wantLogDir := filepath.Join(w.s.Dir, "logs")
		assert.Equal(t, wantLogDir, w.s.LogDir)
	})

	t.Run("no leaves the log dir empty", func(t *testing.T) {
		w := loggingWizard(t, "f/tasks.yaml")
		w.commit = true
		w.s.LogDir = "stale"
		require.NoError(t, w.checkLogging(false))
		assert.Empty(t, w.s.LogDir)
	})
}
