package looper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twistingmercury/gralph/internal/agent"
)

func testConfig(promptPath, prdPath, progressPath string, maxAttempts int) Config {
	return Config{
		PromptPath:   promptPath,
		PRDPath:      prdPath,
		ProgressPath: progressPath,
		MaxAttempts:  maxAttempts,
		AgentCommand: agent.AgentCommand{
			Executable: os.Args[0],
			PromptMode: agent.PromptModeStdin,
		},
	}
}

func TestStart_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	prd := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prd, []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Start(context.Background(), testConfig(filepath.Join(dir, "missing.md"), prd, "", 1))
	if err == nil {
		t.Fatal("expected error for missing prompt file, got nil")
	}
}

func TestStart_MissingPRD(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(prompt, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Start(context.Background(), testConfig(prompt, filepath.Join(dir, "missing.md"), "", 1))
	if err == nil {
		t.Fatal("expected error for missing PRD file, got nil")
	}
}

func TestStartInputValidation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		kind  string
	}{
		{name: "missing prompt", input: "prompt", kind: "missing"},
		{name: "prompt directory", input: "prompt", kind: "directory"},
		{name: "unreadable prompt", input: "prompt", kind: "unreadable"},
		{name: "prompt symlink", input: "prompt", kind: "symlink"},
		{name: "missing PRD", input: "prd", kind: "missing"},
		{name: "PRD directory", input: "prd", kind: "directory"},
		{name: "unreadable PRD", input: "prd", kind: "unreadable"},
		{name: "PRD symlink", input: "prd", kind: "symlink"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			promptPath := filepath.Join(dir, "prompt.md")
			prdPath := filepath.Join(dir, "prd.md")
			progressPath := filepath.Join(dir, "progress.txt")
			invocationMarker := filepath.Join(dir, "agent-invoked")
			originalPrompt := []byte("# Prompt\n")
			originalPRD := []byte("# PRD\n\n- [ ] Task must remain open\n")
			if err := os.WriteFile(promptPath, originalPrompt, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(prdPath, originalPRD, 0o600); err != nil {
				t.Fatal(err)
			}

			invalidPath := promptPath
			invalidContent := originalPrompt
			prdContentPath := prdPath
			if tt.input == "prd" {
				invalidPath = prdPath
				invalidContent = originalPRD
			}
			switch tt.kind {
			case "missing":
				if err := os.Remove(invalidPath); err != nil {
					t.Fatal(err)
				}
				if tt.input == "prd" {
					prdContentPath = ""
				}
			case "directory":
				if err := os.Remove(invalidPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(invalidPath, 0o700); err != nil {
					t.Fatal(err)
				}
				if tt.input == "prd" {
					prdContentPath = ""
				}
			case "unreadable":
				if err := os.Chmod(invalidPath, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(invalidPath, 0o600) })
			case "symlink":
				targetPath := invalidPath + ".target"
				if err := os.WriteFile(targetPath, invalidContent, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(invalidPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(targetPath, invalidPath); err != nil {
					t.Skipf("symlinks are not available: %v", err)
				}
				if tt.input == "prd" {
					prdContentPath = targetPath
				}
			default:
				t.Fatalf("unknown fixture kind %q", tt.kind)
			}

			config := testConfig(promptPath, prdPath, progressPath, 3)
			config.AgentCommand.Args = []string{
				"-test.run=^TestInputValidationAgentHelper$",
				"--",
				"--input-validation-agent-helper",
				invocationMarker,
			}
			err := Start(context.Background(), config)
			if err == nil {
				t.Fatal("expected input validation failure, got nil")
			}
			if errors.Is(err, agent.ErrProcessStart) {
				t.Fatalf("input validation reached agent startup: %v", err)
			}
			if _, statErr := os.Stat(progressPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("progress file was created before input validation completed: %v", statErr)
			}
			if _, statErr := os.Stat(invocationMarker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("agent was invoked before input validation completed: %v", statErr)
			}

			if prdContentPath != "" {
				if err := os.Chmod(prdContentPath, 0o600); err != nil {
					t.Fatal(err)
				}
				after, readErr := os.ReadFile(prdContentPath) // #nosec G304 -- path is created by the test
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !bytes.Equal(after, originalPRD) {
					t.Fatalf("input validation changed PRD:\nwant: %q\n got: %q", originalPRD, after)
				}
			}
		})
	}
}

func TestInputValidationAgentHelper(t *testing.T) {
	const marker = "--input-validation-agent-helper"

	for i, arg := range os.Args {
		if arg != marker {
			continue
		}
		if i+1 >= len(os.Args) {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Args[i+1], nil, 0o600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
}

func TestStart_DerivedProgressPath(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	prd := filepath.Join(dir, "PRD.md")
	if err := os.WriteFile(prompt, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prd, []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// PRD has no open items, so runLoop returns nil immediately.
	// What matters is that file validation passed and progress.txt was
	// created/appended in the PRD directory.
	if err := Start(context.Background(), testConfig(prompt, prd, "", 1)); err != nil {
		t.Fatalf("unexpected error from Start: %v", err)
	}

	derived := filepath.Join(dir, "progress.txt")
	if _, err := os.Stat(derived); err != nil {
		t.Fatalf("expected progress file at %s to exist, got: %v", derived, err)
	}
}

func TestStart_ExplicitProgressPath(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	prd := filepath.Join(dir, "PRD.md")
	explicit := filepath.Join(dir, "custom_progress.txt")
	if err := os.WriteFile(prompt, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prd, []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Start(context.Background(), testConfig(prompt, prd, explicit, 1)); err != nil {
		t.Fatalf("unexpected error from Start: %v", err)
	}

	if _, err := os.Stat(explicit); err != nil {
		t.Fatalf("expected explicit progress file at %s to exist, got: %v", explicit, err)
	}
}

func TestStart_UsesConfiguredAgent(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	prd := filepath.Join(dir, "PRD.md")
	invocationMarker := filepath.Join(dir, "agent-invoked")
	if err := os.WriteFile(prompt, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prd, []byte("# PRD\n\n- [ ] Task one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	config := testConfig(prompt, prd, "", 1)
	config.AgentCommand.Args = []string{
		"-test.run=^TestConfiguredAgentHelper$",
		"--",
		"--configured-agent-helper",
		prd,
		invocationMarker,
	}
	if err := Start(context.Background(), config); err != nil {
		t.Fatalf("unexpected error from Start: %v", err)
	}

	if _, err := os.Stat(invocationMarker); err != nil {
		t.Fatalf("expected configured agent to be invoked: %v", err)
	}
	data, err := os.ReadFile(prd) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- [x] Task one") {
		t.Fatalf("expected configured agent to complete the task, got:\n%s", data)
	}
}

func TestConfiguredAgentHelper(t *testing.T) {
	const marker = "--configured-agent-helper"

	markerIndex := -1
	for i, arg := range os.Args {
		if arg == marker {
			markerIndex = i
			break
		}
	}
	if markerIndex == -1 {
		return
	}
	if markerIndex+2 >= len(os.Args) {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[markerIndex+1], []byte("# PRD\n\n- [x] Task one\n"), 0o600); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(os.Args[markerIndex+2], nil, 0o600); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestAbandonFirstOpenItem(t *testing.T) {
	t.Run("abandons first open item when multiple exist", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "prd.md")
		content := "# PRD\n\n- [x] Done\n- [ ] First open\n- [ ] Second open\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := abandonFirstOpenItem(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "- [ ] First open" {
			t.Errorf("got %q, want %q", got, "- [ ] First open")
		}

		updated, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			t.Fatal(err)
		}
		updatedStr := string(updated)
		if !strings.Contains(updatedStr, "- [~] First open") {
			t.Error("expected first open item to be abandoned with [~]")
		}
		if !strings.Contains(updatedStr, "- [ ] Second open") {
			t.Error("expected second open item to remain unchanged")
		}
	})

	t.Run("returns empty string when no open items", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "prd.md")
		content := "# PRD\n\n- [x] Done\n- [~] Abandoned\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := abandonFirstOpenItem(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
		// file should be unchanged
		after, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != content {
			t.Error("expected file to be unchanged when no open items")
		}
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := abandonFirstOpenItem(filepath.Join(dir, "nonexistent.md"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("replaces only exact prefix not mid-line text", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "prd.md")
		content := "# PRD\n\n- [ ] Task with - [ ] in description\n- [ ] Second\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := abandonFirstOpenItem(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "- [ ] Task with - [ ] in description" {
			t.Errorf("got %q", got)
		}

		updated, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			t.Fatal(err)
		}
		updatedStr := string(updated)
		// The first line should be abandoned; the mid-line text is preserved as-is
		if !strings.Contains(updatedStr, "- [~] Task with - [ ] in description") {
			t.Errorf("expected first line abandoned, got:\n%s", updatedStr)
		}
		if !strings.Contains(updatedStr, "- [ ] Second") {
			t.Error("expected second item still open")
		}
	})

	t.Run("file has exactly one fewer open and one more abandoned", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "prd.md")
		content := "- [ ] A\n- [ ] B\n- [ ] C\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := abandonFirstOpenItem(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		after, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			t.Fatal(err)
		}
		afterStr := string(after)

		openCount := strings.Count(afterStr, "- [ ]")
		abandonCount := strings.Count(afterStr, "- [~]")
		if openCount != 2 {
			t.Errorf("expected 2 open items after abandon, got %d", openCount)
		}
		if abandonCount != 1 {
			t.Errorf("expected 1 abandoned item after abandon, got %d", abandonCount)
		}
	})
}

func TestInvokeAgent(t *testing.T) {
	t.Run("returns nil on zero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		stubRunner := func(_ context.Context, _ string) (string, error) { return "", nil }

		if _, err := invokeAgent(context.Background(), promptPath, "prd.md", "progress.txt", stubRunner); err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})

	t.Run("returns error on nonzero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		stubRunner := func(_ context.Context, _ string) (string, error) { return "", errors.New("exit status 1") }

		if _, err := invokeAgent(context.Background(), promptPath, "prd.md", "progress.txt", stubRunner); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("passes combined prompt to runner", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		promptContent := "# My Prompt\n"
		if err := os.WriteFile(promptPath, []byte(promptContent), 0o644); err != nil {
			t.Fatal(err)
		}

		prdPath := "/path/to/PRD.md"
		progressPath := "/path/to/progress.txt"

		var capturedStdin string
		stubRunner := func(_ context.Context, stdin string) (string, error) {
			capturedStdin = stdin
			return "", nil
		}

		if _, err := invokeAgent(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(capturedStdin, promptContent) {
			t.Errorf("expected stdin to contain prompt content, got: %q", capturedStdin)
		}
		if !strings.Contains(capturedStdin, "## Runtime paths") {
			t.Errorf("expected stdin to contain runtime paths header, got: %q", capturedStdin)
		}
		if !strings.Contains(capturedStdin, "- PRD: "+prdPath) {
			t.Errorf("expected stdin to contain PRD path, got: %q", capturedStdin)
		}
		if !strings.Contains(capturedStdin, "- Progress: "+progressPath) {
			t.Errorf("expected stdin to contain Progress path, got: %q", capturedStdin)
		}
	})

	t.Run("returns error for missing prompt file", func(t *testing.T) {
		dir := t.TempDir()
		called := false

		stubRunner := func(_ context.Context, _ string) (string, error) {
			called = true
			return "", nil
		}

		_, err := invokeAgent(context.Background(), filepath.Join(dir, "missing.md"), "prd.md", "progress.txt", stubRunner)
		if err == nil {
			t.Fatal("expected error for missing prompt file, got nil")
		}
		if called {
			t.Error("expected runner not to be called when prompt file is missing")
		}
	})
}

func TestRunLoop_NoOpenItems(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [x] Done\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	stubRunner := func(_ context.Context, _ string) (string, error) {
		called = true
		return "", nil
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, 3, stubRunner); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if called {
		t.Error("expected agent runner not to be called when no open items")
	}
}

func TestRunLoop_LogsHumanReadableItemLabels(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] **Cycle 1 - Add GetByIDs to pattern repository**: long details here\n  - Agent: `technical writer`\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stubRunner := func(_ context.Context, _ string) (string, error) {
		return "", os.WriteFile(prdPath, []byte("# PRD\n\n- [x] **Cycle 1 - Add GetByIDs to pattern repository**: long details here\n"), 0o644) // #nosec G304
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	runErr := runLoop(context.Background(), promptPath, prdPath, progressPath, 3, stubRunner)

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	if runErr != nil {
		t.Fatalf("expected nil, got: %v", runErr)
	}

	output := buf.String()
	if !strings.Contains(output, `Cycle 1: Add GetByIDs to pattern repository`) {
		t.Fatalf("expected cycle header, got:\n%s", output)
	}
	if !strings.Contains(output, `- Agent: technical writer`) {
		t.Fatalf("expected agent line, got:\n%s", output)
	}
	if !strings.Contains(output, `- Status: Running ... (1/3)`) {
		t.Fatalf("expected running status, got:\n%s", output)
	}
	if !strings.Contains(output, `- Status: Complete`) {
		t.Fatalf("expected complete status, got:\n%s", output)
	}
	if !strings.Contains(output, `- Files Changed:`) {
		t.Fatalf("expected files-changed section, got:\n%s", output)
	}
	if strings.Contains(output, "long details here") {
		t.Fatalf("expected verbose item details to be omitted from logs, got:\n%s", output)
	}
}

func TestRunLoop_CompletionDetected(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] Task one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	stubRunner := func(_ context.Context, _ string) (string, error) {
		callCount++
		return "", os.WriteFile(prdPath, []byte("# PRD\n\n- [x] Task one\n"), 0o644) // #nosec G304
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, 3, stubRunner); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected agent runner called once, got %d", callCount)
	}
	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "- [ ]") {
		t.Error("expected no open items after completion")
	}
}

func TestRunLoopFatalInvocationPreservesPRD(t *testing.T) {
	t.Run("invalid configuration", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		prdPath := filepath.Join(dir, "prd.md")
		originalPRD := []byte("# PRD\n\n- [ ] Task must remain open\n")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prdPath, originalPRD, 0o644); err != nil {
			t.Fatal(err)
		}

		config := testConfig(promptPath, prdPath, "", 3)
		config.AgentCommand.Executable = ""
		err := Start(context.Background(), config)
		if err == nil {
			t.Fatal("expected configuration failure, got nil")
		}
		if !errors.Is(err, agent.ErrInvalidConfiguration) {
			t.Fatalf("Start() error = %v, want configuration category", err)
		}

		after, readErr := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, originalPRD) {
			t.Fatalf("configuration failure changed PRD:\nwant: %q\n got: %q", originalPRD, after)
		}
	})

	t.Run("missing configured executable", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		prdPath := filepath.Join(dir, "prd.md")
		originalPRD := []byte("# PRD\n\n- [ ] Task must remain open\n")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prdPath, originalPRD, 0o644); err != nil {
			t.Fatal(err)
		}

		config := testConfig(promptPath, prdPath, "", 3)
		config.AgentCommand.Executable = filepath.Join(dir, "missing-agent")
		err := Start(context.Background(), config)
		if err == nil {
			t.Fatal("expected process-start failure, got nil")
		}
		if !errors.Is(err, agent.ErrProcessStart) {
			t.Fatalf("Start() error = %v, want process-start category", err)
		}

		after, readErr := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, originalPRD) {
			t.Fatalf("fatal invocation changed PRD:\nwant: %q\n got: %q", originalPRD, after)
		}
	})

	t.Run("prompt read failure", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "missing-prompt.md")
		prdPath := filepath.Join(dir, "prd.md")
		originalPRD := []byte("# PRD\n\n- [ ] Task must remain open\n")
		if err := os.WriteFile(prdPath, originalPRD, 0o644); err != nil {
			t.Fatal(err)
		}

		callCount := 0
		stubRunner := func(_ context.Context, _ string) (string, error) {
			callCount++
			return "", nil
		}
		err := runLoop(context.Background(), promptPath, prdPath, filepath.Join(dir, "progress.txt"), 3, stubRunner)
		if err == nil {
			t.Fatal("expected prompt read failure, got nil")
		}
		if callCount != 0 {
			t.Fatalf("runner called %d times, want 0", callCount)
		}

		after, readErr := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, originalPRD) {
			t.Fatalf("prompt read failure changed PRD:\nwant: %q\n got: %q", originalPRD, after)
		}
	})

	t.Run("fatal runner error returns after one attempt and cleans output", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		prdPath := filepath.Join(dir, "prd.md")
		outputPath := filepath.Join(dir, "agent-output.log")
		originalPRD := []byte("# PRD\n\n- [ ] Task must remain open\n")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prdPath, originalPRD, 0o644); err != nil {
			t.Fatal(err)
		}

		callCount := 0
		stubRunner := func(_ context.Context, _ string) (string, error) {
			callCount++
			if err := os.WriteFile(outputPath, []byte("setup failed"), 0o600); err != nil {
				return "", err
			}
			return outputPath, fmt.Errorf("%w: output setup failed", agent.ErrSetup)
		}

		err := runLoop(context.Background(), promptPath, prdPath, filepath.Join(dir, "progress.txt"), 3, stubRunner)
		if err == nil {
			t.Fatal("expected fatal invocation error, got nil")
		}
		if !errors.Is(err, agent.ErrSetup) {
			t.Fatalf("runLoop() error = %v, want setup category", err)
		}
		if callCount != 1 {
			t.Fatalf("runner called %d times, want 1", callCount)
		}
		if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("fatal invocation output was not cleaned up: %v", statErr)
		}

		after, readErr := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, originalPRD) {
			t.Fatalf("fatal invocation changed PRD:\nwant: %q\n got: %q", originalPRD, after)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		prdPath := filepath.Join(dir, "prd.md")
		originalPRD := []byte("# PRD\n\n- [ ] Task must remain open\n")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prdPath, originalPRD, 0o644); err != nil {
			t.Fatal(err)
		}

		callCount := 0
		stubRunner := func(_ context.Context, _ string) (string, error) {
			callCount++
			return "", fmt.Errorf("%w: %w", agent.ErrCanceled, context.Canceled)
		}
		err := runLoop(context.Background(), promptPath, prdPath, filepath.Join(dir, "progress.txt"), 3, stubRunner)
		if err == nil {
			t.Fatal("expected cancellation failure, got nil")
		}
		if !errors.Is(err, agent.ErrCanceled) || !errors.Is(err, context.Canceled) {
			t.Fatalf("runLoop() error = %v, want cancellation categories", err)
		}
		if callCount != 1 {
			t.Fatalf("runner called %d times, want 1", callCount)
		}

		after, readErr := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, originalPRD) {
			t.Fatalf("cancellation changed PRD:\nwant: %q\n got: %q", originalPRD, after)
		}
	})
}

func TestRunLoop_RetriesClassifiedNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] Retryable task\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	stubRunner := func(_ context.Context, _ string) (string, error) {
		callCount++
		return "", fmt.Errorf("%w: exit status 1", agent.ErrNonZeroExit)
	}

	if err := runLoop(context.Background(), promptPath, prdPath, filepath.Join(dir, "progress.txt"), 2, stubRunner); err != nil {
		t.Fatalf("expected retryable exit to reach abandonment, got: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("runner called %d times, want 2", callCount)
	}
	after, err := os.ReadFile(prdPath) // #nosec G304 -- path is created by the test
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "- [~] Retryable task") {
		t.Fatalf("expected retryable task to be abandoned, got:\n%s", after)
	}
}

func TestRunLoop_AbandonAtLimit(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] Stubborn task\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	stubRunner := func(_ context.Context, _ string) (string, error) {
		callCount++
		return "", nil // never modifies PRD
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, 3, stubRunner); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected agent runner called 3 times, got %d", callCount)
	}
	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- [~] Stubborn task") {
		t.Errorf("expected item abandoned, got:\n%s", string(data))
	}
}

func TestRunLoop_AttemptResetOnNewItem(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] First task\n- [ ] Second task\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	stubRunner := func(_ context.Context, _ string) (string, error) {
		callCount++
		// Complete the first task on the 2nd call; second task is never completed.
		if callCount == 2 {
			return "", os.WriteFile(prdPath, []byte("# PRD\n\n- [x] First task\n- [ ] Second task\n"), 0o644) // #nosec G304
		}
		return "", nil
	}

	// maxAttempts=2: first item requires 2 attempts (completes on 2nd),
	// second item gets 2 attempts then is abandoned.
	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, 2, stubRunner); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "- [x] First task") {
		t.Errorf("expected first task completed, got:\n%s", content)
	}
	if !strings.Contains(content, "- [~] Second task") {
		t.Errorf("expected second task abandoned, got:\n%s", content)
	}
}

func TestGetFirstOpenItem(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		content  string
		wantLine string
		wantErr  bool
		usePath  string // if non-empty, use this path instead of writing content
	}{
		{
			name:     "returns first open item from multiple",
			content:  "# PRD\n\n- [x] Done item\n- [ ] First open item\n- [ ] Second open item\n",
			wantLine: "- [ ] First open item",
		},
		{
			name:     "returns empty string when no open items",
			content:  "# PRD\n\n- [x] Done item\n- [~] Abandoned item\n",
			wantLine: "",
		},
		{
			name:     "returns correct open item with mixed states",
			content:  "# PRD\n\n- [x] Completed\n- [~] Abandoned\n- [ ] Open one\n- [ ] Open two\n",
			wantLine: "- [ ] Open one",
		},
		{
			name:    "returns error for nonexistent file",
			usePath: filepath.Join(dir, "nonexistent.md"),
			wantErr: true,
		},
		{
			name:     "returns open item preceded by headers and content",
			content:  "# Title\n\n## Objective\n\nSome prose here.\n\n## Plan\n\n- [x] Already done\n- [ ] First real task\n",
			wantLine: "- [ ] First real task",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.usePath
			if path == "" {
				path = filepath.Join(dir, tc.name+".md")
				if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			got, err := getFirstOpenItem(path)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantLine {
				t.Errorf("got %q, want %q", got, tc.wantLine)
			}
		})
	}
}

func TestItemLabel(t *testing.T) {
	tests := []struct {
		name string
		item string
		want string
	}{
		{
			name: "strips checklist marker and details",
			item: "- [ ] **Cycle 1 - Add GetByIDs to pattern repository**: long details here",
			want: "Cycle 1 - Add GetByIDs to pattern repository",
		},
		{
			name: "collapses whitespace",
			item: "- [ ]   `Cycle 2`   :   more details",
			want: "Cycle 2",
		},
		{
			name: "falls back for empty item",
			item: "- [ ]   ",
			want: "unnamed item",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := itemLabel(tc.item); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCycleHeader(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{
			name:  "cycle with title",
			label: "Cycle 10 - Migrate embeddings model",
			want:  "Cycle 10: Migrate embeddings model",
		},
		{
			name:  "non-cycle label passthrough",
			label: "Task one",
			want:  "Task one",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cycleHeader(tc.label); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetFirstOpenItemMeta(t *testing.T) {
	t.Run("extracts agent from first open cycle block", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "prd.md")
		content := "# PRD\n\n- [ ] **Cycle 10 - Refactor output format**: details\n  - Agent: `technical writer`\n- [ ] **Cycle 11 - Next**: details\n  - Agent: `go-software-engineer`\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := getFirstOpenItemMeta(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.item != "- [ ] **Cycle 10 - Refactor output format**: details" {
			t.Fatalf("got item %q", got.item)
		}
		if got.agent != "technical writer" {
			t.Fatalf("got agent %q", got.agent)
		}
	})

	t.Run("uses unknown when no agent line", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "prd.md")
		content := "# PRD\n\n- [ ] Task one\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := getFirstOpenItemMeta(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.agent != "unknown" {
			t.Fatalf("got agent %q, want unknown", got.agent)
		}
	})
}
