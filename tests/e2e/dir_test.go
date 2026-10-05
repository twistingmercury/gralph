package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dirTasksYAML = `tasks:
  - id: 1
    name: Folder task
    prompt: Do the folder thing.
`

// TestDir_RunsTheFoldersFiles pins that -d alone stands in for --tasks and
// --prompt: claude gets the folder's prompt and the folder's task.
func TestDir_RunsTheFoldersFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePrompt(t, dir, "FOLDER PROMPT\n")
	tasksPath := writeTasksYAML(t, dir, dirTasksYAML)

	recordFile := filepath.Join(t.TempDir(), "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "-d", dir}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 1)
	assert.Equal(t, expectedStdin("FOLDER PROMPT", 1, "Folder task", "Do the folder thing."), records[0].Stdin)

	after := readTasksYAML(t, tasksPath)
	require.Len(t, after.Tasks, 1)
	assert.Equal(t, "completed", after.Tasks[0].State)
}

// TestDir_TasksFlagWins pins that -t overrides the folder's tasks.yaml while
// the folder still supplies prompt.md.
func TestDir_TasksFlagWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePrompt(t, dir, "FOLDER PROMPT\n")
	folderTasks := writeTasksYAML(t, dir, dirTasksYAML)

	otherDir := t.TempDir()
	otherYAML := `tasks:
  - id: 7
    name: Other task
    prompt: Do the other thing.
`
	otherTasks := writeTasksYAML(t, otherDir, otherYAML)

	recordFile := filepath.Join(t.TempDir(), "record.ndjson")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_RECORD_FILE": recordFile,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "-d", dir, "-t", otherTasks}, env)
	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)

	records := readFakeClaudeRecords(t, recordFile)
	require.Len(t, records, 1)
	assert.Equal(t, expectedStdin("FOLDER PROMPT", 7, "Other task", "Do the other thing."), records[0].Stdin)

	raw, err := os.ReadFile(folderTasks)
	require.NoError(t, err)
	assert.Equal(t, dirTasksYAML, string(raw), "expected the folder's task file to be untouched")
}

// TestDir_MissingTasksFile pins the error for a folder without tasks.yaml,
// and that claude never starts.
func TestDir_MissingTasksFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	attemptLog := filepath.Join(t.TempDir(), "attempts.log")
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
	})

	res := runGralph(t, 15*time.Second, []string{"--skip-permissions", "-d", dir}, env)

	require.Equal(t, 1, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Equal(t, "error: --dir: no tasks.yaml in "+dir+"\n", res.stderr)
	assert.Equal(t, 0, countAttempts(t, attemptLog), "expected claude never invoked")
}

// TestDir_DryRunNeedsNoPrompt pins that a dry run with -d reads only the
// folder's tasks.yaml.
func TestDir_DryRunNeedsNoPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tasksPath := writeTasksYAML(t, dir, dirTasksYAML)

	res := runGralph(t, 15*time.Second, []string{"--dry-run", "-d", dir}, gralphEnv(fakeClaudeDir, nil))

	require.Equal(t, 0, res.exitCode, "stdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	assert.Contains(t, res.stdout, tasksPath+" is valid\n")
}
