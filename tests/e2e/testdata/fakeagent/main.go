package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type invocation struct {
	Prompt       string `json:"prompt"`
	PRDPath      string `json:"prd_path"`
	ProgressPath string `json:"progress_path"`
}

func main() {
	recordPath := flag.String("record", "", "path to write the captured invocation")
	flag.Parse()
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
