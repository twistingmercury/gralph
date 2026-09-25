package looper

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
)

// fenceLineRegexp matches a line that is only a Markdown code fence marker,
// with an optional info string (e.g. "```json"), and nothing else.
var fenceLineRegexp = regexp.MustCompile("^```[[:alnum:]_+-]*$")

// lastLineWriter is an io.Writer that retains only the last non-blank,
// non-fence-marker line written to it. It is meant to be combined with
// os.Stdout via io.MultiWriter so a claude session's output keeps streaming
// to the terminal while gralph also recovers the final JSON result line.
//
// Memory use does not grow with the volume of output: only the current
// incomplete line and the last complete non-blank line seen so far are
// retained.
type lastLineWriter struct {
	mu       sync.Mutex
	partial  []byte
	lastLine string
}

func newLastLineWriter() *lastLineWriter {
	return &lastLineWriter{}
}

// Write implements io.Writer. It always accepts the full input and never
// returns an error: it exists to observe output, not to gate it.
func (w *lastLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.partial = append(w.partial, p...)
	for {
		idx := bytes.IndexByte(w.partial, '\n')
		if idx < 0 {
			break
		}
		w.consider(w.partial[:idx])

		rest := w.partial[idx+1:]
		remaining := make([]byte, len(rest))
		copy(remaining, rest)
		w.partial = remaining
	}

	return len(p), nil
}

// Finalize considers any buffered partial line as complete. Without this, a
// process whose output does not end with a trailing newline would never
// have its final line considered.
func (w *lastLineWriter) Finalize() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.partial) > 0 {
		w.consider(w.partial)
		w.partial = nil
	}
}

// LastLine returns the last non-blank, non-fence-marker line seen so far.
func (w *lastLineWriter) LastLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastLine
}

// consider updates lastLine from a single line's raw bytes, excluding the
// terminating newline. Blank lines and lines that are only a Markdown code
// fence marker are ignored, so a fenced result line can still be found on
// the line before the closing fence. The caller must hold w.mu.
func (w *lastLineWriter) consider(line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || fenceLineRegexp.MatchString(trimmed) {
		return
	}
	w.lastLine = trimmed
}
