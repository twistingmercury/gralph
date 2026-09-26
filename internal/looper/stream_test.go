package looper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseStreamLine(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		wantActivity []string
		wantResult   string
		wantIsResult bool
	}{
		{
			name:         "text block single line",
			line:         `{"type":"assistant","message":{"content":[{"type":"text","text":"Reading the spec."}]}}`,
			wantActivity: []string{"Reading the spec."},
		},
		{
			name:         "text block multi-line skips blank lines",
			line:         `{"type":"assistant","message":{"content":[{"type":"text","text":"First line.\n\n   \nSecond line.\n"}]}}`,
			wantActivity: []string{"First line.", "Second line."},
		},
		{
			name:         "Bash tool uses command",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"make test"}}]}}`,
			wantActivity: []string{"→ Bash make test"},
		},
		{
			name:         "multi-line Bash command is cut to its first line",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"cat <<EOF > f\nhello\nEOF"}}]}}`,
			wantActivity: []string{"→ Bash cat <<EOF > f"},
		},
		{
			name:         "Read tool uses file_path",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/repo/a.go"}}]}}`,
			wantActivity: []string{"→ Read /repo/a.go"},
		},
		{
			name:         "Edit tool uses file_path",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/repo/b.go","old_string":"x"}}]}}`,
			wantActivity: []string{"→ Edit /repo/b.go"},
		},
		{
			name:         "Write tool uses file_path",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/repo/c.go","content":"y"}}]}}`,
			wantActivity: []string{"→ Write /repo/c.go"},
		},
		{
			name:         "Grep tool uses pattern",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"func main"}}]}}`,
			wantActivity: []string{"→ Grep func main"},
		},
		{
			name:         "Glob tool uses pattern",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Glob","input":{"pattern":"**/*.go"}}]}}`,
			wantActivity: []string{"→ Glob **/*.go"},
		},
		{
			name:         "unknown tool has no target and no trailing space",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TodoWrite","input":{"command":"ignored","todos":[]}}]}}`,
			wantActivity: []string{"→ TodoWrite"},
		},
		{
			name:         "known tool with missing target has no trailing space",
			line:         `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{}}]}}`,
			wantActivity: []string{"→ Bash"},
		},
		{
			name:         "mixed blocks keep order",
			line:         `{"type":"assistant","message":{"content":[{"type":"text","text":"Checking."},{"type":"tool_use","name":"Read","input":{"file_path":"x.go"}},{"type":"thinking","thinking":"hmm"}]}}`,
			wantActivity: []string{"Checking.", "→ Read x.go"},
		},
		{
			name:         "result event",
			line:         `{"type":"result","subtype":"success","is_error":false,"result":"Done.\n{\"state\": \"completed\", \"error\": \"\"}"}`,
			wantResult:   "Done.\n{\"state\": \"completed\", \"error\": \"\"}",
			wantIsResult: true,
		},
		{
			name: "system event is ignored",
			line: `{"type":"system","subtype":"init","session_id":"abc"}`,
		},
		{
			name: "user event is ignored",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","content":"file body"}]}}`,
		},
		{
			name: "rate_limit_event is ignored",
			line: `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`,
		},
		{
			name: "unknown event type is ignored",
			line: `{"type":"something_new","message":{"content":[{"type":"text","text":"hi"}]}}`,
		},
		{
			name: "invalid json is ignored",
			line: `{"type":"assistant",`,
		},
		{
			name: "empty line is ignored",
			line: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity, result, isResult := parseStreamLine([]byte(tt.line))
			assert.Equal(t, tt.wantActivity, activity)
			assert.Equal(t, tt.wantResult, result)
			assert.Equal(t, tt.wantIsResult, isResult)
		})
	}
}
