package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClaudeLoop_Success_MultiItem drives gralph through a PRD with two open
// items using the fake claude fixture, and asserts the full happy path: one
// invocation per item, both items marked done, and the exact argv/stdin
// (including the Runtime paths block) gralph sends to claude.
func TestClaudeLoop_Success_MultiItem(t *testing.T) {
	dir := t.TempDir()

	prd := writePRD(t, dir,
		"- [ ] First item: do the thing",
		"- [ ] Second item: do another thing",
	)
	promptBody := "PROMPT BODY MARKER\nDo the work described in the PRD.\n"
	prompt := writePrompt(t, dir, promptBody)

	recordFile := filepath.Join(dir, "record.json")
	attemptLog := filepath.Join(dir, "attempts.log")

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE":      recordFile,
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout:\n%s\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "[gralph] done") {
		t.Errorf("expected stdout to contain %q; got:\n%s", "[gralph] done", res.stdout)
	}

	prdContent, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	if strings.Contains(string(prdContent), "- [ ]") {
		t.Errorf("expected all items done, PRD still has an open item:\n%s", string(prdContent))
	}
	if got := strings.Count(string(prdContent), "- [x]"); got != 2 {
		t.Errorf("expected 2 completed items, got %d:\n%s", got, string(prdContent))
	}

	if attempts := countAttempts(t, attemptLog); attempts != 2 {
		t.Errorf("expected claude invoked exactly twice (once per item), got %d", attempts)
	}

	rec := readFakeClaudeRecord(t, recordFile)
	wantArgv := []string{"--print", "--dangerously-skip-permissions"}
	if !equalArgv(rec.Argv, wantArgv) {
		t.Errorf("expected recorded argv %v, got %v", wantArgv, rec.Argv)
	}
	if !strings.Contains(rec.Stdin, promptBody) {
		t.Errorf("expected recorded stdin to contain the prompt body %q; got:\n%s", promptBody, rec.Stdin)
	}
	wantBlock := runtimeBlock(prd, defaultProgressPath(prd))
	if !strings.Contains(rec.Stdin, wantBlock) {
		t.Errorf("expected recorded stdin to contain the exact runtime paths block %q; got:\n%s", wantBlock, rec.Stdin)
	}

	if _, err := os.Stat(defaultProgressPath(prd)); err != nil {
		t.Errorf("expected default progress file to exist: %v", err)
	}
}

// TestClaudeLoop_CompletedDespiteNonZeroExit verifies that a claude
// invocation which changes the PRD item still counts as completed even when
// claude itself exited non-zero.
func TestClaudeLoop_CompletedDespiteNonZeroExit(t *testing.T) {
	dir := t.TempDir()

	prd := writePRD(t, dir, "- [ ] Only item: finish it")
	prompt := writePrompt(t, dir, "Body.\n")

	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_EXIT_CODE": "7",
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	if res.exitCode != 0 {
		t.Fatalf("expected exit 0 when the item changed despite a non-zero claude exit, got %d; stdout:\n%s\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "[gralph] completed item=") {
		t.Errorf("expected stdout to report the item completed; got:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "[gralph] done") {
		t.Errorf("expected stdout to contain %q; got:\n%s", "[gralph] done", res.stdout)
	}

	prdContent, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	if !strings.Contains(string(prdContent), "- [x]") {
		t.Errorf("expected item marked done despite non-zero claude exit:\n%s", string(prdContent))
	}
}

// TestClaudeLoop_FailFast_NonZeroExit covers the fail-fast branch where
// claude exits non-zero and leaves the item unchanged: exactly one
// invocation, non-zero gralph exit, PRD untouched, next item never started.
func TestClaudeLoop_FailFast_NonZeroExit(t *testing.T) {
	dir := t.TempDir()

	prd := writePRD(t, dir,
		"- [ ] First item: never gets touched",
		"- [ ] Second item: should never start",
	)
	prompt := writePrompt(t, dir, "Body.\n")

	original, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD fixture: %v", err)
	}

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_SKIP_PRD_UPDATE":  "1",
		"FAKECLAUDE_EXIT_CODE":        "3",
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit for an unchanged item; stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, `[gralph] failed item=`) {
		t.Errorf("expected stdout to report the failed item; got:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "cycle failed") {
		t.Errorf("expected stderr to mention a cycle failure; got:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr, "exit status 3") {
		t.Errorf("expected stderr to surface the claude exit status; got:\n%s", res.stderr)
	}

	if attempts := countAttempts(t, attemptLog); attempts != 1 {
		t.Errorf("expected exactly one claude invocation, got %d", attempts)
	}

	after, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf("expected PRD to be byte-for-byte unchanged\nbefore:\n%s\nafter:\n%s", string(original), string(after))
	}
	if strings.Contains(string(after), "- [~]") {
		t.Errorf("expected no abandoned (- [~]) items; got:\n%s", string(after))
	}
}

// TestClaudeLoop_FailFast_ZeroExit covers the fail-fast branch where claude
// exits zero (claiming success) but leaves the item unchanged.
func TestClaudeLoop_FailFast_ZeroExit(t *testing.T) {
	dir := t.TempDir()

	prd := writePRD(t, dir, "- [ ] Only item: never gets touched")
	prompt := writePrompt(t, dir, "Body.\n")

	original, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD fixture: %v", err)
	}

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_SKIP_PRD_UPDATE":  "1",
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit for an unchanged item even when claude exited 0; stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, `[gralph] failed item=`) {
		t.Errorf("expected stdout to report the failed item; got:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "cycle failed") {
		t.Errorf("expected stderr to mention a cycle failure; got:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, "exit status") {
		t.Errorf("expected no claude exit status in stderr when claude exited 0; got:\n%s", res.stderr)
	}

	if attempts := countAttempts(t, attemptLog); attempts != 1 {
		t.Errorf("expected exactly one claude invocation, got %d", attempts)
	}

	after, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf("expected PRD to be byte-for-byte unchanged\nbefore:\n%s\nafter:\n%s", string(original), string(after))
	}
}

// TestClaudeLoop_TildeItemsSkipped verifies that existing "- [~]" items are
// treated as closed: gralph skips straight to the first "- [ ]" item and
// never invokes claude for the "- [~]" one.
func TestClaudeLoop_TildeItemsSkipped(t *testing.T) {
	dir := t.TempDir()

	prd := writePRD(t, dir,
		"- [~] Abandoned item: left as-is",
		"- [ ] Open item: gets done",
	)
	prompt := writePrompt(t, dir, "Body.\n")

	attemptLog := filepath.Join(dir, "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stdout:\n%s\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}

	if attempts := countAttempts(t, attemptLog); attempts != 1 {
		t.Errorf("expected exactly one claude invocation (for the open item only), got %d", attempts)
	}

	after, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	lines := strings.Split(string(after), "\n")
	if !containsLine(lines, "- [~] Abandoned item: left as-is") {
		t.Errorf("expected the pre-existing abandoned item to be left untouched; got:\n%s", string(after))
	}
	if !containsLine(lines, "- [x] Open item: gets done") {
		t.Errorf("expected the open item to be marked done; got:\n%s", string(after))
	}
}

// TestIterationsFlagRejected verifies that --iterations no longer exists as
// a flag: gralph must reject it as unknown rather than silently accepting or
// ignoring it.
func TestIterationsFlagRejected(t *testing.T) {
	result := runCLI(t, "--iterations=3")
	if result.exitCode == 0 {
		t.Fatalf("expected non-zero exit for the removed --iterations flag; stdout:\n%s\nstderr:\n%s", result.stdout, result.stderr)
	}
	if !strings.Contains(strings.ToLower(result.stderr), "iterations") {
		t.Errorf("expected stderr to mention the unknown --iterations flag; got:\n%s", result.stderr)
	}
}

// TestClaudeMissingFromPath verifies that when claude cannot be resolved on
// PATH at all, gralph still fails fast (single failed cycle, PRD untouched)
// rather than hanging or crashing.
func TestClaudeMissingFromPath(t *testing.T) {
	dir := t.TempDir()
	emptyPathDir := t.TempDir()

	prd := writePRD(t, dir, "- [ ] Only item: never gets touched")
	prompt := writePrompt(t, dir, "Body.\n")

	original, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD fixture: %v", err)
	}

	env := gralphEnvWithPath(emptyPathDir, nil)
	res := runGralph(t, 15*time.Second, []string{"--prompt=" + prompt, "--prd=" + prd}, env)

	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit when claude is missing from PATH; stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	}
	lowerErr := strings.ToLower(res.stderr)
	if !strings.Contains(lowerErr, "claude") {
		t.Errorf("expected stderr to mention claude; got:\n%s", res.stderr)
	}
	if !strings.Contains(lowerErr, "not found") {
		t.Errorf("expected stderr to indicate claude could not be found; got:\n%s", res.stderr)
	}

	after, err := os.ReadFile(prd)
	if err != nil {
		t.Fatalf("read PRD after run: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf("expected PRD to be byte-for-byte unchanged when claude is missing\nbefore:\n%s\nafter:\n%s", string(original), string(after))
	}
}

func equalArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
