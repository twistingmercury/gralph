package looper

import (
	"bytes"
	"context"
	"errors"
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

func TestInvokeClaude(t *testing.T) {
	t.Run("returns nil on zero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		stubRunner := func(_ context.Context, _ string) (string, error) { return "", nil }

		if _, err := invokeClaude(context.Background(), promptPath, "prd.md", "progress.txt", stubRunner); err != nil {
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

		if _, err := invokeClaude(context.Background(), promptPath, "prd.md", "progress.txt", stubRunner); err == nil {
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

		if _, err := invokeClaude(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
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

		_, err := invokeClaude(context.Background(), filepath.Join(dir, "missing.md"), "prd.md", "progress.txt", stubRunner)
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
		t.Error("expected claudeRunner not to be called when no open items")
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
		t.Errorf("expected claudeRunner called once, got %d", callCount)
	}
	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "- [ ]") {
		t.Error("expected no open items after completion")
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
		t.Errorf("expected claudeRunner called 3 times, got %d", callCount)
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
