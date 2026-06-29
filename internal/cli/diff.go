package cli

import (
	"fmt"
	"strings"
)

// diffKind classifies a single line of a unified diff.
type diffKind int

const (
	diffContext diffKind = iota // unchanged context line (" " prefix)
	diffAdd                     // added line ("+" prefix)
	diffDel                     // removed line ("-" prefix)
	diffHunk                    // hunk header ("@@ ... @@")
)

// diffLine is one rendered line of a unified diff. Text already carries the
// leading marker (" ", "+", "-", or the "@@" header) so non-TTY output remains
// a valid, prefix-bearing diff.
type diffLine struct {
	kind diffKind
	text string
}

// opKind is an edit-script operation on a pair of line slices.
type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type op struct {
	kind opKind
	text string
}

// splitLines splits s into lines, dropping the trailing empty element produced
// by a final newline so that "a\nb\n" and "a\nb" both yield ["a", "b"]. An
// empty string yields no lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// diffOps computes a line-level edit script from a to b using a standard
// longest-common-subsequence dynamic program. The result is the ordered list of
// equal/delete/insert operations that transforms a into b.
func diffOps(a, b []string) []op {
	m, n := len(a), len(b)
	// dp[i][j] = LCS length of a[i:] and b[j:].
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEqual, a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{opDelete, a[i]})
			i++
		default:
			ops = append(ops, op{opInsert, b[j]})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, op{opDelete, a[i]})
	}
	for ; j < n; j++ {
		ops = append(ops, op{opInsert, b[j]})
	}
	return ops
}

// computeDiff returns the unified-diff lines transforming oldText into newText,
// keeping ctxLines of surrounding context around each change and collapsing
// longer unchanged runs into separate hunks. It is pure (no I/O) and returns nil
// when the two inputs are identical (a no-op edit). A brand-new file (empty
// oldText) renders as all-additions.
func computeDiff(oldText, newText string, ctxLines int) []diffLine {
	if ctxLines < 0 {
		ctxLines = 0
	}
	a := splitLines(oldText)
	b := splitLines(newText)
	ops := diffOps(a, b)

	changed := false
	for _, o := range ops {
		if o.kind != opEqual {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}

	// Annotate each op with its 1-based old/new line numbers.
	type lop struct {
		kind        opKind
		text        string
		oldLn, newLn int
	}
	lops := make([]lop, 0, len(ops))
	oln, nln := 1, 1
	for _, o := range ops {
		l := lop{kind: o.kind, text: o.text}
		switch o.kind {
		case opEqual:
			l.oldLn, l.newLn = oln, nln
			oln++
			nln++
		case opDelete:
			l.oldLn = oln
			oln++
		case opInsert:
			l.newLn = nln
			nln++
		}
		lops = append(lops, l)
	}

	// Mark which lines to emit: every change plus ctxLines on each side.
	total := len(lops)
	include := make([]bool, total)
	for i, l := range lops {
		if l.kind == opEqual {
			continue
		}
		lo, hi := i-ctxLines, i+ctxLines
		for k := lo; k <= hi; k++ {
			if k >= 0 && k < total {
				include[k] = true
			}
		}
	}

	var out []diffLine
	i := 0
	for i < total {
		if !include[i] {
			i++
			continue
		}
		start := i
		for i < total && include[i] {
			i++
		}
		end := i // exclusive

		var oStart, nStart, oCount, nCount int
		for k := start; k < end; k++ {
			switch lops[k].kind {
			case opEqual:
				if oStart == 0 {
					oStart = lops[k].oldLn
				}
				if nStart == 0 {
					nStart = lops[k].newLn
				}
				oCount++
				nCount++
			case opDelete:
				if oStart == 0 {
					oStart = lops[k].oldLn
				}
				oCount++
			case opInsert:
				if nStart == 0 {
					nStart = lops[k].newLn
				}
				nCount++
			}
		}
		out = append(out, diffLine{diffHunk, fmt.Sprintf("@@ -%d,%d +%d,%d @@", oStart, oCount, nStart, nCount)})
		for k := start; k < end; k++ {
			switch lops[k].kind {
			case opEqual:
				out = append(out, diffLine{diffContext, " " + lops[k].text})
			case opDelete:
				out = append(out, diffLine{diffDel, "-" + lops[k].text})
			case opInsert:
				out = append(out, diffLine{diffAdd, "+" + lops[k].text})
			}
		}
	}
	return out
}

// renderDiff formats diff lines into text. When color is true, additions are
// green, deletions red, and hunk headers cyan; otherwise the output is a plain,
// uncolored unified diff (used for non-TTY output). Returns "" for no lines.
func renderDiff(lines []diffLine, color bool) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for _, l := range lines {
		s := l.text
		if color {
			switch l.kind {
			case diffAdd:
				s = green(s)
			case diffDel:
				s = red(s)
			case diffHunk:
				s = cyan(s)
			}
		}
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
}
