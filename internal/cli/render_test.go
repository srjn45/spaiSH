package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"spaish/internal/protocol"
)

// captureStdout redirects os.Stdout for the duration of fn and returns whatever
// was written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = wr
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := rd.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()

	fn()
	wr.Close()
	out := <-done
	rd.Close()
	return out
}

// In tests stdout is a pipe, not a TTY, so the Renderer must emit plain text:
// no ANSI escape sequences and no glamour markdown decoration.
func TestRendererNonTTYIsPlain(t *testing.T) {
	out := captureStdout(t, func() {
		r := NewRenderer()
		if r.tty {
			t.Fatal("expected non-TTY renderer when stdout is a pipe")
		}
		if r.md != nil {
			t.Fatal("expected no glamour renderer for non-TTY output")
		}
		r.Render(protocol.Response{Type: "text", Content: "# Heading\n\nSome **bold** prose.\n"})
		r.Render(protocol.Response{Type: "text", Content: "▶ read_file foo.go\n"})
		r.Render(protocol.Response{Type: "output", Content: "file contents\n"})
		r.Render(protocol.Response{Type: "done"})
	})

	if strings.Contains(out, "\033[") {
		t.Errorf("non-TTY output contained ANSI escape codes: %q", out)
	}
	// Markdown source must pass through verbatim (no glamour reflow/decoration).
	if !strings.Contains(out, "# Heading") {
		t.Errorf("expected raw markdown heading in output, got: %q", out)
	}
	if !strings.Contains(out, "**bold**") {
		t.Errorf("expected raw bold markers in output, got: %q", out)
	}
	if !strings.Contains(out, "▶ read_file foo.go") {
		t.Errorf("expected tool-call line in output, got: %q", out)
	}
	if !strings.Contains(out, "file contents") {
		t.Errorf("expected tool output in output, got: %q", out)
	}
}

// Prose is buffered and only emitted at a block boundary (a ▶ tool-call line),
// not interleaved before it.
func TestRendererBuffersProseUntilBoundary(t *testing.T) {
	r := NewRenderer()
	out := captureStdout(t, func() {
		r.Render(protocol.Response{Type: "text", Content: "Let me "})
		r.Render(protocol.Response{Type: "text", Content: "look."})
	})
	if out != "" {
		t.Errorf("expected prose to stay buffered, got: %q", out)
	}

	out = captureStdout(t, func() {
		r.Render(protocol.Response{Type: "text", Content: "▶ glob *.go\n"})
	})
	// The buffered prose flushes before the tool-call line.
	if !strings.Contains(out, "Let me look.") {
		t.Errorf("expected buffered prose to flush, got: %q", out)
	}
	if !strings.Contains(out, "▶ glob *.go") {
		t.Errorf("expected tool-call line, got: %q", out)
	}
	if strings.Index(out, "Let me look.") > strings.Index(out, "▶ glob") {
		t.Errorf("prose should flush before the tool-call line, got: %q", out)
	}
}

// A final Flush emits any remaining buffered prose (e.g. the closing summary).
func TestRendererFlushEmitsRemainder(t *testing.T) {
	r := NewRenderer()
	out := captureStdout(t, func() {
		r.Render(protocol.Response{Type: "text", Content: "All done."})
		r.Flush()
	})
	if !strings.Contains(out, "All done.") {
		t.Errorf("expected flushed remainder, got: %q", out)
	}
}

// makeLines builds a newline-joined block of n numbered lines ("line 1\n…").
func makeLines(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(parts, "\n")
}

// Content at or under the cap is returned byte-for-byte unchanged, regardless of
// tty — capping must be a true no-op for the common case.
func TestCapOutputLinesUnderCapUnchanged(t *testing.T) {
	content := makeLines(10)
	if got := capOutputLines(content, true); got != content {
		t.Errorf("tty under-cap content should be unchanged, got: %q", got)
	}
	if got := capOutputLines(content, false); got != content {
		t.Errorf("non-tty under-cap content should be unchanged, got: %q", got)
	}
	// Exactly at the cap is still a no-op.
	exact := makeLines(maxDisplayLines)
	if got := capOutputLines(exact, true); got != exact {
		t.Errorf("at-cap content should be unchanged, got: %q", got)
	}
}

// Over the cap on a TTY, output keeps the first and last maxDisplayLines/2 lines
// with a truncation marker between them.
func TestCapOutputLinesOverCapTruncates(t *testing.T) {
	total := 100
	half := maxDisplayLines / 2
	got := capOutputLines(makeLines(total), true)
	lines := strings.Split(got, "\n")

	// half head + 1 marker + half tail.
	if want := 2*half + 1; len(lines) != want {
		t.Fatalf("expected %d lines, got %d: %q", want, len(lines), got)
	}
	if lines[0] != "line 1" {
		t.Errorf("expected first head line preserved, got: %q", lines[0])
	}
	if lines[half-1] != fmt.Sprintf("line %d", half) {
		t.Errorf("expected last head line %q, got: %q", fmt.Sprintf("line %d", half), lines[half-1])
	}
	if lines[len(lines)-1] != fmt.Sprintf("line %d", total) {
		t.Errorf("expected last tail line preserved, got: %q", lines[len(lines)-1])
	}
	marker := lines[half]
	if !strings.Contains(marker, "truncated") {
		t.Errorf("expected truncation marker, got: %q", marker)
	}
	if !strings.Contains(marker, fmt.Sprintf("%d", total-2*half)) {
		t.Errorf("expected marker to report %d truncated lines, got: %q", total-2*half, marker)
	}
	// On a TTY the marker is dim-styled.
	if !strings.Contains(marker, ansiDim) {
		t.Errorf("expected dim-styled marker on tty, got: %q", marker)
	}
}

// Non-tty (piped) output is never capped: a script consuming the pipe wants the
// full content and no injected marker line.
func TestCapOutputLinesNonTTYNeverCaps(t *testing.T) {
	content := makeLines(100)
	got := capOutputLines(content, false)
	if got != content {
		t.Errorf("non-tty output must be unchanged, got %d bytes vs %d", len(got), len(content))
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("non-tty output must not contain a truncation marker, got: %q", got)
	}
}

// Renderer.Render routes "output" responses through capOutputLines: with a TTY
// renderer a long tool result is truncated with a marker before printing.
func TestRendererRenderCapsOutputOnTTY(t *testing.T) {
	// Construct a TTY renderer directly (stdout is a pipe under test). md stays
	// nil, so no glamour is involved.
	r := &Renderer{tty: true}
	out := captureStdout(t, func() {
		r.Render(protocol.Response{Type: "output", Content: makeLines(100)})
	})
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker in rendered output, got: %q", out)
	}
	if !strings.Contains(out, "line 1") {
		t.Errorf("expected head lines in rendered output, got: %q", out)
	}
	if !strings.Contains(out, "line 100") {
		t.Errorf("expected tail lines in rendered output, got: %q", out)
	}
	if strings.Contains(out, "line 50") {
		t.Errorf("expected middle lines to be truncated, got: %q", out)
	}
}

// RenderResponse routes "output" through capOutputLines. Under test stdout is a
// pipe (non-TTY), so capping is skipped and the full content passes through
// verbatim — verifying both the wiring and the non-tty pass-through choice.
func TestRenderResponseOutputNonTTYPassesThrough(t *testing.T) {
	content := makeLines(100)
	out := captureStdout(t, func() {
		RenderResponse(protocol.Response{Type: "output", Content: content})
	})
	if strings.Contains(out, "truncated") {
		t.Errorf("non-tty RenderResponse must not truncate, got: %q", out)
	}
	if !strings.Contains(out, "line 1") || !strings.Contains(out, "line 50") || !strings.Contains(out, "line 100") {
		t.Errorf("expected full content to pass through, got: %q", out)
	}
}
