package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/runlog"
	"github.com/twistingmercury/gralph/internal/tasks"
	"github.com/twistingmercury/gralph/internal/tui"
)

const validTasksYAML = "tasks:\n  - id: 1\n    name: first\n    prompt: do it\n"

func TestSessionArgs(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0o600))
	sandboxed := []string{"--permission-mode", "acceptEdits", "--settings", `{"sandbox":{"allowUnsandboxedCommands":false,"enabled":true,"failIfUnavailable":true}}`}

	tests := []struct {
		name    string
		file    string
		skip    bool
		dryRun  bool
		want    []string
		wantErr string
	}{
		{name: "sandbox file", file: settings, want: sandboxed},
		{name: "skip", skip: true, want: []string{"--dangerously-skip-permissions"}},
		{name: "both", file: settings, skip: true, wantErr: "--sandbox-settings and --skip-permissions cannot be used together"},
		{name: "neither", wantErr: "pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox"},
		{name: "dry run, neither", dryRun: true},
		{name: "dry run, both", file: settings, skip: true, dryRun: true, wantErr: "--sandbox-settings and --skip-permissions cannot be used together"},
		{name: "dry run, sandbox file", file: settings, dryRun: true, want: sandboxed},
		{name: "bad file", file: filepath.Join(t.TempDir(), "missing.json"), wantErr: "--sandbox-settings: file"},
		{name: "dry run, bad file", file: filepath.Join(t.TempDir(), "missing.json"), dryRun: true, wantErr: "--sandbox-settings: file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sessionArgs(tt.file, tt.skip, tt.dryRun, false)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSessionArgs_WizardLeavesTheChoiceOpen(t *testing.T) {
	args, err := sessionArgs("", false, false, true)
	require.NoError(t, err, "the full-screen view asks instead of failing")
	assert.Nil(t, args)

	_, err = sessionArgs("", false, false, false)
	assert.EqualError(t, err, "pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox")
}

func TestSaveGates_OnlyWhenEdited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yaml")
	original := "# a comment SaveTasks would drop\ntasks:\n  - id: 1\n    name: First\n    prompt: do it\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
	tl, err := looper.LoadTasks(path)
	require.NoError(t, err)

	gates := []tasks.Gate{{Cmd: "make test"}}
	edited := *tl
	edited.Gates = &gates

	require.NoError(t, saveGates(tui.Settings{TasksPath: path, Tasks: &edited, GatesEdited: false}))
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, string(unchanged), "no edit, no write")

	require.NoError(t, saveGates(tui.Settings{TasksPath: path, Tasks: &edited, GatesEdited: true}))
	saved, err := looper.LoadTasks(path)
	require.NoError(t, err)
	require.NotNil(t, saved.Gates)
	assert.Equal(t, gates, *saved.Gates)
}

func TestLoadGiven_BothEmpty(t *testing.T) {
	prompt, tasklist, err := loadGiven("", "")

	require.NoError(t, err)
	assert.Empty(t, prompt)
	assert.Nil(t, tasklist)
}

func TestLoadGiven_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte(validTasksYAML), 0o600))
	promptPath := filepath.Join(dir, "missing.md")

	prompt, tasklist, err := loadGiven(tasksPath, promptPath)

	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "failed to start loop runner: "), err.Error())
	assert.Empty(t, prompt)
	assert.Nil(t, tasklist)
}

func TestLoadGiven_BadTasks(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "missing.yaml")

	_, tasklist, err := loadGiven(tasksPath, "")

	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "failed to start loop runner: "), err.Error())
	assert.NotErrorIs(t, err, looper.ErrFailedTasks)
	assert.Nil(t, tasklist)
}

func TestLoadGiven_FailedTask(t *testing.T) {
	tasksPath := filepath.Join(t.TempDir(), "tasks.yaml")
	failedYAML := "tasks:\n  - id: 1\n    name: first\n    prompt: do it\n    state: failed\n"
	require.NoError(t, os.WriteFile(tasksPath, []byte(failedYAML), 0o600))

	_, _, err := loadGiven(tasksPath, "")

	require.ErrorIs(t, err, looper.ErrFailedTasks)
	assert.False(t, strings.HasPrefix(err.Error(), "failed to start loop runner"), err.Error())
}

func TestLoadGiven_Valid(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.yaml")
	require.NoError(t, os.WriteFile(tasksPath, []byte(validTasksYAML), 0o600))
	promptPath := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("  shared prompt\n"), 0o600))

	prompt, tasklist, err := loadGiven(tasksPath, promptPath)

	require.NoError(t, err)
	assert.Equal(t, "shared prompt", prompt)
	require.NotNil(t, tasklist)
	require.Len(t, tasklist.Tasks, 1)
	assert.Equal(t, "first", tasklist.Tasks[0].Name)
}

func TestIsPlain(t *testing.T) {
	tests := []struct {
		name                               string
		dryRun, noTUI, stdinTTY, stdoutTTY bool
		want                               bool
	}{
		{name: "dry run", dryRun: true, stdinTTY: true, stdoutTTY: true, want: true},
		{name: "no tui", noTUI: true, stdinTTY: true, stdoutTTY: true, want: true},
		{name: "stdin not a terminal", stdoutTTY: true, want: true},
		{name: "stdout not a terminal", stdinTTY: true, want: true},
		{name: "all terminals, no flags", stdinTTY: true, stdoutTTY: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPlain(tt.dryRun, tt.noTUI, tt.stdinTTY, tt.stdoutTTY))
		})
	}
}

func TestCheckGateTimeout(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "valid", in: "2m"},
		{name: "unitless", in: "30", wantErr: "--gate-timeout: must be a duration string such as 90s or 10m"},
		{name: "empty", in: "", wantErr: "--gate-timeout: must be a duration string such as 90s or 10m"},
		{name: "zero", in: "0s", wantErr: "--gate-timeout: must be greater than zero"},
		{name: "negative", in: "-5m", wantErr: "--gate-timeout: must be greater than zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkGateTimeout(tt.in)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}

			assert.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestCheckLogDir(t *testing.T) {
	tests := []struct {
		name          string
		logDir        string
		plain, dryRun bool
		wantErr       string
	}{
		{name: "not passed, plain", plain: true},
		{name: "not passed, full-screen"},
		{name: "full-screen", logDir: "logs"},
		{name: "plain", logDir: "logs", plain: true, wantErr: "--log-dir only works with the full-screen view"},
		{name: "dry run ignores it", logDir: "logs", plain: true, dryRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkLogDir(tt.logDir, tt.plain, tt.dryRun)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestResolveDir(t *testing.T) {
	full := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(full, "tasks.yaml"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(full, "prompt.md"), []byte("x"), 0o600))
	noPrompt := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(noPrompt, "tasks.yaml"), []byte("x"), 0o600))
	empty := t.TempDir()

	cases := map[string]struct {
		dir, tasks, prompt string
		dryRun             bool
		wantTasks          string
		wantPrompt         string
		wantErr            string
	}{
		"no folder leaves the flags alone": {tasks: "a.yaml", prompt: "b.md", wantTasks: "a.yaml", wantPrompt: "b.md"},
		"folder fills both":                {dir: full, wantTasks: filepath.Join(full, "tasks.yaml"), wantPrompt: filepath.Join(full, "prompt.md")},
		"flag wins over the folder":        {dir: full, tasks: "other.yaml", wantTasks: "other.yaml", wantPrompt: filepath.Join(full, "prompt.md")},
		"missing tasks.yaml":               {dir: empty, wantErr: "--dir: no tasks.yaml in " + empty},
		"missing prompt.md":                {dir: noPrompt, wantErr: "--dir: no prompt.md in " + noPrompt},
		"dry run needs no prompt.md":       {dir: noPrompt, dryRun: true, wantTasks: filepath.Join(noPrompt, "tasks.yaml"), wantPrompt: ""},
		"a flag fills the missing file":    {dir: noPrompt, prompt: "p.md", wantTasks: filepath.Join(noPrompt, "tasks.yaml"), wantPrompt: "p.md"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotTasks, gotPrompt, err := resolveDir(tc.dir, tc.tasks, tc.prompt, tc.dryRun)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantTasks, gotTasks)
			assert.Equal(t, tc.wantPrompt, gotPrompt)
		})
	}
}

func TestOpenLog_NotPassedOpensNothing(t *testing.T) {
	runLog, err := openLog("", nil, runlog.Info{})

	require.NoError(t, err)
	assert.Nil(t, runLog)
}

func TestOpenLog_CreatesOneRunFolderWithALedger(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")

	runLog, err := openLog(logDir, nil, runlog.Info{Version: "dev", Permissions: "skip"})
	require.NoError(t, err)
	require.NotNil(t, runLog)
	require.NoError(t, runLog.Close())

	entries, err := os.ReadDir(logDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Regexp(t, `^\d{8}T\d{6}$`, entries[0].Name())
	assert.FileExists(t, filepath.Join(logDir, entries[0].Name(), "run.jsonl"))
}

// --log-dir naming a file, or a path under one, must fail at startup with
// the flag's name in the error.
func TestOpenLog_PathThatCannotBeADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	for _, logDir := range []string{file, filepath.Join(file, "logs")} {
		runLog, err := openLog(logDir, nil, runlog.Info{})

		require.Error(t, err, logDir)
		assert.True(t, strings.HasPrefix(err.Error(), "--log-dir: "), err.Error())
		assert.Nil(t, runLog)
	}
}
