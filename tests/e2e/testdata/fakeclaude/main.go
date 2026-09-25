// Command fakeclaude is a scriptable stand-in for the `claude` executable
// used by the gralph e2e suite. gralph always invokes claude with a fixed
// argv (`--print --dangerously-skip-permissions`) and the assembled prompt
// on stdin, so this fixture cannot be driven via flags the way a normal CLI
// fixture would be. Instead every behavior is controlled through
// FAKECLAUDE_* environment variables, documented below.
//
// Environment variables:
//
//	FAKECLAUDE_MODE                empty for normal operation, or "descendant"
//	                                to run as a blocking descendant fixture
//	                                spawned by a normal-mode invocation (see
//	                                FAKECLAUDE_DESCENDANT_PID_FILE). Descendant
//	                                mode ignores every other variable, reads
//	                                nothing, and blocks until killed.
//	FAKECLAUDE_RECORD_FILE         path to append a newline-delimited JSON
//	                                record of this invocation
//	                                ({"argv":[...],"stdin":"..."}), one line
//	                                per invocation in call order, so a test
//	                                can recover every invocation across a
//	                                single gralph run
//	FAKECLAUDE_ATTEMPT_LOG_FILE    path to append one line to per invocation,
//	                                used to count invocations across a run
//	FAKECLAUDE_EXIT_CODE           exit code to use (default "0")
//	FAKECLAUDE_STDOUT_MSG          text to print to stdout before exiting
//	FAKECLAUDE_STDERR_MSG          text to print to stderr before exiting
//	FAKECLAUDE_DESCENDANT_PID_FILE path to write the PID of a spawned
//	                                blocking descendant process (empty: no
//	                                descendant is spawned)
//	FAKECLAUDE_BLOCK               "1" to block forever after reading stdin
//	                                and completing setup (descendant spawn,
//	                                ready file) instead of exiting
//	FAKECLAUDE_READY_FILE          path to write this process's own PID once
//	                                setup is complete and it is about to
//	                                block or exit
//	FAKE_CLAUDE_OUTPUT             session-result stdout content, checked via
//	                                os.LookupEnv so it is honored even when set
//	                                to the empty string. If present, its value
//	                                is written verbatim to stdout before
//	                                exiting (letting a test control the
//	                                session-result line gralph parses,
//	                                including invalid/missing/fenced cases).
//	                                If absent, the default is
//	                                `{"state":"completed","error":""}` on a
//	                                zero exit code and nothing on a non-zero
//	                                exit code.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type record struct {
	Argv  []string `json:"argv"`
	Stdin string   `json:"stdin"`
}

func main() {
	if os.Getenv("FAKECLAUDE_MODE") == "descendant" {
		blockForever()
	}

	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail("read stdin: %v", err)
	}

	if recordFile := os.Getenv("FAKECLAUDE_RECORD_FILE"); recordFile != "" {
		rec := record{Argv: os.Args[1:], Stdin: string(stdin)}
		data, err := json.Marshal(rec)
		if err != nil {
			fail("marshal record: %v", err)
		}
		data = append(data, '\n')
		if err := appendUnderRoot(recordFile, data, 0o600); err != nil {
			fail("write record file: %v", err)
		}
	}

	if attemptLog := os.Getenv("FAKECLAUDE_ATTEMPT_LOG_FILE"); attemptLog != "" {
		if err := appendUnderRoot(attemptLog, []byte("attempt\n"), 0o600); err != nil {
			fail("write attempt log: %v", err)
		}
	}

	if descendantPIDFile := os.Getenv("FAKECLAUDE_DESCENDANT_PID_FILE"); descendantPIDFile != "" {
		self, err := os.Executable()
		if err != nil {
			fail("resolve self executable: %v", err)
		}
		cmd := exec.Command(self)
		cmd.Env = append(os.Environ(), "FAKECLAUDE_MODE=descendant")
		if err := cmd.Start(); err != nil {
			fail("start descendant: %v", err)
		}
		if err := writeUnderRoot(descendantPIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			_ = cmd.Process.Kill()
			fail("write descendant pid file: %v", err)
		}
	}

	if readyFile := os.Getenv("FAKECLAUDE_READY_FILE"); readyFile != "" {
		if err := writeUnderRoot(readyFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			fail("write ready file: %v", err)
		}
	}

	if os.Getenv("FAKECLAUDE_BLOCK") == "1" {
		blockForever()
	}

	if msg := os.Getenv("FAKECLAUDE_STDOUT_MSG"); msg != "" {
		fmt.Println(msg)
	}
	if msg := os.Getenv("FAKECLAUDE_STDERR_MSG"); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}

	exitCode := 0
	if raw := os.Getenv("FAKECLAUDE_EXIT_CODE"); raw != "" {
		code, err := strconv.Atoi(raw)
		if err != nil {
			fail("invalid FAKECLAUDE_EXIT_CODE %q: %v", raw, err)
		}
		exitCode = code
	}

	if out, ok := os.LookupEnv("FAKE_CLAUDE_OUTPUT"); ok {
		fmt.Fprint(os.Stdout, out)
	} else if exitCode == 0 {
		fmt.Fprint(os.Stdout, `{"state":"completed","error":""}`+"\n")
	}

	os.Exit(exitCode)
}

// writeUnderRoot and appendUnderRoot confine file access to the directory
// component of the caller-supplied path via os.Root, rather than passing an
// environment-derived path straight to os.WriteFile/os.OpenFile. Neither
// path is trusted: both come from FAKECLAUDE_* environment variables set by
// the test process.

func writeUnderRoot(path string, data []byte, perm os.FileMode) error {
	return withRootFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm, data)
}

func appendUnderRoot(path string, data []byte, perm os.FileMode) error {
	return withRootFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, perm, data)
}

func withRootFile(path string, flag int, perm os.FileMode, data []byte) error {
	dir, base := filepath.Split(filepath.Clean(path))
	if dir == "" {
		dir = "."
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open root %q: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	f, err := root.OpenFile(base, flag, perm)
	if err != nil {
		return fmt.Errorf("open %q under root %q: %w", base, dir, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %q under root %q: %w", base, dir, err)
	}
	return nil
}

func blockForever() {
	for {
		time.Sleep(time.Hour)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fakeclaude: "+format+"\n", args...)
	os.Exit(1)
}
