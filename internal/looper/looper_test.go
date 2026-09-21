package looper

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStart_MissingPrompt(t *testing.T) {
	dir := t.TempDir()
	prd := filepath.Join(dir, "prd.md")
	require.NoError(t, os.WriteFile(prd, []byte("# PRD\n"), 0o644))

	err := Start(context.Background(), filepath.Join(dir, "missing.md"), prd)
	assert.Error(t, err)
}

func TestStart_MissingPRD(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	require.NoError(t, os.WriteFile(prompt, []byte("# Prompt\n"), 0o644))

	err := Start(context.Background(), prompt, filepath.Join(dir, "missing.md"))
	assert.Error(t, err)
}

func TestStart_NoProgressFileCreated(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	prd := filepath.Join(dir, "PRD.md")
	require.NoError(t, os.WriteFile(prompt, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prd, []byte("# PRD\n"), 0o644))

	// PRD has no open items, so runLoop returns nil immediately without
	// exec'ing a real claude. Nothing in the Start/runLoop path creates a
	// progress file anymore.
	err := Start(context.Background(), prompt, prd)
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotEqual(t, "progress.txt", entry.Name(), "no progress.txt should be created")
	}
}

func TestInvokeClaude(t *testing.T) {
	t.Run("returns nil on zero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))

		stubRunner := func(_ context.Context, _ string) error { return nil }

		err := invokeClaude(context.Background(), promptPath, "prd.md", stubRunner)
		assert.NoError(t, err)
	})

	t.Run("returns error on nonzero exit", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))

		stubRunner := func(_ context.Context, _ string) error { return errors.New("exit status 1") }

		err := invokeClaude(context.Background(), promptPath, "prd.md", stubRunner)
		assert.Error(t, err)
	})

	t.Run("passes combined prompt to runner", func(t *testing.T) {
		dir := t.TempDir()
		promptPath := filepath.Join(dir, "prompt.md")
		promptContent := "# My Prompt\n"
		require.NoError(t, os.WriteFile(promptPath, []byte(promptContent), 0o644))

		prdPath := "/path/to/PRD.md"

		var capturedStdin string
		stubRunner := func(_ context.Context, stdin string) error {
			capturedStdin = stdin
			return nil
		}

		err := invokeClaude(context.Background(), promptPath, prdPath, stubRunner)
		require.NoError(t, err)

		assert.Contains(t, capturedStdin, promptContent)
		assert.Equal(t, promptContent+"\n## Runtime paths\n- PRD: "+prdPath+"\n", capturedStdin)
		assert.NotContains(t, capturedStdin, "Progress")
	})

	t.Run("returns error for missing prompt file", func(t *testing.T) {
		dir := t.TempDir()
		called := false

		stubRunner := func(_ context.Context, _ string) error {
			called = true
			return nil
		}

		err := invokeClaude(context.Background(), filepath.Join(dir, "missing.md"), "prd.md", stubRunner)
		assert.Error(t, err)
		assert.False(t, called, "expected runner not to be called when prompt file is missing")
	})
}

func TestRunLoop_NoOpenItems(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte("# PRD\n\n- [x] Done\n"), 0o644))

	called := false
	stubRunner := func(_ context.Context, _ string) error {
		called = true
		return nil
	}

	err := runLoop(context.Background(), promptPath, prdPath, stubRunner)
	require.NoError(t, err)
	assert.False(t, called, "expected claudeRunner not to be called when no open items")
}

func TestRunLoop_LogsHumanReadableItemLabels(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] **Cycle 1 - Add GetByIDs to pattern repository**: long details here\n"), 0o644))

	stubRunner := func(_ context.Context, _ string) error {
		return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] **Cycle 1 - Add GetByIDs to pattern repository**: long details here\n"), 0o644)
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runLoop(context.Background(), promptPath, prdPath, stubRunner)

	require.NoError(t, w.Close())
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)

	require.NoError(t, runErr)

	output := buf.String()
	assert.Contains(t, output, `[gralph] attempt item="Cycle 1 - Add GetByIDs to pattern repository"`)
	assert.Contains(t, output, `[gralph] claude_output_begin item="Cycle 1 - Add GetByIDs to pattern repository"`)
	assert.Contains(t, output, `[gralph] claude_output_end item="Cycle 1 - Add GetByIDs to pattern repository" status=ok`)
	assert.NotContains(t, output, "long details here", "expected verbose item details to be omitted from logs")
}

func TestRunLoop_CompletionDetected(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] Task one\n"), 0o644))

	callCount := 0
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] Task one\n"), 0o644)
	}

	err := runLoop(context.Background(), promptPath, prdPath, stubRunner)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "expected claudeRunner called once")

	data, err := os.ReadFile(prdPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "- [ ]", "expected no open items after completion")
}

func TestRunLoop_MultipleItemsSucceed(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] First task\n- [ ] Second task\n"), 0o644))

	callCount := 0
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		switch callCount {
		case 1:
			return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] First task\n- [ ] Second task\n"), 0o644)
		case 2:
			return os.WriteFile(prdPath, []byte("# PRD\n\n- [x] First task\n- [x] Second task\n"), 0o644)
		default:
			return nil
		}
	}

	err := runLoop(context.Background(), promptPath, prdPath, stubRunner)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "expected claudeRunner called twice")

	data, err := os.ReadFile(prdPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "- [ ]", "expected no open items after both tasks completed")
}

func TestRunLoop_UnchangedItemFailsCycle(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	const prdContent = "# PRD\n\n- [ ] Stubborn task\n"
	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte(prdContent), 0o644))

	callCount := 0
	stubRunner := func(_ context.Context, _ string) error {
		callCount++
		return nil // never modifies the PRD
	}

	err := runLoop(context.Background(), promptPath, prdPath, stubRunner)
	assert.ErrorIs(t, err, ErrCycleFailed)
	assert.Equal(t, 1, callCount, "expected claudeRunner called exactly once")

	data, err := os.ReadFile(prdPath)
	require.NoError(t, err)
	assert.Equal(t, prdContent, string(data), "expected PRD to be left untouched")
}

func TestRunLoop_UnchangedItemNextItemNeverStarted(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] First task\n- [ ] Second task\n"), 0o644))

	var capturedPrompts []string
	stubRunner := func(_ context.Context, prompt string) error {
		capturedPrompts = append(capturedPrompts, prompt)
		return nil // never modifies the PRD
	}

	err := runLoop(context.Background(), promptPath, prdPath, stubRunner)
	assert.ErrorIs(t, err, ErrCycleFailed)
	require.Len(t, capturedPrompts, 1, "expected exactly one invocation")
}

func TestRunLoop_ChangedItemWithRunnerErrorContinues(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte("# PRD\n\n- [ ] Task one\n"), 0o644))

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

	err := runLoop(context.Background(), promptPath, prdPath, stubRunner)
	assert.NoError(t, err, "expected nil (cycle completes despite runner error since item changed)")
	assert.Equal(t, 1, callCount, "expected claudeRunner called once")
}

func TestRunLoop_CancellationReturnsContextErrorAndLeavesPRDUntouched(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.md")
	prdPath := filepath.Join(dir, "prd.md")

	const prdContent = "# PRD\n\n- [ ] Task one\n"
	require.NoError(t, os.WriteFile(promptPath, []byte("# Prompt\n"), 0o644))
	require.NoError(t, os.WriteFile(prdPath, []byte(prdContent), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	stubRunner := func(_ context.Context, _ string) error {
		cancel()
		return context.Canceled
	}

	err := runLoop(ctx, promptPath, prdPath, stubRunner)
	assert.ErrorIs(t, err, context.Canceled)

	data, err := os.ReadFile(prdPath)
	require.NoError(t, err)
	assert.Equal(t, prdContent, string(data), "expected PRD to be left untouched")
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
				require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o644))
			}

			got, err := getFirstOpenItem(path)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLine, got)
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
			assert.Equal(t, tc.want, itemLabel(tc.item))
		})
	}
}
