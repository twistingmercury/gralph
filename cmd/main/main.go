package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/twistingmercury/gralph/internal/agent"
	"github.com/twistingmercury/gralph/internal/looper"
	"github.com/twistingmercury/gralph/internal/version"

	"github.com/spf13/pflag"
)

var (
	versionFlag      = pflag.Bool("version", false, "Show the current version of gralph")
	prdFlag          = pflag.String("prd", "", "Required. Path to the PRD.md checklist file that drives the loop")
	promptFlag       = pflag.String("prompt", "", "Required. Path to the PROMPT.md template passed to the agent each iteration")
	progressFlag     = pflag.String("progress", "", "Optional. Path to the progress log file (default: <prd dir>/progress.txt)")
	iterationsFlag   = pflag.IntP("iterations", "i", 10, "Maximum attempts per checklist item before it is abandoned")
	agentExecFlag    = pflag.String("agent-exec", "", "Required unless --agent-profile is set. Agent executable path or name")
	agentArgsFlag    = pflag.StringArray("agent-arg", nil, "Agent argument; may be repeated to preserve argument boundaries and order")
	promptModeFlag   = pflag.String("prompt-mode", string(agent.PromptModeStdin), "Prompt transport mode: stdin or arg")
	agentProfileFlag = pflag.String("agent-profile", "", "Optional verified agent profile; explicit agent flags override its command values")
)

func main() {
	pflag.Parse()
	checkVersion()
	command, err := configuredAgentCommand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid agent profile: %v\n\n", err)
		pflag.Usage()
		os.Exit(1)
	}
	validateRequiredFlags(command.Executable)

	config := looper.Config{
		PromptPath:   *promptFlag,
		PRDPath:      *prdFlag,
		ProgressPath: *progressFlag,
		MaxAttempts:  *iterationsFlag,
		OutputWriter: os.Stdout,
		AgentCommand: command,
	}
	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid configuration: %v\n\n", err)
		pflag.Usage()
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := looper.Start(ctx, config); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
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

func configuredAgentCommand() (agent.AgentCommand, error) {
	command := agent.AgentCommand{
		Executable: *agentExecFlag,
		Args:       append([]string(nil), (*agentArgsFlag)...),
		PromptMode: agent.PromptMode(*promptModeFlag),
	}
	if *agentProfileFlag == "" {
		return command, nil
	}

	command, err := agent.Profile(*agentProfileFlag)
	if err != nil {
		return agent.AgentCommand{}, err
	}
	if pflag.CommandLine.Changed("agent-exec") {
		command.Executable = *agentExecFlag
	}
	if pflag.CommandLine.Changed("agent-arg") {
		command.Args = append([]string(nil), (*agentArgsFlag)...)
	}
	if pflag.CommandLine.Changed("prompt-mode") {
		command.PromptMode = agent.PromptMode(*promptModeFlag)
	}
	return command, nil
}

func validateRequiredFlags(agentExecutable string) {
	var missing []string
	if *promptFlag == "" {
		missing = append(missing, "--prompt")
	}
	if *prdFlag == "" {
		missing = append(missing, "--prd")
	}
	if agentExecutable == "" {
		missing = append(missing, "--agent-exec")
	}
	if len(missing) == 0 {
		if *iterationsFlag < 1 {
			fmt.Fprintf(os.Stderr, "error: --iterations must be at least 1\n")
			fmt.Fprintln(os.Stderr)
			pflag.Usage()
			os.Exit(1)
		}
		return
	}
	for _, f := range missing {
		fmt.Fprintf(os.Stderr, "error: required flag %s not set\n", f)
	}
	fmt.Fprintln(os.Stderr)
	pflag.Usage()
	os.Exit(1)
}
