package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chzyer/readline"

	"spaish/internal/agent"
	"spaish/internal/app"
	"spaish/internal/config"
	"spaish/internal/protocol"
)

// Multiline-input markers (interactive stdin only). fenceMarker on its own line
// opens/closes a verbatim block; a single trailing backslash continues a line.
const (
	fenceMarker        = `"""`
	continuationPrompt = "… "
	// multilineAbort is the sentinel line value next() returns (with ok=false)
	// to signal an interrupt (Ctrl+C) mid-block, telling assembleMultiline to
	// discard the accumulated block. Any other ok=false result is treated as EOF:
	// submit what has accumulated so far.
	multilineAbort = "\x00abort\x00"
)

// Fuzzy @file completion bounds.
const (
	maxWalkEntries  = 10000 // cap on working-tree entries gathered per session
	maxFuzzyResults = 50    // cap on completion candidates returned per Tab
)

// Fuzzy-match scoring weights. Higher score = better match; see fuzzyMatch.
const (
	boundaryBonus    = 10 // matched rune at index 0 or after '/' or '.'
	consecutiveBonus = 5  // matched rune adjacent to the previous match
	gapPenalty       = 1  // per skipped rune between matches
)

// walkSkipDirs are directory names never descended into when gathering the
// working-tree snapshot for fuzzy @file completion.
var walkSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".worktrees":   true,
}

// REPL holds interactive-session state.
type REPL struct {
	app       *app.App
	sessionID string
	cwd       string
	gitBranch string
	mode      string
	commands  []config.Command // discovered .spai/commands/*.md custom commands
}

// NewREPL creates an interactive session driver. Custom slash commands are
// discovered once from the working directory at construction time.
func NewREPL(a *app.App, sessionID, cwd, gitBranch string) *REPL {
	cmds, _ := config.DiscoverCommands(cwd)
	return &REPL{app: a, sessionID: sessionID, cwd: cwd, gitBranch: gitBranch, mode: agent.ModeManual, commands: cmds}
}

// commandNames returns the names of the discovered custom commands, for tab
// completion.
func (r *REPL) commandNames() []string {
	names := make([]string, len(r.commands))
	for i, c := range r.commands {
		names[i] = c.Name
	}
	return names
}

func (r *REPL) prompt() string {
	tag := r.mode
	color := ansiCyan
	if r.mode == agent.ModeAuto {
		color = ansiYellow
	} else if r.mode == agent.ModePlan {
		color = ansiDim
	}
	return fmt.Sprintf("%s%s%s ▶ ", color, tag, ansiReset)
}

func historyFile() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	d := filepath.Join(dir, "spaish")
	os.MkdirAll(d, 0700)
	return filepath.Join(d, "repl_history")
}

// Run starts the interactive loop. It returns when the user exits.
func (r *REPL) Run() error {
	var rl *readline.Instance
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 r.prompt(),
		HistoryFile:            historyFile(),
		HistorySearchFold:      true,
		DisableAutoSaveHistory: false,
		AutoComplete:           newFuzzyCompleter(r.cwd, r.commandNames()...),
		InterruptPrompt:        "^C",
		EOFPrompt:              "exit",
		// Read Shift-Tab from a wrapper that rewrites CSI Z to a sentinel rune,
		// since readline's terminal layer otherwise drops it.
		Stdin: newShiftTabReader(os.Stdin),
		// Shift-Tab cycles the execution mode at the prompt. This runs in
		// readline's line-editing goroutine, so it is safe to update the prompt
		// here; returning false makes readline ignore the rune and redraw.
		FuncFilterInputRune: func(rn rune) (rune, bool) {
			if rn == shiftTabSentinel {
				r.mode = cycleMode(r.mode)
				if rl != nil {
					rl.SetPrompt(r.prompt())
				}
				return 0, false
			}
			return rn, true
		},
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	fmt.Printf("%s — interactive session. Type %s for commands, %s to leave.\n",
		bold("spaiSH"), cyan("/help"), cyan("/quit"))
	fmt.Printf("%s\n\n", dim("provider: "+r.app.ProviderInfo()))

	for {
		rl.SetPrompt(r.prompt())
		line, err := rl.Readline()
		if err == readline.ErrInterrupt {
			continue // Ctrl+C at the prompt clears the line
		}
		if err == io.EOF {
			break
		}
		// On an interactive terminal, assemble a possibly multi-line message from
		// the fence/continuation markers before treating it as one turn. Piped or
		// redirected stdin skips this entirely and submits each line verbatim, so
		// no script's behaviour shifts.
		if isTerminal(os.Stdin) {
			var aborted bool
			line, aborted = assembleMultiline(line, func() (string, bool) {
				rl.SetPrompt(dim(continuationPrompt))
				l, e := rl.Readline()
				switch e {
				case readline.ErrInterrupt:
					return multilineAbort, false
				case io.EOF:
					return "", false
				}
				return l, true
			})
			if aborted {
				continue // Ctrl+C mid-block discards it and returns to the prompt
			}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if r.handleSlash(line) {
				break
			}
			continue
		}
		r.runTurn(line)
	}
	return nil
}

// runTurn sends a query to the agent, rendering the response. Ctrl+C (SIGINT)
// and Esc both cancel the in-flight turn without exiting the REPL.
func (r *REPL) runTurn(line string) {
	query := expandAtRefs(line)
	req := &protocol.Request{
		Type:       "agent",
		SessionID:  r.sessionID,
		WorkingDir: r.cwd,
		GitBranch:  r.gitBranch,
		Agent:      &protocol.AgentRequest{Query: query, Mode: r.mode},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		if _, ok := <-sigCh; ok {
			fmt.Fprint(os.Stderr, dim(" (interrupted)\n"))
			cancel()
		}
	}()

	stopEsc := r.watchEscape(cancel)
	defer stopEsc()

	fmt.Println()
	if err := RunOneShot(ctx, r.app, req); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red("error: "+err.Error()))
	}
	fmt.Println()
}

// watchEscape watches stdin for a lone Esc keypress while a turn runs and
// cancels the turn when seen. It puts the terminal into cbreak mode (keeping
// SIGINT working) for the duration and returns a function that stops watching
// and restores the terminal. On non-terminal stdin (e.g. piped input) it is a
// no-op, so the REPL still relies on SIGINT for cancellation.
func (r *REPL) watchEscape(cancel context.CancelFunc) func() {
	fd := int(os.Stdin.Fd())
	restore, ok := enterCbreak(fd)
	if !ok {
		return func() {}
	}

	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		buf := make([]byte, 64)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, err := os.Stdin.Read(buf)
			if err != nil {
				// A blocked read is unblocked by a past deadline on stop, and
				// SIGINT delivery can surface as EINTR; either way, re-check
				// whether we should exit and otherwise keep watching.
				select {
				case <-done:
					return
				default:
					continue
				}
			}
			// A terminal delivers an escape sequence atomically, so a lone Esc
			// arrives as exactly one byte; multi-byte sequences (arrow keys,
			// etc.) start with Esc but must not count as an interrupt.
			if n == 1 && buf[0] == keyEsc {
				fmt.Fprint(os.Stderr, dim(" (interrupted)\n"))
				cancel()
				return
			}
		}
	}()

	return func() {
		close(done)
		// Force any in-flight blocking read to return so the goroutine can see
		// the done signal; bound the wait so a non-pollable stdin can't hang us.
		_ = os.Stdin.SetReadDeadline(time.Now())
		select {
		case <-finished:
		case <-time.After(500 * time.Millisecond):
		}
		_ = os.Stdin.SetReadDeadline(time.Time{})
		_ = restore()
	}
}

// assembleMultiline builds one message from an initial line plus a reader for
// subsequent lines. next returns (line, ok); ok=false ends input — a line equal
// to multilineAbort signals an interrupt (discard the whole block), any other
// ok=false means EOF (submit what has accumulated). The returned bool is aborted,
// true only for that interrupt case.
//
// Two additive submission markers are recognised, fence before continuation:
//   - a line trimmed to fenceMarker opens a fenced block, read verbatim until a
//     closing fenceMarker line; both markers are consumed, inner lines joined
//     with "\n".
//   - a line ending in a single (odd count of) trailing backslash continues onto
//     the next line; the marker backslash is stripped and reading continues until
//     a line without one. An escaped "\\" does not continue.
//
// Anything else is a single line, returned verbatim with next never called — so
// non-interactive behaviour is unchanged.
func assembleMultiline(first string, next func() (string, bool)) (string, bool) {
	if strings.TrimSpace(first) == fenceMarker {
		return readFence(next)
	}
	seg, cont := continuationSegment(first)
	if !cont {
		return first, false
	}
	return readContinuation(seg, next)
}

// readFence reads lines until a closing fenceMarker (or EOF), joining the inner
// lines with "\n". An interrupt sentinel aborts and discards the block.
func readFence(next func() (string, bool)) (string, bool) {
	var body []string
	for {
		line, ok := next()
		if !ok {
			if line == multilineAbort {
				return "", true
			}
			return strings.Join(body, "\n"), false // EOF: submit unterminated block
		}
		if strings.TrimSpace(line) == fenceMarker {
			return strings.Join(body, "\n"), false
		}
		body = append(body, line)
	}
}

// readContinuation keeps reading backslash-continued lines, starting from the
// already-stripped first segment, until a line that does not continue (or EOF).
func readContinuation(firstSeg string, next func() (string, bool)) (string, bool) {
	segs := []string{firstSeg}
	for {
		line, ok := next()
		if !ok {
			if line == multilineAbort {
				return "", true
			}
			return strings.Join(segs, "\n"), false // EOF: submit what accumulated
		}
		seg, cont := continuationSegment(line)
		segs = append(segs, seg)
		if !cont {
			return strings.Join(segs, "\n"), false
		}
	}
}

// continuationSegment inspects a line's trailing backslashes and returns the line
// with its continuation marker resolved plus whether it continues onto the next
// line. An odd count is a continuation (the final backslash is the marker and is
// stripped); an even count is an escaped backslash that collapses to one literal
// "\" and does not continue; no backslash returns the line unchanged.
func continuationSegment(line string) (string, bool) {
	k := trailingBackslashes(line)
	if k == 0 {
		return line, false
	}
	if k%2 == 1 {
		return line[:len(line)-1], true
	}
	return line[:len(line)-1], false
}

// trailingBackslashes counts the run of '\' at the end of s.
func trailingBackslashes(s string) int {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n
}

// fuzzyCompleter wraps the slash.go replCompleter, adding subsequence-fuzzy
// completion for '@file' fragments while delegating every other case (slash
// commands and non-'@' words) to the wrapped base completer unchanged. It
// implements readline.AutoCompleter.
type fuzzyCompleter struct {
	base  *replCompleter // the existing slash.go completer, reused as-is
	cwd   string
	files []string  // working-tree snapshot, gathered lazily on first @-Tab
	once  sync.Once // guards the one-time snapshot
}

// newFuzzyCompleter builds a fuzzy @file completer wrapping the standard REPL
// completer (newCompleter) rooted at cwd, so slash-command completion and every
// non-'@' case stay exactly as they are.
func newFuzzyCompleter(cwd string, custom ...string) *fuzzyCompleter {
	return &fuzzyCompleter{base: newCompleter(cwd, custom...), cwd: cwd}
}

// Do implements readline.AutoCompleter. It intercepts the whitespace-delimited
// word ending at the cursor when it starts with '@', serving fuzzy file
// completions; every other case delegates to the wrapped base completer.
func (c *fuzzyCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos > len(line) {
		pos = len(line)
	}
	start := pos
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	word := line[start:pos]
	if len(word) > 0 && word[0] == '@' {
		return c.fuzzyFileCompletions(string(word[1:]))
	}
	return c.base.Do(line, pos)
}

// snapshot returns the cached working-tree file list, gathering it once on first
// use. Lazy because most REPL turns never trigger @-completion; cached for the
// session, so files created mid-session need a restart to be offered.
func (c *fuzzyCompleter) snapshot() []string {
	c.once.Do(func() {
		if c.files == nil {
			c.files = gatherWorkingTree(c.cwd)
		}
	})
	return c.files
}

// fuzzyFileCompletions returns readline completion candidates for the '@'
// fragment frag (the text after '@', up to the cursor). Because a fuzzy match
// shares no prefix with frag, candidates are the FULL relative paths and the
// returned length is the rune count of frag, so readline replaces frag entirely
// while leaving the '@' in place. Results are the top maxFuzzyResults matches by
// score, ties broken by shorter path then lexicographically.
func (c *fuzzyCompleter) fuzzyFileCompletions(frag string) ([][]rune, int) {
	length := utf8.RuneCountInString(frag)
	type scored struct {
		path  string
		score int
	}
	var matches []scored
	for _, f := range c.snapshot() {
		if s, ok := fuzzyMatch(frag, f); ok {
			matches = append(matches, scored{f, s})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if len(matches[i].path) != len(matches[j].path) {
			return len(matches[i].path) < len(matches[j].path)
		}
		return matches[i].path < matches[j].path
	})
	if len(matches) > maxFuzzyResults {
		matches = matches[:maxFuzzyResults]
	}
	out := make([][]rune, len(matches))
	for i, m := range matches {
		out[i] = []rune(m.path)
	}
	return out, length
}

// fuzzyMatch reports whether every rune of pattern appears in candidate in order
// (case-insensitive) and, when it does, a score where higher is a better match.
// An empty pattern matches everything with score 0. Scoring rewards consecutive
// matched runes and matches at path boundaries (index 0, or after '/' or '.'),
// and penalises gaps, so filename-start matches outrank scattered ones.
func fuzzyMatch(pattern, candidate string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	pr := []rune(strings.ToLower(pattern))
	cr := []rune(strings.ToLower(candidate))
	score, pi, prevMatch := 0, 0, -2
	for ci := 0; ci < len(cr) && pi < len(pr); ci++ {
		if cr[ci] != pr[pi] {
			continue
		}
		if ci == 0 || cr[ci-1] == '/' || cr[ci-1] == '.' {
			score += boundaryBonus
		}
		if ci == prevMatch+1 {
			score += consecutiveBonus
		} else if prevMatch >= 0 {
			score -= (ci - prevMatch - 1) * gapPenalty
		}
		prevMatch = ci
		pi++
	}
	if pi != len(pr) {
		return 0, false
	}
	return score, true
}

// gatherWorkingTree walks cwd and returns file and directory paths relative to
// cwd (slash-separated via filepath.ToSlash), skipping walkSkipDirs and stopping
// at maxWalkEntries so a huge or pathological tree can't stall the prompt.
// Directories are included as candidates with a trailing '/'.
func gatherWorkingTree(cwd string) []string {
	var out []string
	_ = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the walk
		}
		if path == cwd {
			return nil
		}
		if d.IsDir() && walkSkipDirs[d.Name()] {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		out = append(out, rel)
		if len(out) >= maxWalkEntries {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}

// expandAtRefs appends the contents of any @path tokens to the query as context.
func expandAtRefs(line string) string {
	var refs strings.Builder
	for _, f := range strings.Fields(line) {
		if strings.HasPrefix(f, "@") && len(f) > 1 {
			p := f[1:]
			if data, err := os.ReadFile(p); err == nil {
				fmt.Fprintf(&refs, "\n\n--- %s ---\n%s", p, string(data))
			}
		}
	}
	return line + refs.String()
}
