//go:build linux

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	keyEnter = "\r"
	keyDown  = "\x1b[B"

	streamResultEvent = `{"type":"result","result":"{\"state\":\"completed\",\"error\":\"\"}"}`
)

// TestWizardPty walks the setup wizard's happy path with real keystrokes: pick
// the run folder, choose skip permissions, keep every optional answer at its
// default, finish the gates step with Done, and Start. It pins what gralph
// saved, ran, and printed, not how the screens look.
func TestWizardPty(t *testing.T) {
	workDir := t.TempDir()

	// The folder picker lists the working directory's folders and cannot pick
	// the directory itself, so the run folder is the only entry.
	runDir := filepath.Join(workDir, "run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))

	writePrompt(t, runDir, "Body.\n")
	tasksPath := writeTasksYAML(t, runDir, `tasks:
  - id: 1
    name: First task
    prompt: Do the thing.
`)
	attemptLog := filepath.Join(workDir, "attempts.log")

	// The full-screen view runs claude with stream-json output, which the e2e
	// fake does not speak on its own, so its stdout is the result event.
	env := gralphEnv(fakeClaudeDir, map[string]string{
		"TERM":                        "xterm-256color",
		"FAKECLAUDE_ATTEMPT_LOG_FILE": attemptLog,
		"FAKE_CLAUDE_OUTPUT":          streamResultEvent,
	})
	run := startPty(t, workDir, env)

	// Each step is a title to wait for and the keys that answer it. The
	// permission step needs two Downs to get past the placeholder and the
	// sandbox choice; on the gates step one Down moves from Add to Done.
	steps := []struct {
		title string
		keys  string
	}{
		{"Run folder", keyEnter},
		{"How should sessions run?", keyDown + keyDown + keyEnter},
		{"Commit each completed task", keyEnter},
		{"Keep a record of the run", keyEnter},
		{"Gate time limit", keyEnter},
		{"Gates (run after every completed task", keyDown + keyEnter},
		{"Ready to run", keyEnter},
	}
	for _, step := range steps {
		answerStep(t, run, step.title, step.keys)
	}

	// The run view stays open after the last task so its result can be read.
	answerStep(t, run, "All tasks completed", "q")

	var exitCode int
	select {
	case exitCode = <-run.exited:
	case <-time.After(20 * time.Second):
		_ = run.cmd.Process.Kill()
		msg := fmt.Sprintf("gralph did not exit after q\noutput:\n%s", run.out.String())
		require.FailNow(t, msg)
	}

	output := run.out.String()
	assert.Equal(t, 0, exitCode, "output:\n%s", output)
	assert.Regexp(t, regexp.MustCompile(`Same run, no wizard: gralph .*\brun\b`), output)
	assert.Equal(t, 1, countAttempts(t, attemptLog), "expected one claude session")

	saved, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Contains(t, string(saved), "gates: []")
	assert.Contains(t, string(saved), "state: completed")
}

// answerStep waits for a step's title in the raw output before sending its
// keys, so a key never lands on the step before.
func answerStep(t *testing.T, run *ptyRun, title, keys string) {
	t.Helper()

	waitForOutput(t, run.out, title, 10*time.Second)

	_, err := run.ptmx.WriteString(keys)
	require.NoError(t, err, "send keys for %q", title)
}
