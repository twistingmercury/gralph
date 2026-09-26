package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/skillinstall"
	"github.com/twistingmercury/gralph/internal/tui"
	"github.com/twistingmercury/gralph/internal/version"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/pflag"
)

var (
	versionFlag = pflag.BoolP("version", "v", false, "Show the current version of gralph")
	tasksFlag   = pflag.StringP("tasks", "t", "", "Required. Path to the tasks.yaml task list that drives the loop")
	promptFlag  = pflag.StringP("prompt", "p", "", "Required unless --dry-run. Path to the prompt.md shared prompt passed to Claude with every task")
	dryRunFlag  = pflag.Bool("dry-run", false, "Validate the tasks file and report failed tasks without running anything")
	installFlag = pflag.Bool("install-skill", false, "Install the gralph-docs-writer skill bundled with this binary into ~/.claude/skills")
	noTUIFlag   = pflag.Bool("no-tui", false, "Use plain output instead of the full-screen view")
)

func main() {
	pflag.Parse()
	checkVersion()
	checkInstallSkill()
	plain := *dryRunFlag || *noTUIFlag || !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd())
	validateRequiredFlags()

	if err := skillinstall.Check(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dryRunFlag {
		if err := looper.DryRun(os.Stdout, *tasksFlag); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !plain {
		os.Exit(runTUI(ctx))
	}

	if err := looper.Start(ctx, *promptFlag, *tasksFlag); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runTUI loads the prompt and tasks like looper.Start, runs the loop in the
// full-screen view, prints its summary, and returns the exit code.
func runTUI(ctx context.Context) int {
	prompt, err := looper.LoadPrompt(*promptFlag)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: failed to start loop runner: %v\n", err)
		return 1
	}

	tasklist, err := looper.LoadTasks(*tasksFlag)
	if errors.Is(err, looper.ErrFailedTasks) {
		fmt.Println("Some tasks failed previous runs:")
		looper.PrintTasks(os.Stdout, tasklist)
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: failed to start loop runner: %v\n", err)
		return 1
	}

	code, summary, err := tui.Run(ctx, prompt, tasklist, *tasksFlag)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\nrun with --no-tui to use plain output\n", err)
		return 1
	}

	fmt.Println(summary)
	return code
}

func checkVersion() {
	if !*versionFlag {
		return
	}

	version.Print()
	os.Exit(0)
}

func checkInstallSkill() {
	if !*installFlag {
		return
	}

	path, err := skillinstall.Install()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(path)
	os.Exit(0)
}

func validateRequiredFlags() {
	var missing []string
	if *promptFlag == "" && !*dryRunFlag {
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
