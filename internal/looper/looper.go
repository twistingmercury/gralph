package looper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// commandRunner is a function that executes claude with the given stdin text.
type commandRunner func(ctx context.Context, stdin string) error

func defaultClaudeRunner(ctx context.Context, stdin string) error {
	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions") // #nosec G204 -- command name is a literal, not user-controlled
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func Start(ctx context.Context, prompt, prd, progressFile string, maxAttempts int) error {
	if _, err := os.Stat(prompt); err != nil {
		return fmt.Errorf("prompt file %q is not accessible: %w", prompt, err)
	}
	if _, err := os.Stat(prd); err != nil {
		return fmt.Errorf("PRD file %q is not accessible: %w", prd, err)
	}

	if progressFile == "" {
		progressFile = filepath.Join(filepath.Dir(prd), "progress.txt")
	}

	if err := ensureProgressFileExists(progressFile); err != nil {
		return err
	}

	if err := runLoop(ctx, prompt, prd, progressFile, maxAttempts, defaultClaudeRunner); err != nil {
		return fmt.Errorf("loop error: %w", err)
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

func runLoop(ctx context.Context, prompt, prd, progress string, maxAttempts int, runner commandRunner) error {
	fmt.Printf("[gralph] start max_attempts=%d\n", maxAttempts)

	currentItem := ""
	attempt := 0

	for {
		item, err := getFirstOpenItem(prd)
		if err != nil {
			return fmt.Errorf("could not read PRD: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if item == "" {
			fmt.Printf("[gralph] done\n")
			return nil
		}

		if item != currentItem {
			currentItem = item
			attempt = 1
		} else {
			attempt++
		}

		label := itemLabel(currentItem)
		fmt.Printf("[gralph] attempt %d/%d item=%q\n", attempt, maxAttempts, label)
		fmt.Printf("[gralph] claude_output_begin item=%q\n", label)

		if err := invokeClaude(ctx, prompt, prd, progress, runner); err != nil {
			fmt.Printf("[gralph] claude_output_end item=%q status=error\n", label)
			fmt.Printf("[gralph] invoke_failed item=%q err=%v\n", label, err)
		} else {
			fmt.Printf("[gralph] claude_output_end item=%q status=ok\n", label)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		fmt.Printf("[gralph] check item=%q\n", label)

		itemAfter, err := getFirstOpenItem(prd)
		if err != nil {
			return fmt.Errorf("could not read PRD after invocation: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if itemAfter != currentItem {
			fmt.Printf("[gralph] completed item=%q\n", label)
			currentItem = ""
			attempt = 0
			continue
		}

		if attempt >= maxAttempts {
			fmt.Printf("[gralph] abandoned item=%q\n", label)
			if _, err := abandonFirstOpenItem(prd); err != nil {
				return fmt.Errorf("could not abandon item: %w", err)
			}
			currentItem = ""
			attempt = 0
			continue
		}

		fmt.Printf("[gralph] retry item=%q next_attempt=%d/%d\n", label, attempt+1, maxAttempts)
	}
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

func invokeClaude(ctx context.Context, prompt, prd, progress string, runner commandRunner) error {
	data, err := os.ReadFile(prompt) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return fmt.Errorf("could not read prompt file %q: %w", prompt, err)
	}
	combined := string(data) + fmt.Sprintf("\n## Runtime paths\n- PRD: %s\n- Progress: %s\n", prd, progress)
	return runner(ctx, combined)
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
