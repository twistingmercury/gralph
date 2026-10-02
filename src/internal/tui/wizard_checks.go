package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/tasks"
)

const (
	permSandbox = "sandbox"
	permSkip    = "skip"

	timeoutCustom = "custom"
)

// checkFolder loads the folder's files with the loaders a run uses, so a bad
// folder is refused on the step instead of after the review screen. A path
// given by flag wins over the folder's file. Settings change only on success.
func (w *wizard) checkFolder(dir string) error {
	tasksPath, promptPath := w.s.TasksPath, w.s.PromptPath
	if tasksPath == "" {
		tasksPath = filepath.Join(dir, "tasks.yaml")
	}

	if promptPath == "" {
		promptPath = filepath.Join(dir, "prompt.md")
	}

	if err := requireIn(tasksPath, dir, "tasks.yaml"); err != nil {
		return err
	}

	if err := requireIn(promptPath, dir, "prompt.md"); err != nil {
		return err
	}

	tl, err := looper.LoadTasks(tasksPath)
	if errors.Is(err, looper.ErrFailedTasks) {
		return fmt.Errorf("%s has failed tasks; run gralph -d %s --dry-run to see them", tasksPath, dir)
	}

	if err != nil {
		return err
	}

	prompt, err := looper.LoadPrompt(promptPath)
	if err != nil {
		return err
	}

	w.s.Dir, w.s.TasksPath, w.s.PromptPath = dir, tasksPath, promptPath
	w.s.Tasks, w.s.Prompt = tl, prompt
	return nil
}

// requireIn names the folder rather than the path, because the user picked a
// folder; the --dir: prefix belongs to the flag, not the wizard.
func requireIn(path, dir, name string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no %s in %s", name, dir)
	}

	return nil
}

// checkPermission refuses an empty choice, so Enter on a step the user never
// moved through chooses nothing (ADR-014).
func checkPermission(choice string) error {
	if choice == "" {
		return errors.New("choose how sessions run")
	}

	return nil
}

// checkSandboxFile reads the file the way a run does, so a file the run would
// refuse never gets past the step.
func checkSandboxFile(path string) error {
	_, err := looper.SandboxArgs(path)
	return err
}

func checkCustomTimeout(value string) error {
	if value == "" {
		return errors.New("enter a timeout such as 90s or 10m")
	}

	_, err := tasks.ParseTimeout(value)
	return err
}

// checkCommit opens the repository the way a --commit run does, so a dirty
// tree or a task file git can see is refused here, not after the review
// screen.
func (w *wizard) checkCommit(yes bool) error {
	if !yes {
		return nil
	}

	_, err := looper.OpenRepo(w.s.TasksPath)
	return err
}

// checkLogging refuses a log folder git would commit (ADR-016). Commit may
// have come from the flag (its step hidden) or the wizard, so both are
// asked. CheckLogDir needs a path inside the folder, because git matches a
// directory pattern such as "logs/" only for paths it can tell lie inside it;
// the run's own folder does not exist yet, so a stand-in name is used.
func (w *wizard) checkLogging(yes bool) error {
	if !yes {
		w.s.LogDir = ""
		return nil
	}

	logDir := filepath.Join(w.s.Dir, "logs")
	if w.s.Commit || w.commit {
		if err := checkLogDirIgnored(w.s.TasksPath, logDir); err != nil {
			return err
		}
	}

	w.s.LogDir = logDir
	return nil
}

func checkLogDirIgnored(tasksPath, logDir string) error {
	repo, err := looper.OpenRepo(tasksPath)
	if err != nil {
		return err
	}

	probe := filepath.Join(logDir, "run")
	return repo.CheckLogDir(probe)
}
