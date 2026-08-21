package looper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/twistingmercury/gralph/internal/agent"
)

// promptRunner is a function that executes an agent with the given prompt.
// It returns the path to a file containing combined stdout/stderr.
type promptRunner func(ctx context.Context, prompt string) (string, error)

var cycleLabelPattern = regexp.MustCompile(`^(Cycle\s+\d+)\s*-\s*(.+)$`)

func withHiddenCursor(runner promptRunner) promptRunner {
	return func(ctx context.Context, prompt string) (string, error) {
		hideCursor()
		defer showCursor()
		return runner(ctx, prompt)
	}
}

func hideCursor() {
	if !isTerminal(os.Stdout) {
		return
	}
	_, _ = os.Stdout.WriteString("\x1b[?25l")
}

func showCursor() {
	if !isTerminal(os.Stdout) {
		return
	}
	_, _ = os.Stdout.WriteString("\x1b[?25h")
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func Start(ctx context.Context, config Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid loop configuration: %w", err)
	}

	if err := validateInputFile("prompt", config.PromptPath); err != nil {
		return err
	}
	if err := validateInputFile("PRD", config.PRDPath); err != nil {
		return err
	}

	progressPath := config.ProgressPath
	if progressPath == "" {
		progressPath = filepath.Join(filepath.Dir(config.PRDPath), "progress.txt")
	}

	if err := ensureProgressFileExists(progressPath); err != nil {
		return err
	}

	runner, err := agent.NewCommandRunner(config.AgentCommand)
	if err != nil {
		return fmt.Errorf("could not configure agent runner: %w", err)
	}

	if err := runLoop(ctx, config.PromptPath, config.PRDPath, progressPath, config.MaxAttempts, withHiddenCursor(runner.Run)); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

// validateInputFile rejects symlinks so a later atomic PRD replacement cannot
// replace a link itself while leaving its target unchanged. Prompt and PRD
// inputs follow the same policy for a consistent CLI contract.
func validateInputFile(label, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s file %q is not accessible: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s file %q must not be a symbolic link", label, path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s file %q must be a regular file", label, path)
	}
	if info.Mode().Perm()&0o444 == 0 {
		return fmt.Errorf("%s file %q has no read permissions", label, path)
	}

	f, err := os.Open(path) // #nosec G304 -- path is supplied through validated loop configuration
	if err != nil {
		return fmt.Errorf("%s file %q is not readable: %w", label, path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not close %s file %q after validation: %w", label, path, err)
	}

	return nil
}

func ensureProgressFileExists(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return fmt.Errorf("could not open progress file %q: %w", path, err)
	}
	return f.Close()
}

func runLoop(ctx context.Context, prompt, prd, progress string, maxAttempts int, runner promptRunner) error {
	currentItem := ""
	attempt := 0

	for {
		meta, err := getFirstOpenItemMeta(prd)
		if err != nil {
			return fmt.Errorf("could not read PRD: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		item := meta.item
		if item == "" {
			return nil
		}

		if item != currentItem {
			currentItem = item
			attempt = 1
		} else {
			attempt++
		}

		label := itemLabel(currentItem)
		fmt.Printf("%s\n", cycleHeader(label))
		fmt.Printf("- Agent: %s\n", meta.agent)
		fmt.Printf("- Status: Running ... (%d/%d)\n", attempt, maxAttempts)

		agentOutputPath, invokeErr := invokeAgent(ctx, prompt, prd, progress, runner)
		if invokeErr != nil && !errors.Is(invokeErr, agent.ErrNonZeroExit) {
			_ = cleanupOutputFile(agentOutputPath)
			return fmt.Errorf("agent invocation failed: %w", invokeErr)
		}
		if err := ctx.Err(); err != nil {
			_ = cleanupOutputFile(agentOutputPath)
			return err
		}

		itemAfter, err := getFirstOpenItem(prd)
		if err != nil {
			_ = cleanupOutputFile(agentOutputPath)
			return fmt.Errorf("could not read PRD after invocation: %w", err)
		}
		if err := ctx.Err(); err != nil {
			_ = cleanupOutputFile(agentOutputPath)
			return err
		}

		if itemAfter != currentItem {
			fmt.Printf("- Status: Complete\n")
			fmt.Printf("- Agent Output:\n")
			if err := printAgentOutput(agentOutputPath); err != nil {
				_ = cleanupOutputFile(agentOutputPath)
				return fmt.Errorf("could not print agent output: %w", err)
			}
			_ = cleanupOutputFile(agentOutputPath)
			currentItem = ""
			attempt = 0
			continue
		}

		if attempt >= maxAttempts {
			fmt.Printf("- Status: Failed\n")
			if invokeErr != nil {
				fmt.Printf("- Error: %v\n", invokeErr)
			}
			if err := printAgentOutput(agentOutputPath); err != nil {
				_ = cleanupOutputFile(agentOutputPath)
				return fmt.Errorf("could not print agent output: %w", err)
			}
			_ = cleanupOutputFile(agentOutputPath)
			if _, err := abandonFirstOpenItem(prd); err != nil {
				return fmt.Errorf("could not abandon item: %w", err)
			}
			currentItem = ""
			attempt = 0
			continue
		}

		_ = cleanupOutputFile(agentOutputPath)
	}
}

func printAgentOutput(outputPath string) error {
	if outputPath == "" {
		return nil
	}
	f, err := os.Open(outputPath) // #nosec G304 -- outputPath is system-generated by os.CreateTemp
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = io.Copy(os.Stdout, f)
	return err
}

func cleanupOutputFile(outputPath string) error {
	if outputPath == "" {
		return nil
	}
	return os.Remove(outputPath) // #nosec G703 -- outputPath is system-generated by os.CreateTemp
}

func cycleHeader(label string) string {
	matches := cycleLabelPattern.FindStringSubmatch(label)
	if len(matches) == 3 {
		return fmt.Sprintf("%s: %s", strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]))
	}
	return label
}

func itemLabel(item string) string {
	label := strings.TrimSpace(item)
	for _, prefix := range []string{"- [ ]", "- [x]", "- [~]"} {
		if strings.HasPrefix(label, prefix) {
			label = strings.TrimSpace(strings.TrimPrefix(label, prefix))
			break
		}
	}

	if idx := strings.Index(label, ":"); idx >= 0 {
		label = label[:idx]
	}

	label = strings.TrimSpace(label)
	label = strings.Trim(label, "*` ")
	label = strings.ReplaceAll(label, "**", "")
	label = strings.ReplaceAll(label, "`", "")
	label = strings.Join(strings.Fields(label), " ")

	if label == "" {
		return "unnamed item"
	}

	return label
}

func invokeAgent(ctx context.Context, prompt, prd, progress string, runner promptRunner) (string, error) {
	data, err := os.ReadFile(prompt) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return "", fmt.Errorf("could not read prompt file %q: %w", prompt, err)
	}
	combined := string(data) + fmt.Sprintf("\n## Runtime paths\n- PRD: %s\n- Progress: %s\n", prd, progress)
	output, runErr := runner(ctx, combined)
	if runErr != nil {
		return output, runErr
	}
	return output, nil
}

func getFirstOpenItem(prdPath string) (string, error) {
	data, err := os.ReadFile(prdPath) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return "", fmt.Errorf("could not read PRD file %q: %w", prdPath, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "- [ ]") {
			return line, nil
		}
	}
	return "", nil
}

type openItemMeta struct {
	item  string
	agent string
}

func getFirstOpenItemMeta(prdPath string) (openItemMeta, error) {
	data, err := os.ReadFile(prdPath) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return openItemMeta{}, fmt.Errorf("could not read PRD file %q: %w", prdPath, err)
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "- [ ]") {
			continue
		}

		agent := "unknown"
		for j := i + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(trimmed, "- [ ]") || strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [~]") {
				break
			}
			if strings.HasPrefix(trimmed, "- Agent:") {
				agent = strings.TrimSpace(strings.TrimPrefix(trimmed, "- Agent:"))
				agent = strings.Trim(agent, "`* ")
				if agent == "" {
					agent = "unknown"
				}
				break
			}
		}

		return openItemMeta{item: line, agent: agent}, nil
	}

	return openItemMeta{agent: "unknown"}, nil
}

func abandonFirstOpenItem(prdPath string) (string, error) {
	data, err := os.ReadFile(prdPath) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return "", fmt.Errorf("could not read PRD file %q: %w", prdPath, err)
	}

	lines := strings.Split(string(data), "\n")
	abandoned := ""
	for i, line := range lines {
		if strings.HasPrefix(line, "- [ ]") {
			abandoned = line
			lines[i] = "- [~]" + line[len("- [ ]"):]
			break
		}
	}
	if abandoned == "" {
		return "", nil
	}

	updated := strings.Join(lines, "\n")
	dir := filepath.Dir(prdPath)
	tmp, err := os.CreateTemp(dir, "prd-*.tmp") // #nosec G306 -- temp file replaced by rename; PRD files are not secret
	if err != nil {
		return "", fmt.Errorf("could not create temp file for PRD update: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(updated); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // #nosec G703 -- tmpName is system-generated by os.CreateTemp
		return "", fmt.Errorf("could not write temp PRD file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName) // #nosec G703 -- tmpName is system-generated by os.CreateTemp
		return "", fmt.Errorf("could not close temp PRD file: %w", err)
	}
	if err := os.Rename(tmpName, prdPath); err != nil { // #nosec G703 -- tmpName is system-generated by os.CreateTemp
		_ = os.Remove(tmpName) // #nosec G703 -- tmpName is system-generated by os.CreateTemp
		return "", fmt.Errorf("could not replace PRD file: %w", err)
	}

	return abandoned, nil
}
