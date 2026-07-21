package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Fuzzy matcher ---------------------------------------------------------

func TestFuzzyMatch_subsequence(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		cand    string
		want    bool
	}{
		{"scattered subsequence", "rndr", "internal/cli/render.go", true},
		{"not a subsequence", "xyz", "render.go", false},
		{"case-insensitive", "RENDER", "render.go", true},
		{"empty matches all", "", "anything/at/all.go", true},
		{"exact filename", "render.go", "internal/cli/render.go", true},
		{"out of order fails", "gorrender", "render.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := fuzzyMatch(tt.pattern, tt.cand)
			if ok != tt.want {
				t.Errorf("fuzzyMatch(%q, %q) ok = %v, want %v", tt.pattern, tt.cand, ok, tt.want)
			}
		})
	}
}

func TestFuzzyMatch_ranking(t *testing.T) {
	// A filename-start, consecutive match should outrank a scattered mid-word one
	// (each matched rune mid-token, separated by gaps).
	start, ok1 := fuzzyMatch("render", "internal/cli/render.go")
	scattered, ok2 := fuzzyMatch("render", "xrxexnxdxexr.go")
	if !ok1 || !ok2 {
		t.Fatalf("expected both to match, got %v and %v", ok1, ok2)
	}
	if start <= scattered {
		t.Errorf("filename-start match (%d) should outrank scattered match (%d)", start, scattered)
	}

	// A boundary match (after '/') should outrank the same run mid-token.
	boundary, _ := fuzzyMatch("cli", "internal/cli.go")
	mid, _ := fuzzyMatch("cli", "xxcliyy.go")
	if boundary <= mid {
		t.Errorf("boundary match (%d) should outrank mid-token match (%d)", boundary, mid)
	}
}

func TestFuzzyFileCompletions_insertion(t *testing.T) {
	c := &fuzzyCompleter{
		files: []string{
			"internal/cli/render.go",
			"internal/cli/repl.go",
			"internal/cli/",
			"README.md",
		},
	}
	// once has not fired but files is pre-populated; snapshot() must return it.
	frag := "rndr"
	cands, length := c.fuzzyFileCompletions(frag)
	if length != len([]rune(frag)) {
		t.Errorf("length = %d, want rune count of frag %d", length, len([]rune(frag)))
	}
	if len(cands) == 0 {
		t.Fatal("expected at least one candidate")
	}
	// Candidates are full relative paths (not suffixes), so the top one matches.
	top := string(cands[0])
	if top != "internal/cli/render.go" {
		t.Errorf("expected full-path candidate 'internal/cli/render.go' first, got %q", top)
	}

	// A directory candidate keeps its trailing slash.
	dirCands, _ := c.fuzzyFileCompletions("internalcli")
	var sawDir bool
	for _, cd := range dirCands {
		if strings.HasSuffix(string(cd), "/") {
			sawDir = true
		}
	}
	if !sawDir {
		t.Errorf("expected a directory candidate ending in '/', got %v", runesToStrings(dirCands))
	}
}

func TestFuzzyFileCompletions_capped(t *testing.T) {
	files := make([]string, maxFuzzyResults+25)
	for i := range files {
		files[i] = filepath.ToSlash(filepath.Join("dir", "file"+string(rune('a'+i%26))+".go"))
	}
	c := &fuzzyCompleter{files: files}
	cands, _ := c.fuzzyFileCompletions("") // empty pattern matches everything
	if len(cands) > maxFuzzyResults {
		t.Errorf("expected at most %d candidates, got %d", maxFuzzyResults, len(cands))
	}
}

func TestGatherWorkingTree_bounds(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "node_modules", "left-pad"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "internal", "cli"), 0o755))
	must(os.WriteFile(filepath.Join(root, ".git", "config"), []byte("x"), 0o644))
	must(os.WriteFile(filepath.Join(root, "node_modules", "left-pad", "index.js"), []byte("x"), 0o644))
	must(os.WriteFile(filepath.Join(root, "internal", "cli", "repl.go"), []byte("x"), 0o644))
	must(os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o644))

	got := gatherWorkingTree(root)
	for _, p := range got {
		if strings.HasPrefix(p, ".git/") || p == ".git/" {
			t.Errorf(".git must be excluded, found %q", p)
		}
		if strings.HasPrefix(p, "node_modules/") || p == "node_modules/" {
			t.Errorf("node_modules must be excluded, found %q", p)
		}
		if strings.Contains(p, `\`) {
			t.Errorf("paths must be slash-separated, found %q", p)
		}
	}
	// The real files under an allowed dir are present.
	if !contains(got, "internal/cli/repl.go") {
		t.Errorf("expected internal/cli/repl.go in snapshot, got %v", got)
	}
	if !contains(got, "README.md") {
		t.Errorf("expected README.md in snapshot, got %v", got)
	}
	// Directories are included with a trailing slash.
	if !contains(got, "internal/") {
		t.Errorf("expected directory candidate 'internal/', got %v", got)
	}
}

// --- Multiline assembly ----------------------------------------------------

// feeder returns a next() closure yielding the given lines in order, then an EOF
// (ok=false, empty line). It records how many times it was called.
func feeder(lines ...string) (func() (string, bool), *int) {
	i, calls := 0, 0
	next := func() (string, bool) {
		calls++
		if i >= len(lines) {
			return "", false // EOF
		}
		l := lines[i]
		i++
		return l, true
	}
	return next, &calls
}

func TestAssembleMultiline_fence(t *testing.T) {
	next, _ := feeder("a", "b", fenceMarker)
	msg, aborted := assembleMultiline(fenceMarker, next)
	if aborted {
		t.Fatal("fence block should not be aborted")
	}
	if msg != "a\nb" {
		t.Errorf("msg = %q, want %q", msg, "a\nb")
	}
}

func TestAssembleMultiline_continuation(t *testing.T) {
	next, _ := feeder(`b\`, "c")
	msg, aborted := assembleMultiline(`a\`, next)
	if aborted {
		t.Fatal("continuation should not be aborted")
	}
	if msg != "a\nb\nc" {
		t.Errorf("msg = %q, want %q", msg, "a\nb\nc")
	}
}

func TestAssembleMultiline_escapedBackslashDoesNotContinue(t *testing.T) {
	// A first line ending in an escaped "\\" is a single line: next is untouched.
	next, calls := feeder("should-not-be-read")
	msg, aborted := assembleMultiline(`path\\`, next)
	if aborted {
		t.Fatal("escaped backslash line should not be aborted")
	}
	if *calls != 0 {
		t.Errorf("next should not be called for a non-continued first line, called %d times", *calls)
	}
	if msg != `path\\` {
		t.Errorf("msg = %q, want verbatim %q", msg, `path\\`)
	}

	// Within a continuation, an escaped backslash stops it and collapses to one.
	next2, _ := feeder(`b\\`, "unreached")
	msg2, _ := assembleMultiline(`a\`, next2)
	if msg2 != `a`+"\n"+`b\` {
		t.Errorf("msg = %q, want %q", msg2, `a`+"\n"+`b\`)
	}
}

func TestAssembleMultiline_singleLine(t *testing.T) {
	next := func() (string, bool) {
		t.Fatal("next must not be called for a plain single line")
		return "", false
	}
	msg, aborted := assembleMultiline("just a line", next)
	if aborted || msg != "just a line" {
		t.Errorf("got (%q, %v), want (%q, false)", msg, aborted, "just a line")
	}
}

func TestAssembleMultiline_abortAndEOF(t *testing.T) {
	// Interrupt mid-fence -> aborted, empty msg.
	abortNext := func() (string, bool) { return multilineAbort, false }
	msg, aborted := assembleMultiline(fenceMarker, abortNext)
	if !aborted || msg != "" {
		t.Errorf("interrupt: got (%q, %v), want (\"\", true)", msg, aborted)
	}

	// EOF mid-fence -> submit what accumulated.
	next, _ := feeder("x") // one line then EOF, no closing fence
	msg, aborted = assembleMultiline(fenceMarker, next)
	if aborted || msg != "x" {
		t.Errorf("eof: got (%q, %v), want (%q, false)", msg, aborted, "x")
	}

	// Interrupt mid-continuation discards it too.
	msg, aborted = assembleMultiline(`a\`, abortNext)
	if !aborted || msg != "" {
		t.Errorf("interrupt continuation: got (%q, %v), want (\"\", true)", msg, aborted)
	}
}

// --- helpers ---------------------------------------------------------------

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func runesToStrings(rs [][]rune) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}
