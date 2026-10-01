package looper

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var streamArgs = []string{"--print", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions"}

// runStreamFake execs the built fake claude directly in stream mode with the
// given extra environment and returns its stdout lines.
func runStreamFake(t *testing.T, env ...string) []string {
	t.Helper()

	cmd := exec.Command(filepath.Join(fakeClaudeDir, "claude"), streamArgs...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader("prompt\n")
	out, err := cmd.Output()
	require.NoError(t, err)

	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

func TestFakeClaude_StreamJSON(t *testing.T) {
	lines := runStreamFake(t)
	require.NotEmpty(t, lines)

	for _, line := range lines {
		var event map[string]any
		assert.NoError(t, json.Unmarshal([]byte(line), &event), "line %q", line)
	}

	var last struct {
		Type   string `json:"type"`
		Result string `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &last))
	assert.Equal(t, "result", last.Type)
	assert.Equal(t, `{"state":"completed","error":""}`, last.Result)
}

func TestFakeClaude_StreamJSONBigEvent(t *testing.T) {
	lines := runStreamFake(t, "FAKE_CLAUDE_BIG_EVENT=200000")

	longest := 0
	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "line %q", line)
		longest = max(longest, len(line))
	}
	assert.GreaterOrEqual(t, longest, 200000)
}

func TestFakeClaude_RecordsArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	runStreamFake(t, "FAKE_CLAUDE_ARGS="+argsFile)

	data, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, strings.Join(streamArgs, " ")+"\n", string(data))
}
