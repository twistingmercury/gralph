package looper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/twistingmercury/gralph/internal/tasks"
)

// streamEvent holds the fields gralph uses from one stream-json event.
type streamEvent struct {
	Type    string `json:"type"`
	Result  string `json:"result"`
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Name  string `json:"name"`
			Input struct {
				Command  string `json:"command"`
				FilePath string `json:"file_path"`
				Pattern  string `json:"pattern"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
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
				for _, l := range strings.Split(block.Text, "\n") {
					if strings.TrimSpace(l) != "" {
						activity = append(activity, l)
					}
				}
			case "tool_use":
				var target string
				switch block.Name {
				case "Bash":
					target = block.Input.Command
				case "Read", "Edit", "Write":
					target = block.Input.FilePath
				case "Grep", "Glob":
					target = block.Input.Pattern
				}
				target, _, _ = strings.Cut(target, "\n")
				if target == "" {
					activity = append(activity, "→ "+block.Name)
				} else {
					activity = append(activity, "→ "+block.Name+" "+target)
				}
			}
		}
	}
	return activity, "", false
}

// runTaskStream runs one task with claude's stream-json output, sending each
// activity line and stderr line to report as an Activity event and writing
// nothing to gralph's own stdout or stderr. The outcome comes from the
// result event's text. It returns the task's outcome, or an error when ctx
// was cancelled.
func runTaskStream(ctx context.Context, p string, task tasks.Task, report func(Event)) (state, errMsg string, err error) {
	const claude = "claude"
	const print = "--print"
	const outputFormat = "--output-format"
	const streamJSON = "stream-json"
	const verbose = "--verbose"
	const skipPermissions = "--dangerously-skip-permissions"

	prompt := fmt.Sprintf("%s\n\n%s\n", p, task.String())

	cmd := exec.CommandContext(ctx, claude, print, outputFormat, streamJSON, verbose, skipPermissions)
	configureProcessTree(cmd)
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("task %d: %s: %w", task.ID, task.Name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("task %d: %s: %w", task.ID, task.Name, err)
	}

	var resultText string
	runErr := cmd.Start()
	if runErr == nil {
		stderrDone := make(chan struct{})
		go func() {
			defer close(stderrDone)
			readLines(stderr, func(line []byte) {
				report(Event{Kind: Activity, Task: task, Line: strings.TrimRight(string(line), "\r\n")})
			})
		}()

		readLines(stdout, func(line []byte) {
			activity, result, isResult := parseStreamLine(line)
			for _, a := range activity {
				report(Event{Kind: Activity, Task: task, Line: a})
			}
			if isResult {
				resultText = result
			}
		})

		<-stderrDone
		runErr = cmd.Wait()
	}

	if runErr != nil && ctx.Err() != nil {
		// Cancelled by SIGINT/SIGTERM: leave the task's state and the
		// tasks file untouched so a re-run picks up where it left off.
		return "", "", fmt.Errorf("task %d: %s failed: %w", task.ID, task.Name, runErr)
	}

	state, errMsg = outcome(runErr, lastResultLine(resultText))
	return state, errMsg, nil
}

// readLines calls fn with each line read from r, including a final line with
// no trailing newline, until r is exhausted. Lines have no length limit.
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
