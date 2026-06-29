package cli

import (
	"bytes"
	"io"

	"spaish/internal/agent"
)

// Raw key constants used by the REPL's terminal handling.
const (
	keyEsc = 0x1b // ESC, sent on its own when the user presses Escape

	// shiftTabSentinel is an otherwise-unused control rune. The stdin wrapper
	// rewrites the Shift-Tab escape sequence to this single byte so that it
	// survives readline's terminal layer (which silently drops CSI Z) and can
	// be intercepted in FuncFilterInputRune.
	shiftTabSentinel = 0x1e // RS (record separator); not bound by readline
)

// shiftTabSeq is the terminal escape sequence emitted for Shift-Tab: CSI Z,
// i.e. ESC [ Z.
var shiftTabSeq = []byte{keyEsc, '[', 'Z'}

// cycleMode returns the next execution mode in the manual -> auto -> plan ->
// manual rotation. Any unrecognised value resets to manual.
func cycleMode(current string) string {
	switch current {
	case agent.ModeManual:
		return agent.ModeAuto
	case agent.ModeAuto:
		return agent.ModePlan
	case agent.ModePlan:
		return agent.ModeManual
	default:
		return agent.ModeManual
	}
}

// shiftTabReader wraps an io.ReadCloser and rewrites the Shift-Tab escape
// sequence (CSI Z) into a single sentinel byte. Real terminals deliver an
// escape sequence atomically in one read, so a per-read replacement is
// reliable; a sequence split across reads simply isn't recognised (and is
// harmlessly ignored downstream) rather than corrupting other input.
type shiftTabReader struct {
	r io.ReadCloser
}

func newShiftTabReader(r io.ReadCloser) *shiftTabReader { return &shiftTabReader{r: r} }

func (s *shiftTabReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 && bytes.Contains(p[:n], shiftTabSeq) {
		out := bytes.ReplaceAll(p[:n], shiftTabSeq, []byte{shiftTabSentinel})
		n = copy(p, out)
	}
	return n, err
}

func (s *shiftTabReader) Close() error { return s.r.Close() }
