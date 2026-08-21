package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type invocation struct {
	Prompt       string `json:"prompt"`
	PRDPath      string `json:"prd_path"`
	ProgressPath string `json:"progress_path"`
}

func main() {
	recordPath := flag.String("record", "", "path to write the captured invocation")
	attemptLogPath := flag.String("attempt-log", "", "path to append one line per invocation")
	block := flag.Bool("block", false, "block until the caller terminates the process")
	readyPath := flag.String("ready-file", "", "path to write the process ID before blocking")
	exitCode := flag.Int("exit-code", 0, "exit unsuccessfully before changing the PRD")
	flag.Parse()
	if *block && *recordPath == "" {
		blockForever()
	}
	if *recordPath == "" {
		fail("--record is required")
	}

	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail("read stdin: %v", err)
	}
	inv := invocation{
		Prompt:       string(prompt),
		PRDPath:      runtimePath(string(prompt), "PRD"),
		ProgressPath: runtimePath(string(prompt), "Progress"),
	}
	if inv.PRDPath == "" || inv.ProgressPath == "" {
		fail("prompt does not contain both runtime paths")
	}

	data, err := json.Marshal(inv)
	if err != nil {
		fail("marshal invocation: %v", err)
	}
	if err := os.WriteFile(*recordPath, data, 0o600); err != nil {
		fail("write invocation: %v", err)
	}
	if *attemptLogPath != "" {
		attemptLog, err := os.OpenFile(*attemptLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is supplied by the e2e test
		if err != nil {
			fail("open attempt log: %v", err)
		}
		if _, err := fmt.Fprintln(attemptLog, "attempt"); err != nil {
			_ = attemptLog.Close()
			fail("write attempt log: %v", err)
		}
		if err := attemptLog.Close(); err != nil {
			fail("close attempt log: %v", err)
		}
	}
	if *readyPath != "" {
		if err := os.WriteFile(*readyPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			fail("write ready file: %v", err)
		}
	}
	if *block {
		blockForever()
	}
	if *exitCode != 0 {
		fmt.Fprintf(os.Stderr, "fake agent exiting with status %d\n", *exitCode)
		os.Exit(*exitCode)
	}

	prd, err := os.ReadFile(inv.PRDPath)
	if err != nil {
		fail("read PRD: %v", err)
	}
	updated := strings.Replace(string(prd), "- [ ]", "- [x]", 1)
	if updated == string(prd) {
		fail("PRD has no open checklist item")
	}
	if err := os.WriteFile(inv.PRDPath, []byte(updated), 0o600); err != nil {
		fail("write PRD: %v", err)
	}

	fmt.Println("fake agent completed first checklist item")
}

func blockForever() {
	for {
		time.Sleep(time.Hour)
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

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fake agent: "+format+"\n", args...)
	os.Exit(1)
}
