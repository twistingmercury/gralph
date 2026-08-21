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
// It returns separate stdout and stderr captures.
type promptRunner func(ctx context.Context, prompt string) (agent.CommandOutput, error)

var cycleLabelPattern = regexp.MustCompile(`^(Cycle\s+\d+)\s*-\s*(.+)$`)

func withHiddenCursor(output io.Writer, runner promptRunner) promptRunner {
	return func(ctx context.Context, prompt string) (agent.CommandOutput, error) {
		hideCursor(output)
		defer showCursor(output)
		return runner(ctx, prompt)
	}
}

func hideCursor(output io.Writer) {
	file, ok := output.(*os.File)
	if !ok || !isTerminal(file) {
		return
	}
	_, _ = io.WriteString(output, "\x1b[?25l")
}

func showCursor(output io.Writer) {
	file, ok := output.(*os.File)
	if !ok || !isTerminal(file) {
		return
	}
	_, _ = io.WriteString(output, "\x1b[?25h")
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

	if err := runLoop(ctx, config.PromptPath, config.PRDPath, progressPath, config.MaxAttempts, config.OutputWriter, withHiddenCursor(config.OutputWriter, runner.Run)); err != nil {
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

func runLoop(ctx context.Context, prompt, prd, progress string, maxAttempts int, output io.Writer, runner promptRunner) error {
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
		if err := writeLoopOutput(output, "%s\n- Agent: %s\n- Status: Running ... (%d/%d)\n", cycleHeader(label), meta.agent, attempt, maxAttempts); err != nil {
			return err
		}

		capture, invokeErr := invokeAgent(ctx, prompt, prd, progress, runner)
		if invokeErr != nil && !errors.Is(invokeErr, agent.ErrNonZeroExit) {
			return cleanupAfterInvocation(capture, fmt.Errorf("agent invocation failed: %w", invokeErr))
		}
		if err := ctx.Err(); err != nil {
			return cleanupAfterInvocation(capture, err)
		}

		itemAfter, err := getFirstOpenItem(prd)
		if err != nil {
			return cleanupAfterInvocation(capture, fmt.Errorf("could not read PRD after invocation: %w", err))
		}
		if err := ctx.Err(); err != nil {
			return cleanupAfterInvocation(capture, err)
		}

		// Completed and final abandoned attempts print captured output. Intermediate
		// retries, fatal failures, and cancellations discard it. Because stdout and
		// stderr are captured independently, their original interleaving cannot be
		// reconstructed; printed output is replayed deterministically stdout first.
		if itemAfter != currentItem {
			if err := writeLoopOutput(output, "- Status: Complete\n- Agent Output:\n"); err != nil {
				return cleanupAfterInvocation(capture, err)
			}
			if err := printAgentOutput(output, capture); err != nil {
				return cleanupAfterInvocation(capture, fmt.Errorf("could not print agent output: %w", err))
			}
			if err := cleanupAfterInvocation(capture, nil); err != nil {
				return err
			}
			currentItem = ""
			attempt = 0
			continue
		}

		if attempt >= maxAttempts {
			if err := writeLoopOutput(output, "- Status: Failed\n"); err != nil {
				return cleanupAfterInvocation(capture, err)
			}
			if invokeErr != nil {
				if err := writeLoopOutput(output, "- Error: %v\n", invokeErr); err != nil {
					return cleanupAfterInvocation(capture, err)
				}
			}
			if err := printAgentOutput(output, capture); err != nil {
				return cleanupAfterInvocation(capture, fmt.Errorf("could not print agent output: %w", err))
			}
			if err := cleanupAfterInvocation(capture, nil); err != nil {
				return err
			}
			if _, err := abandonFirstOpenItem(prd); err != nil {
				return fmt.Errorf("could not abandon item: %w", err)
			}
			currentItem = ""
			attempt = 0
			continue
		}

		if err := cleanupAfterInvocation(capture, nil); err != nil {
			return err
		}
	}
}

func writeLoopOutput(output io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(output, format, args...); err != nil {
		return fmt.Errorf("could not write loop output: %w", err)
	}
	return nil
}

func printAgentOutput(output io.Writer, capture agent.CommandOutput) error {
	if err := copyOutputFile(output, capture.StdoutPath); err != nil {
		return err
	}
	return copyOutputFile(output, capture.StderrPath)
}

func copyOutputFile(output io.Writer, path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path) // #nosec G304 -- path is system-generated by os.CreateTemp
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = io.Copy(output, f)
	return err
}

func cleanupAfterInvocation(capture agent.CommandOutput, primaryErr error) error {
	if err := capture.Cleanup(); err != nil {
		cleanupErr := fmt.Errorf("could not clean up agent output: %w", err)
		return errors.Join(primaryErr, cleanupErr)
	}
	return primaryErr
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

func invokeAgent(ctx context.Context, prompt, prd, progress string, runner promptRunner) (agent.CommandOutput, error) {
	data, err := os.ReadFile(prompt) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return agent.CommandOutput{}, fmt.Errorf("could not read prompt file %q: %w", prompt, err)
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
