package looper

import (
	"encoding/json"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// resultLine is the JSON object the shared prompt asks a claude session to
// end its stdout with, on its own last non-blank line.
type resultLine struct {
	State string `json:"state"`
	Error string `json:"error"`
}

// parseResult parses line as the session's result object. ok is true only
// when line is valid JSON, decodes to an object (not an array, string, or
// number), and its state field is "completed" or "failed" once trimmed and
// lowercased. Unknown fields are ignored. When ok is false, state and
// errMsg are the zero value and must not be used.
func parseResult(line string) (state, errMsg string, ok bool) {
	var r resultLine
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return "", "", false
	}

	state = strings.ToLower(strings.TrimSpace(r.State))
	if state != tasks.CompletedState && state != tasks.FailedState {
		return "", "", false
	}

	return state, strings.TrimSpace(r.Error), true
}

// lastResultLine returns the last non-blank, non-fence-marker line of
// output, trimmed of surrounding whitespace. It is meant to find a claude
// session's trailing JSON result line even when that line is wrapped in a
// Markdown code fence.
func lastResultLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return ""
}

// outcome resolves the state and error message to persist for a task run,
// given the exec error (nil on a zero exit) and the last non-blank line of
// the session's stdout. Cancellation is the caller's responsibility: outcome
// must not be called when the task run was cancelled.
func outcome(runErr error, lastLine string) (state, errMsg string) {
	resState, resErr, ok := parseResult(lastLine)

	if runErr == nil {
		switch {
		case ok && resState == tasks.CompletedState:
			return tasks.CompletedState, ""
		case ok && resState == tasks.FailedState:
			if resErr == "" {
				resErr = "session reported failed with no error"
			}
			return tasks.FailedState, resErr
		default:
			return tasks.FailedState, "no valid result line in session output"
		}
	}

	if ok && resErr != "" {
		return tasks.FailedState, resErr
	}
	return tasks.FailedState, runErr.Error()
}
