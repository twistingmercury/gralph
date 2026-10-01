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
	"github.com/twistingmercury/gralph/internal/tasks"
	"github.com/twistingmercury/gralph/internal/tui"
	"github.com/twistingmercury/gralph/internal/version"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/pflag"
)

var (
	versionFlag     = pflag.BoolP("version", "v", false, "Show the current version of gralph")
	tasksFlag       = pflag.StringP("tasks", "t", "", "Path to the tasks.yaml task list that drives the loop; required, but asked for when missing in the full-screen view")
	promptFlag      = pflag.StringP("prompt", "p", "", "Path to the prompt.md shared prompt passed to Claude with every task; required unless --dry-run, but asked for when missing in the full-screen view")
	dryRunFlag      = pflag.Bool("dry-run", false, "Validate the tasks file and report failed tasks without running anything")
	installFlag     = pflag.Bool("install-skill", false, "Install the gralph-docs-writer skill bundled with this binary into ~/.claude/skills")
	gateTimeoutFlag = pflag.String("gate-timeout", "", "Limit for every gate, such as 90s or 10m; overrides each gate's own timeout (default: the gate's timeout, else 10m)")
	noTUIFlag       = pflag.Bool("no-tui", false, "Use plain output instead of the full-screen view")
	sandboxFlag     = pflag.String("sandbox-settings", "", "Path to a Claude Code settings JSON file; sessions run in Claude's sandbox with it. A run needs this or --skip-permissions")
	skipPermsFlag   = pflag.Bool("skip-permissions", false, "Run sessions with no sandbox and no permission checks (claude --dangerously-skip-permissions); what they do is on you")
	commitFlag      = pflag.Bool("commit", false, "Commit each completed task with git after its gates pass, with the task name as the message; in a repository, the work tree must be clean")
)

func main() {
	pflag.Parse()
	checkVersion()
	checkInstallSkill()
	validateGateTimeout()
	stdinFd := os.Stdin.Fd()
	stdinTTY := term.IsTerminal(stdinFd)
	stdoutFd := os.Stdout.Fd()
	stdoutTTY := term.IsTerminal(stdoutFd)
	plain := isPlain(*dryRunFlag, *noTUIFlag, stdinTTY, stdoutTTY)
	if plain {
		validateRequiredFlags()
	}

	session := validateSessionFlags()

	if err := skillinstall.Check(); err != nil {
		fatal(err)
	}

	if *dryRunFlag {
		if err := looper.DryRun(os.Stdout, *tasksFlag, *gateTimeoutFlag, *sandboxFlag, *commitFlag); err != nil {
			fatal(err)
		}

		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !plain {
		exitCode := runTUI(ctx, session)
		os.Exit(exitCode)
	}

	if err := looper.Start(ctx, *promptFlag, *tasksFlag, *gateTimeoutFlag, session, *commitFlag); err != nil {
		fatal(err)
	}
}

// fatal is the one way to fail at startup, so every startup error has the
// same "error: " prefix and exit code 1.
func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// isPlain reports whether gralph runs in plain mode: on --dry-run or
// --no-tui, or when stdin or stdout is not a terminal.
func isPlain(dryRun, noTUI, stdinTTY, stdoutTTY bool) bool {
	return dryRun || noTUI || !stdinTTY || !stdoutTTY
}

// runTUI loads the given prompt and tasks like looper.Start, asks for any
// missing path on the setup screen, runs the loop in the full-screen view,
// prints its summary, and returns the exit code. session is the claude flags
// from validateSessionFlags.
func runTUI(ctx context.Context, session []string) int {
	tasksPath, promptPath := *tasksFlag, *promptFlag

	prompt, tasklist, err := loadGiven(tasksPath, promptPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if promptPath == "" || tasksPath == "" {
		s, err := tui.Setup(tasksPath, promptPath)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %v\nrun with --no-tui to use plain output\n", err)
			return 1
		}

		if s.Cancelled() {
			_, _ = fmt.Fprintln(os.Stderr, "error: setup cancelled")
			return 1
		}

		if tasksPath == "" {
			tasksPath, tasklist = s.TasksPath(), s.Tasks()
		}

		if promptPath == "" {
			prompt = s.Prompt()
		}
	}

	repo, err := openRepo(tasksPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	code, summary, err := tui.Run(ctx, prompt, tasklist, tasksPath, *gateTimeoutFlag, session, repo, nil)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\nrun with --no-tui to use plain output\n", err)
		return 1
	}

	fmt.Println(summary)
	return code
}

// loadGiven loads the prompt and tasks whose paths were passed by flag; an
// empty path is left for the setup screen. The prompt loads first so a bad
// prompt fails before any failed-tasks table is printed. ErrFailedTasks is
// returned bare because its table is the explanation; any other error is
// wrapped like looper.Start's.
func loadGiven(tasksPath, promptPath string) (string, *tasks.TaskList, error) {
	var prompt string
	if promptPath != "" {
		var err error
		prompt, err = looper.LoadPrompt(promptPath)
		if err != nil {
			return "", nil, fmt.Errorf("failed to start loop runner: %w", err)
		}
	}

	if tasksPath == "" {
		return prompt, nil, nil
	}

	tasklist, err := looper.LoadTasksReport(os.Stdout, tasksPath)
	if errors.Is(err, looper.ErrFailedTasks) {
		return "", nil, err
	}

	if err != nil {
		return "", nil, fmt.Errorf("failed to start loop runner: %w", err)
	}

	return prompt, tasklist, nil
}

// openRepo opens the work tree for --commit; without the flag git is never
// called. The view has nowhere to print plain mode's "not a git repository"
// notice, so it is opened without it.
func openRepo(tasksPath string) (*looper.Repo, error) {
	if !*commitFlag {
		return nil, nil
	}

	return looper.OpenRepo(tasksPath)
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
		fatal(err)
	}

	fmt.Println(path)
	os.Exit(0)
}

// validateGateTimeout rejects a bad --gate-timeout before anything loads or
// runs. An explicitly empty value counts as passed, and is bad.
func validateGateTimeout() {
	if !pflag.CommandLine.Changed("gate-timeout") {
		return
	}

	if err := checkGateTimeout(*gateTimeoutFlag); err != nil {
		fatal(err)
	}
}

// checkGateTimeout applies the same rules as a gate's timeout in the file.
func checkGateTimeout(v string) error {
	if _, err := tasks.ParseTimeout(v); err != nil {
		return fmt.Errorf("--gate-timeout: %w", err)
	}

	return nil
}

// validateSessionFlags exits unless the run was told how far to trust its
// sessions; see sessionArgs.
func validateSessionFlags() []string {
	args, err := sessionArgs(*sandboxFlag, *skipPermsFlag, *dryRunFlag)
	if err != nil {
		fatal(err)
	}

	return args
}

// sessionArgs returns the claude flags that set what a session may do. A run
// must choose a sandbox or ask by name to go without one (ADR-014): there is
// no default, so nobody runs unsandboxed by accident. A dry run starts no
// session and needs neither, but its sandbox file is still checked.
func sessionArgs(sandboxFile string, skip, dryRun bool) ([]string, error) {
	if sandboxFile != "" && skip {
		return nil, errors.New("--sandbox-settings and --skip-permissions cannot be used together")
	}

	if sandboxFile != "" {
		return looper.SandboxArgs(sandboxFile)
	}

	if skip {
		return looper.BypassArgs(), nil
	}

	if dryRun {
		return nil, nil
	}

	return nil, errors.New("pass --sandbox-settings <path>, or --skip-permissions to run without a sandbox")
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
