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
//	FAKECLAUDE_RECORD_FILE         path to write a JSON record of this
//	                                invocation ({"argv":[...],"stdin":"..."})
//	FAKECLAUDE_ATTEMPT_LOG_FILE    path to append one line to per invocation,
//	                                used to count invocations across a run
//	FAKECLAUDE_EXIT_CODE           exit code to use (default "0")
//	FAKECLAUDE_SKIP_PRD_UPDATE     "1" to leave the PRD untouched instead of
//	                                marking the first open item done
//	FAKECLAUDE_STDOUT_MSG          text to print to stdout before exiting
//	FAKECLAUDE_STDERR_MSG          text to print to stderr before exiting
//	FAKECLAUDE_DESCENDANT_PID_FILE path to write the PID of a spawned
//	                                blocking descendant process (empty: no
//	                                descendant is spawned)
//	FAKECLAUDE_BLOCK               "1" to block forever after setup
//	                                (descendant spawn, ready file) instead of
//	                                touching the PRD and exiting
//	FAKECLAUDE_READY_FILE          path to write this process's own PID once
//	                                setup is complete and it is about to
//	                                block or exit
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
		if err := os.WriteFile(recordFile, data, 0o600); err != nil {
			fail("write record file: %v", err)
		}
	}

	if attemptLog := os.Getenv("FAKECLAUDE_ATTEMPT_LOG_FILE"); attemptLog != "" {
		f, err := os.OpenFile(attemptLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fail("open attempt log: %v", err)
		}
		if _, err := fmt.Fprintln(f, "attempt"); err != nil {
			_ = f.Close()
			fail("write attempt log: %v", err)
		}
		if err := f.Close(); err != nil {
			fail("close attempt log: %v", err)
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
		if err := os.WriteFile(descendantPIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			_ = cmd.Process.Kill()
			fail("write descendant pid file: %v", err)
		}
	}

	if readyFile := os.Getenv("FAKECLAUDE_READY_FILE"); readyFile != "" {
		if err := os.WriteFile(readyFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			fail("write ready file: %v", err)
		}
	}

	if os.Getenv("FAKECLAUDE_BLOCK") == "1" {
		blockForever()
	}

	if os.Getenv("FAKECLAUDE_SKIP_PRD_UPDATE") != "1" {
		prdPath := runtimePath(string(stdin), "PRD")
		if prdPath == "" {
			fail("prompt does not contain a PRD runtime path")
		}
		markFirstOpenItemDone(prdPath)
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
	os.Exit(exitCode)
}

func markFirstOpenItemDone(prdPath string) {
	data, err := os.ReadFile(prdPath)
	if err != nil {
		fail("read PRD: %v", err)
	}
	updated := strings.Replace(string(data), "- [ ]", "- [x]", 1)
	if updated == string(data) {
		fail("PRD has no open checklist item")
	}
	if err := os.WriteFile(prdPath, []byte(updated), 0o600); err != nil {
		fail("write PRD: %v", err)
	}
}

func runtimePath(prompt, label string) string {
	prefix := "- " + label + ": "
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
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
