# Leftover sandbox stubs after a cancelled run

> **Date**: 2026-10-02  
> **Status**: Reproduced. No fix made; this file records the test and what it found.  
> **Tested with**: gralph v0.9.8, Claude Code 2.1.287, bubblewrap, Linux (amd64)

## Summary

Cancelling a sandboxed run with Ctrl-C leaves empty, read-only dotfiles in the
work tree. They stay there until someone deletes them, and they make the next
`--commit` run refuse to start.

## What the stubs are

While a sandboxed session runs a shell command, Claude Code's sandbox hides a
set of paths in the work tree from that command. For each hidden path that does
not exist, an empty read-only file is created on disk so there is something to
mask. Seen from inside the sandbox, each one is a character device (`/dev/null`
bound over it). Seen from outside, each is a zero-byte file with mode `0444`.

In an otherwise empty repository, one session created 23 of them.

In the work tree root (11):

```text
.bash_profile  .bashrc  .gitconfig  .gitmodules  .idea  .mcp.json
.profile  .ripgreprc  .vscode  .zprofile  .zshrc
```

In `.claude/` (12):

```text
agents  commands  hooks  launch.json  loop.md  output-styles  routines
scheduled_tasks.json  settings.json  settings.local.json  skills  workflows
```

`.claude/` also gets an empty `.cc-writes/` directory. That one stays behind
even after a clean finish, but git does not track empty directories, so it
never shows up in `git status`.

A path that already exists in the work tree is not stubbed, so a real project
sees fewer than 23.

## Test setup

Everything ran in a throwaway git repository holding one committed file, with
the real `claude` CLI, in plain mode.

Sandbox settings:

```json
{
  "sandbox": {
    "filesystem": {
      "denyRead": ["~/"],
      "allowRead": ["/path/to/the/throwaway/repo"]
    }
  }
}
```

The one task asked the session to run this in a single shell call:

```bash
ls -la . > inside_listing.txt; sleep 60
```

The `sleep` holds the session inside a sandboxed command long enough to look at
the work tree from outside and to send a signal. The run was started from the
throwaway repository with:

```bash
gralph -p prompt.md -t tasks.yaml --sandbox-settings sandbox.json --no-tui
```

Gralph was started with every `CLAUDE*` environment variable unset, because the
test was driven from inside a Claude Code session.

## Results

| Trial                                                 | Stubs left behind                            | Task state on disk          |
| ----------------------------------------------------- | -------------------------------------------- | --------------------------- |
| Session runs to a clean finish                        | None (only the empty `.claude/.cc-writes/`)  | `completed`                 |
| SIGINT to gralph mid-session (what Ctrl-C does)       | All 23                                       | Unchanged                   |
| SIGTERM to the `claude` process mid-session           | None                                         | `failed`, `exit status 143` |
| A second session, run to a clean finish over leftovers | All 23 still there                           | `completed`                 |
| A `--commit` run over leftovers                       | Refused before any session started           | Unchanged                   |

The details behind each row:

- **Mid-session**, all 23 stubs are on disk and `git status` lists them as
  untracked.
- **After a clean finish**, Claude Code has removed them itself.
- **After Ctrl-C**, gralph reports
  `task 1: List and sleep failed: signal: killed` and every stub is still on
  disk. No session process survived. The task's state was not changed, as a
  cancel promises.
- **After SIGTERM sent straight to `claude`**, the stubs were gone within eight
  seconds. Claude Code cleans up when it is asked to stop.
- **A later session does not clean up old stubs.** A session only removes what
  it created; paths that already exist are left alone, and that includes stubs
  from a killed session.
- **`--commit` is blocked.** The run exits 1 with
  `--commit needs a clean work tree; commit, stash, or remove:` followed by
  every stub.

## Cause

Claude Code removes the stubs when the session ends. On a cancel, gralph kills
the session's whole process group with SIGKILL (`killProcessTree` in
`src/internal/looper/process_tree_unix.go`), so Claude Code never gets to run
that cleanup.

## What it means for a user

- After cancelling a sandboxed run, the work tree holds up to 23 empty files
  that were not there before.
- The next `--commit` run refuses to start until they are removed.
- Without `--commit`, a session that stages everything itself (`git add -A`)
  would commit them.
- Two of the stubs are an empty `.claude/settings.json` and an empty
  `.mcp.json`, files Claude Code reads. The later session in this test still
  ran normally with them present.

The workaround is to delete the empty files by hand. If nothing else untracked
in the work tree matters, `git clean -fd` does it.

## Not tested

- **A fix.** Git is already stopped with SIGTERM first and SIGKILL after a
  grace period (`stopProcessTree`). The SIGTERM trial suggests the same
  treatment would let a session clean up, but the signal there went to the
  `claude` process alone, not to its process group the way gralph sends it, and
  no time limit was measured.
- **A session killed between shell commands**, when no sandboxed command is
  running.
- **A gate timeout or a cancel during the commit.** Neither involves a session,
  so neither should leave stubs.
- **macOS.** Its sandbox (Seatbelt) works differently and may not create stubs
  at all.
- **The full-screen view.** It cancels a session through the same code, so the
  result should match.
