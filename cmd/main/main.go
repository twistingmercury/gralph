package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/version"

	"github.com/spf13/pflag"
)

var (
	versionFlag = pflag.BoolP("version", "v", false, "Show the current version of gralph")
	tasksFlag   = pflag.StringP("tasks", "t", "", "Required. Path to the tasks.yaml task list that drives the loop")
	promptFlag  = pflag.StringP("prompt", "p", "", "Required. Path to the prompt.md shared prompt passed to Claude with every task")
)

func main() {
	pflag.Parse()
	checkVersion()
	validateRequiredFlags()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := looper.Start(ctx, *promptFlag, *tasksFlag); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func checkVersion() {
	if !*versionFlag {
		return
	}

	version.Print()
	os.Exit(0)
}

func validateRequiredFlags() {
	var missing []string
	if *promptFlag == "" {
		missing = append(missing, "--prompt")
	}

	if *tasksFlag == "" {
		missing = append(missing, "--tasks")
	}

	if len(missing) == 0 {
		return
	}

	for _, f := range missing {
		_, _ = fmt.Fprintf(os.Stderr, "error: required flag %s not set\n", f)
	}

	_, _ = fmt.Fprintln(os.Stderr)
	pflag.Usage()
	os.Exit(1)
}
