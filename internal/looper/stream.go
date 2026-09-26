package looper

import (
	"encoding/json"
	"strings"
)

// Keeps parseStreamLine referenced until the stream task path calls it;
// remove this line then.
var _ = parseStreamLine

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
