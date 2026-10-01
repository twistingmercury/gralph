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
