package looper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/twistingmercury/gralph/internal/tasks"
)

func TestParseResult(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantState string
		wantErr   string
		wantOK    bool
	}{
		{
			name:      "valid completed",
			line:      `{"state":"completed","error":""}`,
			wantState: tasks.CompletedState,
			wantOK:    true,
		},
		{
			name:      "valid failed with error",
			line:      `{"state":"failed","error":"tests did not pass"}`,
			wantState: tasks.FailedState,
			wantErr:   "tests did not pass",
			wantOK:    true,
		},
		{
			name:      "uppercase and surrounding whitespace state is normalized",
			line:      `{"state":"  COMPLETED  ","error":""}`,
			wantState: tasks.CompletedState,
			wantOK:    true,
		},
		{
			name:      "error field is trimmed",
			line:      `{"state":"failed","error":"  boom  "}`,
			wantState: tasks.FailedState,
			wantErr:   "boom",
			wantOK:    true,
		},
		{
			name:      "unknown fields are ignored",
			line:      `{"state":"completed","error":"","extra":"field","nested":{"a":1}}`,
			wantState: tasks.CompletedState,
			wantOK:    true,
		},
		{
			name:   "invalid json",
			line:   `{state: completed}`,
			wantOK: false,
		},
		{
			name:   "json array is not an object",
			line:   `["completed",""]`,
			wantOK: false,
		},
		{
			name:   "json string is not an object",
			line:   `"completed"`,
			wantOK: false,
		},
		{
			name:   "json number is not an object",
			line:   `42`,
			wantOK: false,
		},
		{
			name:   "unknown state",
			line:   `{"state":"in_progress","error":""}`,
			wantOK: false,
		},
		{
			name:   "missing state",
			line:   `{"error":"boom"}`,
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, errMsg, ok := parseResult(tt.line)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantState, state)
				assert.Equal(t, tt.wantErr, errMsg)
			}
		})
	}
}

func TestLastResultLine(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "plain last line",
			output: "first\nsecond\n",
			want:   "second",
		},
		{
			name:   "trailing blank lines are skipped",
			output: "result\n\n\n   \n",
			want:   "result",
		},
		{
			name:   "crlf line endings",
			output: "first\r\nsecond\r\n",
			want:   "second",
		},
		{
			name:   "closing fence is skipped",
			output: "```json\n{\"state\":\"completed\",\"error\":\"\"}\n```\n",
			want:   `{"state":"completed","error":""}`,
		},
		{
			name:   "no trailing newline",
			output: "no newline at all",
			want:   "no newline at all",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lastResultLine(tt.output))
		})
	}
}

func TestOutcome(t *testing.T) {
	exitErr := errors.New("exit status 1")

	tests := []struct {
		name       string
		runErr     error
		lastLine   string
		wantState  string
		wantErrMsg string
	}{
		{
			name:       "zero exit, completed result",
			lastLine:   `{"state":"completed","error":""}`,
			wantState:  tasks.CompletedState,
			wantErrMsg: "",
		},
		{
			name:       "zero exit, failed result with error",
			lastLine:   `{"state":"failed","error":"lint failed"}`,
			wantState:  tasks.FailedState,
			wantErrMsg: "lint failed",
		},
		{
			name:       "zero exit, failed result with blank error",
			lastLine:   `{"state":"failed","error":""}`,
			wantState:  tasks.FailedState,
			wantErrMsg: "session reported failed with no error",
		},
		{
			name:       "zero exit, missing result line",
			lastLine:   "",
			wantState:  tasks.FailedState,
			wantErrMsg: "no valid result line in session output",
		},
		{
			name:       "zero exit, invalid result line",
			lastLine:   "not json",
			wantState:  tasks.FailedState,
			wantErrMsg: "no valid result line in session output",
		},
		{
			name:       "non-zero exit, valid result with error",
			runErr:     exitErr,
			lastLine:   `{"state":"failed","error":"tests failed: 3 cases"}`,
			wantState:  tasks.FailedState,
			wantErrMsg: "tests failed: 3 cases",
		},
		{
			name:       "non-zero exit, valid completed result with error still wins",
			runErr:     exitErr,
			lastLine:   `{"state":"completed","error":"but actually not"}`,
			wantState:  tasks.FailedState,
			wantErrMsg: "but actually not",
		},
		{
			name:       "non-zero exit, valid result with blank error falls back to runErr",
			runErr:     exitErr,
			lastLine:   `{"state":"failed","error":""}`,
			wantState:  tasks.FailedState,
			wantErrMsg: "exit status 1",
		},
		{
			name:       "non-zero exit, no result line falls back to runErr",
			runErr:     exitErr,
			lastLine:   "",
			wantState:  tasks.FailedState,
			wantErrMsg: "exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, errMsg := outcome(tt.runErr, tt.lastLine)
			assert.Equal(t, tt.wantState, state)
			assert.Equal(t, tt.wantErrMsg, errMsg)
		})
	}
}
