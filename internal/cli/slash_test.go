package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/pricing"
	"spaish/internal/session"
)

// suffixes flattens a [][]rune completion result into strings for comparison.
func suffixes(rs [][]rune) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	sort.Strings(out)
	return out
}

func TestAtPathCompletions(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"))
	mustWrite(t, filepath.Join(dir, "makefile"))
	mustWrite(t, filepath.Join(dir, "README.md"))
	mustWrite(t, filepath.Join(dir, ".hidden"))
	if err := os.Mkdir(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "internal", "app.go"))

	tests := []struct {
		name       string
		frag       string
		want       []string
		wantOffset int
	}{
		{"empty lists visible entries", "", []string{"README.md", "internal/", "main.go", "makefile"}, 0},
		{"prefix match", "ma", []string{"in.go", "kefile"}, 2},
		{"directory gets trailing slash", "int", []string{"ernal/"}, 3},
		{"case sensitive", "r", nil, 1},
		{"into subdir", "internal/", []string{"app.go"}, 0},
		{"subdir prefix", "internal/a", []string{"pp.go"}, 1},
		{"dotfiles hidden unless dot typed", ".", []string{"hidden"}, 1},
		{"no match", "zzz", nil, 3},
		{"missing dir is graceful", "nope/deeper", nil, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, off := atPathCompletions(dir, tc.frag)
			if off != tc.wantOffset {
				t.Errorf("offset = %d, want %d", off, tc.wantOffset)
			}
			if g := suffixes(got); !equalStrings(g, tc.want) {
				t.Errorf("suffixes = %v, want %v", g, tc.want)
			}
		})
	}
}

func TestReplCompleterRouting(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "notes.txt"))
	c := newCompleter(dir)

	// @-path completion fires on a word starting with '@', anywhere in the line.
	got, off := c.Do([]rune("explain @not"), len("explain @not"))
	if off != 3 || !equalStrings(suffixes(got), []string{"es.txt"}) {
		t.Errorf("@-path route: got %v off=%d", suffixes(got), off)
	}

	// Slash completion fires only when the line begins with '/'.
	if got, _ := c.Do([]rune("/he"), 3); len(got) == 0 {
		t.Errorf("slash route returned no candidates for /he")
	}

	// Plain prose gets no completion.
	if got, _ := c.Do([]rune("hello world"), 11); len(got) != 0 {
		t.Errorf("prose should yield no completion, got %v", suffixes(got))
	}

	// A '/' that is not at line start is not a slash command.
	if got, _ := c.Do([]rune("what is /he"), 11); len(got) != 0 {
		t.Errorf("mid-line slash should not complete, got %v", suffixes(got))
	}
}

// TestHelpStaysInSync guards against /help drifting from the actual command set:
// every command handled by handleSlash (bar known aliases) must appear both in
// helpText and in commandDetails, and every commandDetails key must be a real
// command.
func TestHelpStaysInSync(t *testing.T) {
	src, err := os.ReadFile("slash.go")
	if err != nil {
		t.Fatalf("read slash.go: %v", err)
	}
	handled := handledCommands(t, string(src))
	aliases := map[string]bool{"/exit": true, "/q": true}

	for cmd := range handled {
		if aliases[cmd] {
			continue
		}
		if !strings.Contains(helpText, cmd) {
			t.Errorf("command %q handled but missing from helpText", cmd)
		}
		if _, ok := commandDetails[cmd]; !ok {
			t.Errorf("command %q handled but missing from commandDetails", cmd)
		}
	}
	for cmd := range commandDetails {
		if !handled[cmd] {
			t.Errorf("commandDetails has %q, which handleSlash does not handle", cmd)
		}
	}
}

// handledCommands extracts the slash commands from handleSlash's switch cases.
func handledCommands(t *testing.T, src string) map[string]bool {
	t.Helper()
	start := strings.Index(src, "func (r *REPL) handleSlash")
	if start < 0 {
		t.Fatal("could not locate handleSlash in source")
	}
	body := src[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	re := regexp.MustCompile(`case\s+((?:"/[^"]+"(?:,\s*)?)+):`)
	tok := regexp.MustCompile(`"(/[^"]+)"`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		for _, cm := range tok.FindAllStringSubmatch(m[1], -1) {
			out[cm[1]] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("found no slash-command cases in handleSlash")
	}
	return out
}

func TestSuggestCommand(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/hlep", "/help"},   // transposition
		{"/mdoe", "/mode"}, // transposition
		{"/moddel", "/model"}, // one insertion
		{"/modle", "/mode"},   // nearer to /mode (1 edit) than /model (2 edits)
		{"/quiy", "/quit"},   // one substitution
		{"/exi", "/exit"},    // one deletion (alias)
		{"/cost", "/cost"},   // exact (still returns itself)
		{"/help", "/help"},   // exact
		// Too far from any command -> no suggestion (plain error path).
		{"/xyzzy", ""},
		{"/deploy", ""},
	}
	for _, tc := range tests {
		if got := suggestCommand(tc.in); got != tc.want {
			t.Errorf("suggestCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"/help", "/hlep", 2},
	}
	for _, tc := range cases {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// ---------- buildCostReport ----------

func TestBuildCostReportPrefersActualUsage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("costest")
	s.AddActualUsage(ai.Usage{InputTokens: 1000, OutputTokens: 500, CacheCreationTokens: 200, CacheReadTokens: 100})
	s.AddExchange("hello", "world")

	rate, known := pricing.Lookup("claude-opus-4-8")
	rep := buildCostReport(s, rate, known, "claude-opus-4-8")
	if !rep.isActual {
		t.Error("expected isActual=true when ActualUsage has data")
	}
	if !strings.Contains(rep.tokens, "input") {
		t.Errorf("tokens line should mention 'input', got: %q", rep.tokens)
	}
	if !strings.Contains(rep.tokens, "cache-write") {
		t.Errorf("tokens line should mention 'cache-write', got: %q", rep.tokens)
	}
	if strings.Contains(rep.footer, "estimate") {
		t.Errorf("footer should not say 'estimate' when using actual usage, got: %q", rep.footer)
	}
	if !strings.Contains(rep.footer, "actual") {
		t.Errorf("footer should say 'actual' for real usage, got: %q", rep.footer)
	}
}

func TestBuildCostReportFallsBackToEstimate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("costfallback")
	// No actual usage recorded — only text messages.
	s.AddExchange("what is go?", "Go is a statically typed language.")

	rate, known := pricing.Lookup("claude-opus-4-8")
	rep := buildCostReport(s, rate, known, "claude-opus-4-8")
	if rep.isActual {
		t.Error("expected isActual=false when no actual usage recorded")
	}
	if !strings.Contains(rep.tokens, "prompt") {
		t.Errorf("tokens line should mention 'prompt', got: %q", rep.tokens)
	}
	if !strings.Contains(rep.footer, "estimate") {
		t.Errorf("footer should say 'estimate' when using heuristic, got: %q", rep.footer)
	}
}

func TestBuildCostReportLocalModel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("costlocal")
	s.AddActualUsage(ai.Usage{InputTokens: 1000, OutputTokens: 500})

	rate, known := pricing.Lookup("ollama:llama3")
	rep := buildCostReport(s, rate, known, "ollama:llama3")
	if !strings.Contains(rep.cost, "$0.00") {
		t.Errorf("local model cost line should contain '$0.00', got: %q", rep.cost)
	}
}

func TestBuildCostReportUnknownModel(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("costunk")
	s.AddActualUsage(ai.Usage{InputTokens: 1000, OutputTokens: 500})

	rate, known := pricing.Lookup("gpt-99-unknown")
	rep := buildCostReport(s, rate, known, "gpt-99-unknown")
	if !strings.Contains(rep.cost, "unknown pricing") {
		t.Errorf("unknown model cost line should mention 'unknown pricing', got: %q", rep.cost)
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
