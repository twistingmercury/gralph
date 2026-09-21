package looper

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStart_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	prd := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(prd, []byte("# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Start(context.Background(), filepath.Join(dir, "missing.md"), prd, "")
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

	err := Start(context.Background(), prompt, filepath.Join(dir, "missing.md"), "")
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
	if err := Start(context.Background(), prompt, prd, ""); err != nil {
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

	if err := Start(context.Background(), prompt, prd, explicit); err != nil {
		t.Fatalf("unexpected error from Start: %v", err)
	}

	if _, err := os.Stat(explicit); err != nil {
		t.Fatalf("expected explicit progress file at %s to exist, got: %v", explicit, err)
	}
}

func TestInvokeClaude(t *testing.T) {
	t.Run("returns nil on zero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		stubRunner := func(_ context.Context, _ string) error { return nil }

		if err := invokeClaude(context.Background(), promptPath, "prd.md", "progress.txt", stubRunner); err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	})

	t.Run("returns error on nonzero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		stubRunner := func(_ context.Context, _ string) error { return errors.New("exit status 1") }

		if err := invokeClaude(context.Background(), promptPath, "prd.md", "progress.txt", stubRunner); err == nil {
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
		stubRunner := func(_ context.Context, stdin string) error {
			capturedStdin = stdin
			return nil
		}

		if err := invokeClaude(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
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

		stubRunner := func(_ context.Context, _ string) error {
			called = true
			return nil
		}

		err := invokeClaude(context.Background(), filepath.Join(dir, "missing.md"), "prd.md", "progress.txt", stubRunner)
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
	stubRunner := func(_ context.Context, _ string) error {
		called = true
		return nil
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
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
	if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] **Cycle 1 - Add GetByIDs to pattern repository**: long details here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stubRunner := func(_ context.Context, _ string) error {
		return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] **Cycle 1 - Add GetByIDs to pattern repository**: long details here\n"), 0o644) // #nosec G304
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	runErr := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner)

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
	if !strings.Contains(output, `[gralph] attempt item="Cycle 1 - Add GetByIDs to pattern repository"`) {
		t.Fatalf("expected shortened attempt log, got:\n%s", output)
	}
	if !strings.Contains(output, `[gralph] claude_output_begin item="Cycle 1 - Add GetByIDs to pattern repository"`) {
		t.Fatalf("expected claude output start marker, got:\n%s", output)
	}
	if !strings.Contains(output, `[gralph] claude_output_end item="Cycle 1 - Add GetByIDs to pattern repository" status=ok`) {
		t.Fatalf("expected claude output end marker, got:\n%s", output)
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
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] Task one\n"), 0o644) // #nosec G304
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
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

func TestRunLoop_MultipleItemsSucceed(t *testing.T) {
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
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		switch callCount {
		case 1:
			return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] First task\n- [ ] Second task\n"), 0o644) // #nosec G304
		case 2:
			return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] First task\n- [x] Second task\n"), 0o644)
		default:
			return nil
		}
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected claudeRunner called twice, got %d", callCount)
	}

	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "- [ ]") {
		t.Errorf("expected no open items after both tasks completed, got:\n%s", string(data))
	}
}

func TestRunLoop_UnchangedItemFailsCycle(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	const prdContent = "# PRD\n\n- [ ] Stubborn task\n"
	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte(prdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		return nil // never modifies the PRD
	}

	err := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner)
	if !errors.Is(err, ErrCycleFailed) {
		t.Fatalf("expected ErrCycleFailed, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected claudeRunner called exactly once, got %d", callCount)
	}

	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != prdContent {
		t.Errorf("expected PRD to be left untouched, got:\n%s", string(data))
	}
}

func TestRunLoop_UnchangedItemNextItemNeverStarted(t *testing.T) {
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

	var capturedPrompts []string
	stubRunner := func(_ context.Context, prompt string) error {
		capturedPrompts = append(capturedPrompts, prompt)
		return nil // never modifies the PRD
	}

	err := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner)
	if !errors.Is(err, ErrCycleFailed) {
		t.Fatalf("expected ErrCycleFailed, got: %v", err)
	}
	if len(capturedPrompts) != 1 {
		t.Fatalf("expected exactly one invocation, got %d", len(capturedPrompts))
	}
}

func TestRunLoop_ChangedItemWithRunnerErrorContinues(t *testing.T) {
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
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		// The runner reports a nonzero-exit style error, but it still edited
		// the PRD before failing (e.g. claude made the change, then errored).
		if err := os.WriteFile(prdPath, []byte("# PRD\n\n- [x] Task one\n"), 0o644); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}

	if err := runLoop(context.Background(), promptPath, prdPath, progressPath, stubRunner); err != nil {
		t.Fatalf("expected nil (cycle completes despite runner error since item changed), got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected claudeRunner called once, got %d", callCount)
	}
}

func TestRunLoop_CancellationReturnsContextErrorAndLeavesPRDUntouched(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")
	progressPath := filepath.Join(dir, "progress.txt")

	const prdContent = "# PRD\n\n- [ ] Task one\n"
	if err := os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte(prdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stubRunner := func(_ context.Context, _ string) error {
		cancel()
		return context.Canceled
	}

	err := runLoop(ctx, promptPath, prdPath, progressPath, stubRunner)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	data, err := os.ReadFile(prdPath) // #nosec G304
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != prdContent {
		t.Errorf("expected PRD to be left untouched, got:\n%s", string(data))
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
