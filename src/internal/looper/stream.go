package looper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// streamEvent holds the fields gralph uses from one stream-json event.
type streamEvent struct {
	Type    string `json:"type"`
	Result  string `json:"result"`
	Message struct {
		Content []struct {
			Type  string    `json:"type"`
			Text  string    `json:"text"`
			Name  string    `json:"name"`
			Input toolInput `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// toolInput holds the tool_use input fields gralph shows as a tool's target.
type toolInput struct {
	Command  string `json:"command"`
	FilePath string `json:"file_path"`
	Pattern  string `json:"pattern"`
}

// parseStreamLine turns one line of claude's stream-json output into
// activity lines for display. For a result event, isResult is true and
// result is the event's result text. Lines that fail to decode and event
// types other than assistant and result give nothing.
func parseStreamLine(line []byte) (activity []string, result string, isResult bool) {
	var ev streamEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil, "", false
	}

	switch ev.Type {
	case "result":
		return nil, ev.Result, true
	case "assistant":
		for _, block := range ev.Message.Content {
			switch block.Type {
			case "text":
				textLines := textActivity(block.Text)
				activity = append(activity, textLines...)
			case "tool_use":
				toolLine := toolActivity(block.Name, block.Input)
				activity = append(activity, toolLine)
			}
		}
	}
	return activity, "", false
}

// textActivity returns text's non-blank lines.
func textActivity(text string) []string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// toolActivity returns the "→ <tool> <target>" line for a tool_use block.
// Only the target's first line is kept so a multi-line command stays one
// activity line.
func toolActivity(name string, input toolInput) string {
	var target string
	switch name {
	case "Bash":
		target = input.Command
	case "Read", "Edit", "Write":
		target = input.FilePath
	case "Grep", "Glob":
		target = input.Pattern
	}
	target = firstLine(target)
	if target == "" {
		return "→ " + name
	}

	return "→ " + name + " " + target
}

// runSession runs the task's stream session and reports how it ended, so a
// listener learns the session's own outcome before any gate changes the
// task's. A cancelled session reports nothing: it has no outcome.
func runSession(ctx context.Context, p string, task tasks.Task, sessionArgs []string, report func(Event)) (state, errMsg string, err error) {
	start := time.Now()
	state, errMsg, err = runTaskStream(ctx, p, task, sessionArgs, report)
	if err != nil {
		return state, errMsg, err
	}

	task.State, task.Error = state, errMsg
	elapsed := time.Since(start)
	report(Event{Kind: SessionFinished, Task: task, Duration: elapsed})
	return state, errMsg, nil
}

// runTaskStream runs one task with claude's stream-json output, sending each
// activity line and stderr line to report as an Activity event and writing
// nothing to gralph's own stdout or stderr. The outcome comes from the
// result event's text. It returns the task's outcome, or an error when ctx
// was cancelled.
func runTaskStream(ctx context.Context, p string, task tasks.Task, sessionArgs []string, report func(Event)) (state, errMsg string, err error) {
	cmd, _ := claudeCmd(ctx, p, task, sessionArgs, true)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("task %d: %s: %w", task.ID, task.Name, err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("task %d: %s: %w", task.ID, task.Name, err)
	}

	if err = cmd.Start(); err != nil {
		return finishTask(ctx, task, err, "")
	}

	st := &streamTask{task: task, report: report}
	stderrDone := make(chan struct{})
	go st.readStderr(stderr, stderrDone)
	readLines(stdout, st.stdoutLine)
	<-stderrDone

	waitErr := cmd.Wait()

	return finishTask(ctx, task, waitErr, st.resultText)
}

// streamTask carries what runTaskStream's line callbacks share, so they can
// be methods rather than closures over its locals.
type streamTask struct {
	task       tasks.Task
	report     func(Event)
	resultText string
}

// readStderr closes done once stderr is drained, so cmd.Wait is not called
// while the pipe still has unread lines.
func (st *streamTask) readStderr(stderr io.Reader, done chan<- struct{}) {
	defer close(done)
	readLines(stderr, st.rawLine)
}

func (st *streamTask) rawLine(line []byte) {
	st.report(Event{Kind: Activity, Task: st.task, Line: strings.TrimRight(string(line), "\r\n")})
}

func (st *streamTask) stdoutLine(line []byte) {
	activity, result, isResult := parseStreamLine(line)
	for _, a := range activity {
		st.report(Event{Kind: Activity, Task: st.task, Line: a})
	}
	if isResult {
		st.resultText = result
	}
}

// readLines exists because bufio.Scanner stops at a 64 KiB token, and one
// stream-json event (a tool result holding a whole file, say) can exceed that,
// which would silently stop reading claude's output. ReadBytes has no line
// limit, and a final line with no trailing newline still reaches fn.
func readLines(r io.Reader, fn func(line []byte)) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			fn(line)
		}

		if err != nil {
			return
		}
	}
}
