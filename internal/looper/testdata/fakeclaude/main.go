// Command fakeclaude is a minimal scriptable stand-in for the `claude`
// executable, used only by internal/looper's unit tests to exercise the
// runLoop exec path without invoking a real agent. runLoop always invokes
// claude with a fixed argv and the assembled prompt on stdin, so every
// behavior here is driven through environment variables:
//
//	FAKE_CLAUDE_EXIT   exit code to return (default "0")
//	FAKE_CLAUDE_RECORD path to append one entry per invocation: the exact
//	                    stdin bytes followed by recordSeparator, so a test
//	                    can recover each invocation's stdin verbatim and in
//	                    call order
//	FAKE_CLAUDE_BLOCK   "1" to write the ready file (if set) and then block
//	                    forever, instead of reading stdin, recording it, and
//	                    exiting
//	FAKE_CLAUDE_READY   path to write a ready marker to just before blocking
//	FAKE_CLAUDE_OUTPUT  if set (even to the empty string), its value is
//	                    written verbatim to stdout before exiting, instead of
//	                    the default result line. This lets a test control
//	                    exactly what runLoop sees as the session's result
//	                    line, including malformed or fenced output. In
//	                    stream mode it is the result event's text instead.
//	FAKE_CLAUDE_ARGS    path to append one line per invocation: the argv
//	                    (os.Args[1:]) joined by spaces
//	FAKE_CLAUDE_STDERR  text written verbatim to stderr just before exiting
//
// Stream mode is on only when argv contains "--output-format" followed by
// "stream-json". Stdout is then one JSON event per line instead of the plain
// result line: a system init event, an assistant text event, and, only on a
// zero exit, a result event whose result is FAKE_CLAUDE_OUTPUT when set (even
// empty) or the default completed result line. One variable applies only in
// stream mode:
//
//	FAKE_CLAUDE_BIG_EVENT byte count; when set, one extra user event, padded
//	                      to at least that many bytes, is written just before
//	                      the result event
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// recordSeparator delimits invocation entries appended to FAKE_CLAUDE_RECORD.
// It must match the constant of the same purpose in the looper package's
// tests.
const recordSeparator = "\x00---FAKE-CLAUDE-RECORD-SEPARATOR---\x00"

// defaultResult is the result line reported when FAKE_CLAUDE_OUTPUT is unset.
const defaultResult = `{"state":"completed","error":""}`

func main() {
	if argsFile := os.Getenv("FAKE_CLAUDE_ARGS"); argsFile != "" {
		if err := appendUnderRoot(argsFile, []byte(strings.Join(os.Args[1:], " ")+"\n"), 0o600); err != nil {
			fail("write args file: %v", err)
		}
	}

	if os.Getenv("FAKE_CLAUDE_BLOCK") == "1" {
		if readyFile := os.Getenv("FAKE_CLAUDE_READY"); readyFile != "" {
			if err := writeUnderRoot(readyFile, []byte("ready"), 0o600); err != nil {
				fail("write ready file: %v", err)
			}
		}
		blockForever()
	}

	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail("read stdin: %v", err)
	}

	if recordFile := os.Getenv("FAKE_CLAUDE_RECORD"); recordFile != "" {
		if err := appendUnderRoot(recordFile, []byte(string(stdin)+recordSeparator), 0o600); err != nil {
			fail("write record file: %v", err)
		}
	}

	exitCode := 0
	if raw := os.Getenv("FAKE_CLAUDE_EXIT"); raw != "" {
		code, err := strconv.Atoi(raw)
		if err != nil {
			fail("invalid FAKE_CLAUDE_EXIT %q: %v", raw, err)
		}
		exitCode = code
	}

	if streamMode() {
		writeStream(exitCode)
	} else {
		writeOutput(exitCode)
	}

	fmt.Fprint(os.Stderr, os.Getenv("FAKE_CLAUDE_STDERR"))
	os.Exit(exitCode)
}

// writeOutput writes fake claude's stdout result line. When FAKE_CLAUDE_OUTPUT
// is set, even to the empty string, its value is written verbatim and no
// default is synthesized. Otherwise a default result line is produced so
// tests that don't care about the result line are unaffected: a completed
// result line on a zero exit, nothing on a non-zero exit.
func writeOutput(exitCode int) {
	if out, ok := os.LookupEnv("FAKE_CLAUDE_OUTPUT"); ok {
		fmt.Print(out)
		return
	}

	if exitCode == 0 {
		fmt.Println(defaultResult)
	}
}

// streamMode reports whether argv asks for --output-format stream-json.
func streamMode() bool {
	args := os.Args[1:]
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--output-format" && args[i+1] == "stream-json" {
			return true
		}
	}
	return false
}

// writeStream writes Claude Code's stream-json events, one per line.
func writeStream(exitCode int) {
	fmt.Println(`{"type":"system","subtype":"init"}`)
	fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`)

	if raw := os.Getenv("FAKE_CLAUDE_BIG_EVENT"); raw != "" {
		size, err := strconv.Atoi(raw)
		if err != nil {
			fail("invalid FAKE_CLAUDE_BIG_EVENT %q: %v", raw, err)
		}
		const prefix, suffix = `{"type":"user","message":{"content":[{"type":"text","text":"`, `"}]}}`
		pad := max(size-len(prefix)-len(suffix), 0)
		fmt.Println(prefix + strings.Repeat("x", pad) + suffix)
	}

	if exitCode != 0 {
		return
	}
	text, ok := os.LookupEnv("FAKE_CLAUDE_OUTPUT")
	if !ok {
		text = defaultResult
	}
	encoded, err := json.Marshal(text)
	if err != nil {
		fail("encode result: %v", err)
	}
	fmt.Printf("{\"type\":\"result\",\"subtype\":\"success\",\"result\":%s}\n", encoded)
}

// writeUnderRoot and appendUnderRoot confine file access to the directory
// component of the caller-supplied path via os.Root, rather than passing an
// environment-derived path straight to os.WriteFile/os.OpenFile. Neither
// path is trusted: both come from FAKE_CLAUDE_* environment variables set by
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
