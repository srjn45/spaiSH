// Package cli renders the agent's response event stream for the terminal:
// streamed text, tool activity, tool output, a working spinner, and tier
// confirmation prompts. It is the front-end over the agent engine; the one-shot
// and (later) interactive REPL are thin drivers on top of it.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"

	"spaish/internal/permissions"
	"spaish/internal/protocol"
)

// ANSI helpers. Kept dependency-free and consistent with the rest of the CLI.
const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
	ansiCyan   = "\033[36m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

func dim(s string) string    { return ansiDim + s + ansiReset }
func cyan(s string) string   { return ansiCyan + s + ansiReset }
func red(s string) string    { return ansiRed + s + ansiReset }
func green(s string) string  { return ansiGreen + s + ansiReset }
func yellow(s string) string { return ansiYellow + s + ansiReset }
func bold(s string) string   { return ansiBold + s + ansiReset }

// RenderResponse writes one streamed response chunk to the terminal without
// markdown styling. Tool activity lines (prefixed with ▶) are highlighted; tool
// output is dimmed; errors go to stderr in red. It is used for simple status
// streams (e.g. session commands); agent turns use a *Renderer instead so
// assistant prose can be rendered as markdown.
func RenderResponse(resp protocol.Response) {
	switch resp.Type {
	case "text":
		if strings.HasPrefix(resp.Content, "▶") {
			fmt.Print(cyan(resp.Content))
		} else {
			fmt.Print(resp.Content)
		}
	case "output":
		fmt.Print(dim(resp.Content))
	case "error":
		fmt.Fprintf(os.Stderr, "\n%s\n", red("error: "+resp.Content))
	}
}

// Renderer renders a streamed agent turn, treating the model's natural-language
// prose as markdown via glamour. Because glamour renders complete documents (not
// token deltas), prose is buffered and flushed as a unit when the prose block
// ends — at a tool-call line (▶), tool output, an error, or turn completion.
// Tool-call lines stay cyan, tool output dim, and errors red, exactly as before.
//
// When stdout is not a TTY (piped output), markdown rendering is disabled and
// prose is written verbatim so piping stays clean and ANSI-free.
type Renderer struct {
	tty bool                  // whether stdout is a terminal
	md  *glamour.TermRenderer // nil when output is not a TTY or glamour init failed
	buf strings.Builder       // accumulated prose awaiting a flush
}

// NewRenderer constructs a Renderer. Markdown and ANSI styling are enabled only
// when stdout is a terminal; the glamour style is auto-detected (dark/light)
// with a dark default. When piped, all output is plain text.
func NewRenderer() *Renderer {
	r := &Renderer{tty: isTerminal(os.Stdout)}
	if !r.tty {
		return r
	}
	width := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}
	md, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err == nil {
		r.md = md
	}
	return r
}

// Render handles one streamed response chunk, buffering prose and flushing it as
// markdown at block boundaries.
func (r *Renderer) Render(resp protocol.Response) {
	switch resp.Type {
	case "text":
		if strings.HasPrefix(resp.Content, "▶") {
			r.Flush()
			fmt.Print(r.style(resp.Content, cyan))
		} else {
			r.buf.WriteString(resp.Content)
		}
	case "output":
		r.Flush()
		fmt.Print(r.style(resp.Content, dim))
	case "error":
		r.Flush()
		fmt.Fprintf(os.Stderr, "\n%s\n", r.style("error: "+resp.Content, red))
	case "done":
		r.Flush()
	}
}

// style applies an ANSI color helper only when stdout is a terminal.
func (r *Renderer) style(s string, fn func(string) string) string {
	if !r.tty {
		return s
	}
	return fn(s)
}

// Flush renders any buffered prose. With a TTY it is rendered as markdown;
// otherwise (or if rendering fails) it is written verbatim.
func (r *Renderer) Flush() {
	if r.buf.Len() == 0 {
		return
	}
	text := r.buf.String()
	r.buf.Reset()
	if r.md == nil {
		fmt.Print(text)
		return
	}
	out, err := r.md.Render(text)
	if err != nil {
		fmt.Print(text)
		return
	}
	fmt.Print(out)
}

// isTerminal reports whether f refers to a terminal (vs. a pipe or file).
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// Spinner shows an animated working indicator on stderr until stopped. Output
// goes to stderr so it never pollutes piped stdout.
type Spinner struct {
	label string
	mu    sync.Mutex
	stop  chan struct{}
	done  chan struct{}
	on    bool
}

// NewSpinner creates a spinner with the given label.
func NewSpinner(label string) *Spinner { return &Spinner{label: label} }

// Start begins animating (no-op if already running or stdout is not a TTY peer).
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.on {
		return
	}
	s.on = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go func() {
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		t := time.NewTicker(90 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K")
				close(s.done)
				return
			case <-t.C:
				fmt.Fprintf(os.Stderr, "\r%s %s", cyan(string(frames[i%len(frames)])), dim(s.label))
				i++
			}
		}
	}()
}

// Stop halts the spinner and clears its line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.on {
		return
	}
	close(s.stop)
	<-s.done
	s.on = false
}

// PromptConfirm asks the user to approve a tier-gated tool call, with stronger
// friction for elevated and destructive actions. Returns true if approved.
func PromptConfirm(req protocol.ConfirmRequest) bool {
	reader := bufio.NewReader(os.Stdin)
	switch req.Tier {
	case permissions.TierDestructive.String():
		fmt.Printf("\n%s\n   %s\n", red(bold("⚠  DESTRUCTIVE — cannot be undone:")), req.Command)
		printDiffPreview(req)
		fmt.Print("Type " + bold("YES") + " to confirm: ")
		input, _ := reader.ReadString('\n')
		return strings.TrimSpace(input) == "YES"
	case permissions.TierElevated.String():
		fmt.Printf("\n%s\n   %s\n", yellow(bold("⚠  ELEVATED — requires elevated privileges:")), req.Command)
		printDiffPreview(req)
		fmt.Print("Allow? [y/N]: ")
	default:
		fmt.Printf("\n%s %s\n", cyan("▶"), req.Command)
		printDiffPreview(req)
		fmt.Print("Allow? [y/N]: ")
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

// diffContextLines is how many unchanged lines of context to show around each
// change in a confirmation diff preview.
const diffContextLines = 3

// printDiffPreview renders a unified diff of the proposed file change before the
// y/N prompt, when the confirm request carries one. Output is colored only when
// stdout is a terminal; piped output gets a plain, ANSI-free diff. A no-op edit
// (identical content) produces no diff and prints nothing.
func printDiffPreview(req protocol.ConfirmRequest) {
	if !req.ShowDiff {
		return
	}
	lines := computeDiff(req.OldContent, req.NewContent, diffContextLines)
	out := renderDiff(lines, isTerminal(os.Stdout))
	if out == "" {
		return
	}
	fmt.Print(out)
}
