package cli

import (
	"strings"
	"testing"
)

// plain renders a diff without color, the way non-TTY output does.
func plain(old, new string, ctx int) string {
	return renderDiff(computeDiff(old, new, ctx), false)
}

func TestComputeDiffNoOp(t *testing.T) {
	if d := computeDiff("a\nb\nc\n", "a\nb\nc\n", 3); d != nil {
		t.Fatalf("expected nil diff for identical content, got %+v", d)
	}
	if out := plain("same", "same", 3); out != "" {
		t.Errorf("expected empty rendered diff for no-op, got %q", out)
	}
}

func TestComputeDiffAddition(t *testing.T) {
	out := plain("line1\nline2\n", "line1\nline2\nline3\n", 3)
	if !strings.Contains(out, "+line3") {
		t.Errorf("expected added line, got:\n%s", out)
	}
	// Unchanged lines appear as context with a leading space.
	if !strings.Contains(out, " line1") {
		t.Errorf("expected context line ' line1', got:\n%s", out)
	}
	// No deletions in a pure addition.
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "-") {
			t.Errorf("unexpected deletion line %q in pure addition:\n%s", l, out)
		}
	}
	if !strings.HasPrefix(out, "@@") {
		t.Errorf("expected a hunk header, got:\n%s", out)
	}
}

func TestComputeDiffDeletion(t *testing.T) {
	out := plain("a\nb\nc\n", "a\nc\n", 3)
	if !strings.Contains(out, "-b") {
		t.Errorf("expected deleted line '-b', got:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "+") {
			t.Errorf("unexpected addition %q in pure deletion:\n%s", l, out)
		}
	}
}

func TestComputeDiffReplacement(t *testing.T) {
	out := plain("a\nold\nc\n", "a\nnew\nc\n", 3)
	if !strings.Contains(out, "-old") || !strings.Contains(out, "+new") {
		t.Errorf("expected '-old' and '+new', got:\n%s", out)
	}
	if !strings.Contains(out, " a") || !strings.Contains(out, " c") {
		t.Errorf("expected surrounding context ' a' and ' c', got:\n%s", out)
	}
}

func TestComputeDiffContextLimits(t *testing.T) {
	// 20 identical lines with a single change in the middle: only a window of
	// context around the change should appear, not the whole file.
	var oldB, newB strings.Builder
	for i := 0; i < 20; i++ {
		oldB.WriteString("line\n")
		if i == 10 {
			newB.WriteString("CHANGED\n")
		} else {
			newB.WriteString("line\n")
		}
	}
	lines := computeDiff(oldB.String(), newB.String(), 2)
	context := 0
	for _, l := range lines {
		if l.kind == diffContext {
			context++
		}
	}
	// At most 2 lines of context on each side of the single change.
	if context > 4 {
		t.Errorf("expected <=4 context lines with ctx=2, got %d", context)
	}
	if context == 0 {
		t.Errorf("expected some context lines, got 0")
	}
}

func TestComputeDiffNewFile(t *testing.T) {
	out := plain("", "first\nsecond\n", 3)
	if !strings.Contains(out, "+first") || !strings.Contains(out, "+second") {
		t.Errorf("expected all-additions for new file, got:\n%s", out)
	}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(l, "-") {
			t.Errorf("new file should have no deletions, got %q", l)
		}
	}
	// New-file hunk header starts the old side at 0.
	if !strings.Contains(out, "@@ -0,0 +1,2 @@") {
		t.Errorf("expected new-file hunk header, got:\n%s", out)
	}
}

func TestRenderDiffNonTTYPlain(t *testing.T) {
	lines := computeDiff("a\nold\nc\n", "a\nnew\nc\n", 3)
	out := renderDiff(lines, false)
	if strings.Contains(out, "\033[") {
		t.Errorf("non-TTY (color=false) diff must be ANSI-free, got %q", out)
	}
	colored := renderDiff(lines, true)
	if !strings.Contains(colored, "\033[") {
		t.Errorf("color=true diff should contain ANSI codes, got %q", colored)
	}
}

func TestRenderDiff_colorMarkers(t *testing.T) {
	lines := computeDiff("a\nold\nc\n", "a\nnew\nc\n", 3)
	out := renderDiff(lines, true)

	if !strings.Contains(out, "\033[") {
		t.Fatalf("expected ANSI in colored diff, got %q", out)
	}
	// The leading +/- markers are bolded (ansiBold) then unbolded (ansiUnbold).
	if !strings.Contains(out, ansiBold) {
		t.Errorf("expected bold marker code %q in output, got %q", ansiBold, out)
	}
	// Additions carry green, deletions red.
	if !strings.Contains(out, ansiGreen) {
		t.Errorf("expected green add code %q, got %q", ansiGreen, out)
	}
	if !strings.Contains(out, ansiRed) {
		t.Errorf("expected red del code %q, got %q", ansiRed, out)
	}
	// The bold applies to the marker rune specifically: bold-on, '+', unbold.
	if !strings.Contains(out, ansiBold+"+"+ansiUnbold) {
		t.Errorf("expected bolded '+' marker (%s+%s), got %q", ansiBold, ansiUnbold, out)
	}
	if !strings.Contains(out, ansiBold+"-"+ansiUnbold) {
		t.Errorf("expected bolded '-' marker (%s-%s), got %q", ansiBold, ansiUnbold, out)
	}
}

func TestEmphasizeMarker_empty(t *testing.T) {
	// An empty line must not panic on the line[:1] slice.
	if got := emphasizeMarker("", ansiGreen); got != ansiGreen+ansiReset {
		t.Errorf("emphasizeMarker(\"\") = %q, want %q", got, ansiGreen+ansiReset)
	}
}
