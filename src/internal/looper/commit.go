package looper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// Repo is the git work tree a --commit run commits into (ADR-015). A nil
// *Repo means the run never calls git.
type Repo struct {
	root string
	// paths limits every git call to the whole work tree minus the task
	// file, which gralph rewrites after each task and never commits. A
	// git-ignored task file is not excluded: git add refuses a pathspec that
	// names an ignored path, and the file is never staged anyway.
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
	if rel, ok := r.relative(tasksFile); ok && !r.ignored(rel) {
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

// ignored reports whether git ignores rel, a path relative to the root. A
// tracked file that matches an ignore pattern is not ignored in this sense:
// it can be staged, so it still needs its exclude. Any error, including the
// exit 1 that means "not ignored", reads as false.
func (r *Repo) ignored(rel string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", "--")
	cmd.Args = append(cmd.Args, rel)
	cmd.Dir = r.root

	return cmd.Run() == nil
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
