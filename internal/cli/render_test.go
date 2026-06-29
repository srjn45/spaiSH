package cli

import (
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
