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
	"slices"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// Repo is the git work tree a --commit run commits into (ADR-015). A nil
// *Repo means the run never calls git.
type Repo struct {
	root string
}

// Root returns the work tree's top-level directory.
func (r *Repo) Root() string {
	return r.root
}

// OpenRepo finds the git work tree around the working directory for a
// --commit run. Outside a work tree, or without git installed, it returns nil
// and no error: there is nothing to commit, and gralph does not require a
// repository. Any other git failure is an error, so a repository gralph cannot
// read never turns into a run that silently commits nothing. Inside a work
// tree, git must not see tasksFile, and the tree must be clean, so that each
// commit holds exactly one task's work.
func OpenRepo(tasksFile string) (*Repo, error) {
	root, err := findRoot()
	if err != nil || root == "" {
		return nil, err
	}

	r := &Repo{root: root}
	if err := r.checkTaskFile(tasksFile); err != nil {
		return nil, err
	}

	if err := r.checkClean(); err != nil {
		return nil, err
	}

	return r, nil
}

// notARepo are the messages git gives when the working directory has no work
// tree: no repository at all, or a bare repository or .git directory.
var notARepo = []string{"not a git repository", "must be run in a work tree"}

// findRoot returns the work tree's root, or "" and no error when there is none.
func findRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	// git's message is matched below, so it must not be localized.
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return "", nil
	}

	gitStderr := stderr.String()
	if slices.ContainsFunc(notARepo, func(msg string) bool { return strings.Contains(gitStderr, msg) }) {
		return "", nil
	}

	reason := gitMessage(err, gitStderr)
	return "", fmt.Errorf("--commit: git rev-parse: %s", reason)
}

// gitMessage is why a git command failed: the first line of its stderr
// without git's "fatal: " prefix, or err when git said nothing.
func gitMessage(err error, stderr string) string {
	trimmed := strings.TrimSpace(stderr)
	first := firstLine(trimmed)
	msg := strings.TrimPrefix(first, "fatal: ")
	if msg == "" {
		return err.Error()
	}

	return msg
}

// checkTaskFile refuses a task file git can see. Gralph rewrites it after
// every task and never names it to git, so an ignore rule is all that keeps
// it out of the commits; a tracked file is not ignored, however its name
// matches a pattern. A file outside the work tree needs no check.
func (r *Repo) checkTaskFile(tasksFile string) error {
	rel, ok := r.relative(tasksFile)
	if !ok {
		return nil
	}

	ignored, err := r.ignored(rel)
	if err != nil {
		return err
	}

	if !ignored {
		return fmt.Errorf("--commit needs the task file ignored by git or outside the repository: %s", rel)
	}

	return nil
}

// checkClean refuses a work tree with uncommitted changes.
func (r *Repo) checkClean() error {
	// Every untracked file is listed by name, and quotePath keeps non-ASCII
	// names readable in the error.
	dirty, stderr, err := r.output(context.Background(), "-c", "core.quotePath=false", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		reason := gitMessage(err, stderr)
		return fmt.Errorf("--commit: git status: %s", reason)
	}

	changes := bytes.TrimSpace(dirty)
	if len(changes) > 0 {
		files := strings.TrimRight(string(dirty), "\n")
		return fmt.Errorf("--commit needs a clean work tree; commit, stash, or remove:\n%s", files)
	}

	return nil
}

// relative returns path relative to the work tree's root, or false when it
// lies outside the tree. Only the parent directory is resolved: git sees a
// symlink as the link itself, so a link to a file elsewhere is still inside.
func (r *Repo) relative(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}

	// git reports the root with symlinks resolved, so the path must be too.
	dir := filepath.Dir(abs)
	resolvedDir := resolveExisting(dir)
	base := filepath.Base(abs)
	abs = filepath.Join(resolvedDir, base)

	rel, err := filepath.Rel(r.root, abs)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}

	return rel, true
}

// resolveExisting resolves symlinks in dir, or in its nearest existing
// ancestor when dir itself does not exist yet.
func resolveExisting(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}

	parent := filepath.Dir(dir)
	if parent == dir {
		return dir
	}

	resolvedParent := resolveExisting(parent)
	base := filepath.Base(dir)
	return filepath.Join(resolvedParent, base)
}

// ignored reports whether git ignores rel, a path relative to the root.
// check-ignore exits 0 for ignored and 1 for not ignored; anything else is a
// failure, not an answer.
func (r *Repo) ignored(rel string) (bool, error) {
	_, stderr, err := r.output(context.Background(), "check-ignore", "-q", "--", rel)

	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	default:
		reason := gitMessage(err, stderr)
		return false, fmt.Errorf("--commit: git check-ignore: %s", reason)
	}
}

// git builds a git command run from the root, so it means the same wherever
// gralph was started. Like a gate it gets no stdin and its own process group,
// so a cancel also stops any hook. The command name stays a constant; args
// are appended after it.
func (r *Repo) git(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git")
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = r.root
	configureGitProcessTree(cmd)
	return cmd
}

// output runs a git command and returns its stdout and, for a failure's
// message, its stderr.
func (r *Repo) output(ctx context.Context, args ...string) (stdout []byte, stderr string, err error) {
	var out, errOut bytes.Buffer
	cmd := r.git(ctx, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.String(), err
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
	addCmd := r.git(ctx, "add", "-A")
	if err := runGit(addCmd, task, report); err != nil {
		return err
	}

	staged, err := r.staged(ctx)
	if err != nil || !staged {
		return err
	}

	announceCommit(task, report)
	commitCmd := r.git(ctx, "commit", "-m", task.Name)
	return runGit(commitCmd, task, report)
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
