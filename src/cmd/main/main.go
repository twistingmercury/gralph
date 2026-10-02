package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/runlog"
	"github.com/twistingmercury/gralph/internal/skillinstall"
	"github.com/twistingmercury/gralph/internal/tasks"
	"github.com/twistingmercury/gralph/internal/tui"
	"github.com/twistingmercury/gralph/internal/version"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/pflag"
)

var (
	versionFlag     = pflag.BoolP("version", "v", false, "Show the current version of gralph")
	dirFlag         = pflag.StringP("dir", "d", "", "Run folder holding tasks.yaml and prompt.md; --tasks and --prompt override either file")
	tasksFlag       = pflag.StringP("tasks", "t", "", "Path to the tasks.yaml task list that drives the loop; required unless --dir gives it, but asked for when missing in the full-screen view")
	promptFlag      = pflag.StringP("prompt", "p", "", "Path to the prompt.md shared prompt passed to Claude with every task; required unless --dry-run or --dir gives it, but asked for when missing in the full-screen view")
	dryRunFlag      = pflag.Bool("dry-run", false, "Validate the tasks file and report failed tasks without running anything")
	installFlag     = pflag.Bool("install-skill", false, "Install the gralph-docs-writer skill bundled with this binary into ~/.claude/skills")
	gateTimeoutFlag = pflag.String("gate-timeout", "", "Limit for every gate, such as 90s or 10m; overrides each gate's own timeout (default: the gate's timeout, else 10m)")
	noTUIFlag       = pflag.Bool("no-tui", false, "Use plain output instead of the full-screen view")
	sandboxFlag     = pflag.String("sandbox-settings", "", "Path to a Claude Code settings JSON file; sessions run in Claude's sandbox with it. A run needs this or --skip-permissions")
	skipPermsFlag   = pflag.Bool("skip-permissions", false, "Run sessions with no sandbox and no permission checks (claude --dangerously-skip-permissions); what they do is on you")
	commitFlag      = pflag.Bool("commit", false, "Commit each completed task with git after its gates pass, with the task name as the message; in a repository, the work tree must be clean")
	logDirFlag      = pflag.String("log-dir", "", "Directory for a record of the run: a ledger and each task's activity, in a new folder per run; full-screen view only")
)

func main() {
	pflag.Parse()
	checkVersion()
	checkInstallSkill()
	validateDir()
	validateGateTimeout()
	stdinFd := os.Stdin.Fd()
	stdinTTY := term.IsTerminal(stdinFd)
	stdoutFd := os.Stdout.Fd()
	stdoutTTY := term.IsTerminal(stdoutFd)
	plain := isPlain(*dryRunFlag, *noTUIFlag, stdinTTY, stdoutTTY)
	if plain {
		validateRequiredFlags()
	}

	validateLogDir(plain)
	session := validateSessionFlags(plain)

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
		exitCode := runTUI(ctx)
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

// runTUI loads what the flags name, opens the setup wizard when the files,
// the permission choice, or the gate list are still open, runs the loop in
// the full-screen view, prints its summary, and returns the exit code. With
// --log-dir it opens the run's record and has the view's events written to
// it.
func runTUI(ctx context.Context) int {
	s, err := settingsFromFlags()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	s, err = askWizard(s)
	if errors.Is(err, tui.ErrCancelled) {
		_, _ = fmt.Fprintln(os.Stderr, "error: setup cancelled")
		return 1
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\nrun with --no-tui to use plain output\n", err)
		return 1
	}

	applySettings(s)
	session, err := recheck()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if err := saveGates(s); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	return runView(ctx, s, session)
}

// settingsFromFlags is the run as the flags and the files they name give it,
// before the wizard fills any gap.
func settingsFromFlags() (tui.Settings, error) {
	prompt, tasklist, err := loadGiven(*tasksFlag, *promptFlag)
	if err != nil {
		return tui.Settings{}, err
	}

	return tui.Settings{
		Dir:             *dirFlag,
		TasksPath:       *tasksFlag,
		PromptPath:      *promptFlag,
		Prompt:          prompt,
		Tasks:           tasklist,
		SandboxSettings: *sandboxFlag,
		SkipPermissions: *skipPermsFlag,
		Commit:          *commitFlag,
		LogDir:          *logDirFlag,
		GateTimeout:     *gateTimeoutFlag,
	}, nil
}

// askWizard opens the wizard only for a gap a run cannot start without; the
// optional steps ride along and never open it alone. Given is read from the
// command line because a flag set to its zero value still answers its step.
func askWizard(s tui.Settings) (tui.Settings, error) {
	if !tui.NeedsWizard(s) {
		return s, nil
	}

	changed := pflag.CommandLine.Changed
	given := tui.Given{Commit: changed("commit"), LogDir: changed("log-dir"), GateTimeout: changed("gate-timeout")}
	return tui.Wizard(s, given)
}

// applySettings puts the answers where the flags live, so the rest of the run
// reads one set of values however each was given.
func applySettings(s tui.Settings) {
	*tasksFlag, *promptFlag = s.TasksPath, s.PromptPath
	*sandboxFlag, *skipPermsFlag = s.SandboxSettings, s.SkipPermissions
	*commitFlag, *logDirFlag, *gateTimeoutFlag = s.Commit, s.LogDir, s.GateTimeout
}

// recheck runs main's startup checks again, in main's order, on the values
// the run will use. The wizard checks its answers as they are given, but its
// answers are never trusted in place of the checks a flag gets.
func recheck() ([]string, error) {
	var err error
	if *gateTimeoutFlag != "" {
		err = checkGateTimeout(*gateTimeoutFlag)
	}

	if err != nil {
		return nil, err
	}

	if err := checkLogDir(*logDirFlag, false, false); err != nil {
		return nil, err
	}

	return sessionArgs(*sandboxFlag, *skipPermsFlag, false, false)
}

// saveGates writes the task file only when the wizard changed its gates, and
// only once Start was chosen, so a cancel anywhere leaves the file as it was.
func saveGates(s tui.Settings) error {
	if !s.GatesEdited {
		return nil
	}

	return tasks.SaveTasks(s.TasksPath, *s.Tasks)
}

// runView runs the loop in the full-screen view with the settings checked.
func runView(ctx context.Context, s tui.Settings, session []string) int {
	repo, err := openRepo(s.TasksPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	info := runInfo(s.TasksPath, s.PromptPath)
	runLog, err := openLog(*logDirFlag, repo, info)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	defer closeLog(runLog)

	code, summary, err := tui.Run(ctx, s.Prompt, s.Tasks, s.TasksPath, *gateTimeoutFlag, session, repo, runLog.Record)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\nrun with --no-tui to use plain output\n", err)
		return 1
	}

	fmt.Println(summary)
	return code
}

// loadGiven loads the prompt and tasks whose paths were passed by flag; an
// empty path is left for the wizard. The prompt loads first so a bad
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

// runInfo is what the run's record says about how it was started.
func runInfo(tasksPath, promptPath string) runlog.Info {
	permissions := "skip"
	if *sandboxFlag != "" {
		permissions = "sandbox"
	}

	ver := version.Version()
	return runlog.Info{
		Version:         ver,
		TasksFile:       tasksPath,
		PromptFile:      promptPath,
		Permissions:     permissions,
		SandboxSettings: *sandboxFlag,
		GateTimeout:     *gateTimeoutFlag,
		Commit:          *commitFlag,
	}
}

// openLog starts the run's record under logDir (ADR-016). With no logDir it
// returns nil, and gralph writes nothing but the task file. The run folder
// is named before it is created so that repo, when there is one, can refuse
// a folder git would commit without anything being left behind.
func openLog(logDir string, repo *looper.Repo, info runlog.Info) (*runlog.Log, error) {
	if logDir == "" {
		return nil, nil
	}

	start := time.Now()
	runDir := runlog.RunDir(logDir, start)
	if err := repo.CheckLogDir(runDir); err != nil {
		return nil, err
	}

	runLog, err := runlog.Open(runDir, info)
	if err != nil {
		return nil, fmt.Errorf("--log-dir: %w", err)
	}

	return runLog, nil
}

// closeLog drops Close's error on purpose: every line was already written
// when Record returned, so a failed close can lose nothing.
func closeLog(runLog *runlog.Log) {
	_ = runLog.Close()
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

// validateDir resolves --dir into --tasks and --prompt before any other
// check, so every later check sees the paths the run will use.
func validateDir() {
	tasksPath, promptPath, err := resolveDir(*dirFlag, *tasksFlag, *promptFlag, *dryRunFlag)
	if err != nil {
		fatal(err)
	}

	*tasksFlag, *promptFlag = tasksPath, promptPath
}

// resolveDir turns a run folder into the two file paths. The names are fixed
// so a folder written by the skill needs no other flag; a path passed by flag
// always wins (ADR-018). A dry run never reads the prompt, so it does not
// require one.
func resolveDir(dir, tasksPath, promptPath string, dryRun bool) (string, string, error) {
	if dir == "" {
		return tasksPath, promptPath, nil
	}

	if tasksPath == "" {
		tasksPath = filepath.Join(dir, "tasks.yaml")
		if err := requireFile(tasksPath, dir, "tasks.yaml"); err != nil {
			return "", "", err
		}
	}

	if promptPath == "" && !dryRun {
		promptPath = filepath.Join(dir, "prompt.md")
		if err := requireFile(promptPath, dir, "prompt.md"); err != nil {
			return "", "", err
		}
	}

	return tasksPath, promptPath, nil
}

func requireFile(path, dir, name string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("--dir: no %s in %s", name, dir)
	}

	return nil
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

// validateLogDir exits when --log-dir was passed to a run that cannot keep a
// record; see checkLogDir.
func validateLogDir(plain bool) {
	if err := checkLogDir(*logDirFlag, plain, *dryRunFlag); err != nil {
		fatal(err)
	}
}

// checkLogDir refuses --log-dir in plain mode. The record is built from
// events only the full-screen view's path produces, and a run that was asked
// for a record must not quietly leave none (ADR-016). An empty value counts
// as not passed. A dry run records nothing and ignores the flag, as it
// ignores --prompt.
func checkLogDir(logDir string, plain, dryRun bool) error {
	if logDir == "" || dryRun || !plain {
		return nil
	}

	return errors.New("--log-dir only works with the full-screen view")
}

// validateSessionFlags exits unless the run was told how far to trust its
// sessions, or the full-screen view will ask; see sessionArgs.
func validateSessionFlags(plain bool) []string {
	args, err := sessionArgs(*sandboxFlag, *skipPermsFlag, *dryRunFlag, !plain)
	if err != nil {
		fatal(err)
	}

	return args
}

// sessionArgs returns the claude flags that set what a session may do. A run
// must choose a sandbox or ask by name to go without one (ADR-014): there is
// no default, so nobody runs unsandboxed by accident. A dry run starts no
// session and needs neither, but its sandbox file is still checked. In the
// full-screen view the wizard asks instead, and this runs again, strict, on
// its answer.
func sessionArgs(sandboxFile string, skip, dryRun, wizard bool) ([]string, error) {
	if sandboxFile != "" && skip {
		return nil, errors.New("--sandbox-settings and --skip-permissions cannot be used together")
	}

	if sandboxFile != "" {
		return looper.SandboxArgs(sandboxFile)
	}

	if skip {
		return looper.BypassArgs(), nil
	}

	if dryRun || wizard {
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
