package looper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLastLineWriter_SingleWriteWithTrailingNewline(t *testing.T) {
	w := newLastLineWriter()
	n, err := w.Write([]byte("hello\nworld\n"))
	assert.NoError(t, err)
	assert.Equal(t, 12, n)
	assert.Equal(t, "world", w.LastLine(), "a line followed by its newline is already complete before Finalize")
}

func TestLastLineWriter_ChunkedWritesAcrossLineBoundaries(t *testing.T) {
	w := newLastLineWriter()
	chunks := []string{"{\"stat", "e\":\"comple", "ted\",\"error\":\"\"}\n"}
	for _, c := range chunks {
		n, err := w.Write([]byte(c))
		assert.NoError(t, err)
		assert.Equal(t, len(c), n)
	}
	assert.Equal(t, `{"state":"completed","error":""}`, w.LastLine())
}

func TestLastLineWriter_TrailingBlankLinesDoNotOverwrite(t *testing.T) {
	w := newLastLineWriter()
	_, err := w.Write([]byte("result\n\n\n   \n"))
	assert.NoError(t, err)
	assert.Equal(t, "result", w.LastLine())
}

func TestLastLineWriter_CRLFLineEndings(t *testing.T) {
	w := newLastLineWriter()
	_, err := w.Write([]byte("first\r\nsecond\r\n"))
	assert.NoError(t, err)
	assert.Equal(t, "second", w.LastLine())
}

func TestLastLineWriter_NoTrailingNewlineRequiresFinalize(t *testing.T) {
	w := newLastLineWriter()
	_, err := w.Write([]byte("no newline at all"))
	assert.NoError(t, err)
	assert.Empty(t, w.LastLine(), "an incomplete line must not be considered until Finalize")

	w.Finalize()
	assert.Equal(t, "no newline at all", w.LastLine())
}

func TestLastLineWriter_FencedResultLineIsFoundOverTheClosingFence(t *testing.T) {
	w := newLastLineWriter()
	_, err := w.Write([]byte("```json\n{\"state\":\"completed\",\"error\":\"\"}\n```\n"))
	assert.NoError(t, err)
	assert.Equal(t, `{"state":"completed","error":""}`, w.LastLine(), "the closing fence marker must not overwrite the JSON line before it")
}

func TestLastLineWriter_BareFenceIsSkipped(t *testing.T) {
	w := newLastLineWriter()
	_, err := w.Write([]byte("result\n```\n"))
	assert.NoError(t, err)
	assert.Equal(t, "result", w.LastLine())
}

func TestLastLineWriter_FenceWithInfoStringInsideAWordIsNotAFence(t *testing.T) {
	// A line that merely contains backticks somewhere is not a fence marker;
	// only a line that is *only* the marker (plus an alnum/_+- info string)
	// counts.
	w := newLastLineWriter()
	_, err := w.Write([]byte("see the ```code``` above\n"))
	assert.NoError(t, err)
	assert.Equal(t, "see the ```code``` above", w.LastLine())
}

func TestLastLineWriter_FinalizeWithNoPendingDataIsANoop(t *testing.T) {
	w := newLastLineWriter()
	_, err := w.Write([]byte("only\n"))
	assert.NoError(t, err)
	w.Finalize()
	w.Finalize()
	assert.Equal(t, "only", w.LastLine())
}

func TestLastLineWriter_ChunkedWritesDoNotGrowUnboundedAcrossManyLines(t *testing.T) {
	w := newLastLineWriter()
	for range 1000 {
		_, err := w.Write([]byte("line of output\n"))
		assert.NoError(t, err)
	}
	_, err := w.Write([]byte("final line\n"))
	assert.NoError(t, err)
	assert.Equal(t, "final line", w.LastLine())
	assert.Empty(t, w.partial, "no partial data should remain once every write ends on a newline")
}
