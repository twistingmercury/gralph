package looper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// commandRunner is a function that executes claude with the given stdin text.
type commandRunner func(ctx context.Context, stdin string) error

// ErrCycleFailed indicates that the first open PRD item was unchanged after a
// single claude invocation. The cycle fails fast: the PRD is left untouched
// and no further attempts are made for that item.
var ErrCycleFailed = errors.New("cycle failed")

func defaultClaudeRunner(ctx context.Context, stdin string) error {
	hideCursor()
	defer showCursor()

	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions") // #nosec G204 -- command name is a literal, not user-controlled
	configureProcessTree(cmd)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

func Start(ctx context.Context, prompt, prd string) error {
	if _, err := os.Stat(prompt); err != nil {
		return fmt.Errorf("prompt file %q is not accessible: %w", prompt, err)
	}
	if _, err := os.Stat(prd); err != nil {
		return fmt.Errorf("PRD file %q is not accessible: %w", prd, err)
	}

	if err := runLoop(ctx, prompt, prd, defaultClaudeRunner); err != nil {
		return fmt.Errorf("loop error: %w", err)
	}

	return nil
}

func runLoop(ctx context.Context, prompt, prd string, runner commandRunner) error {
	fmt.Printf("[gralph] start\n")

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

		label := itemLabel(item)
		fmt.Printf("[gralph] attempt item=%q\n", label)
		fmt.Printf("[gralph] claude_output_begin item=%q\n", label)

		invokeErr := invokeClaude(ctx, prompt, prd, runner)
		if invokeErr != nil {
			fmt.Printf("[gralph] claude_output_end item=%q status=error\n", label)
			fmt.Printf("[gralph] invoke_failed item=%q err=%v\n", label, invokeErr)
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

		if itemAfter != item {
			fmt.Printf("[gralph] completed item=%q\n", label)
			continue
		}

		// Fail fast: the first open item is unchanged after exactly one
		// claude invocation, regardless of exit code. No retry, no PRD
		// rewrite; the PRD is left untouched so the run can be resumed.
		fmt.Printf("[gralph] failed item=%q\n", label)
		if invokeErr != nil {
			return fmt.Errorf("%w: %s: %w", ErrCycleFailed, label, invokeErr)
		}
		return fmt.Errorf("%w: %s", ErrCycleFailed, label)
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

func invokeClaude(ctx context.Context, prompt, prd string, runner commandRunner) error {
	data, err := os.ReadFile(prompt) // #nosec G304 -- path is CLI-provided or derived from validated PRD path
	if err != nil {
		return fmt.Errorf("could not read prompt file %q: %w", prompt, err)
	}
	combined := string(data) + fmt.Sprintf("\n## Runtime paths\n- PRD: %s\n", prd)
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
