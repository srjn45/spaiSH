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
	ansiYellow = "\033[33m"
)

func dim(s string) string    { return ansiDim + s + ansiReset }
func cyan(s string) string   { return ansiCyan + s + ansiReset }
func red(s string) string    { return ansiRed + s + ansiReset }
func yellow(s string) string { return ansiYellow + s + ansiReset }
func bold(s string) string   { return ansiBold + s + ansiReset }

// RenderResponse writes one streamed response chunk to the terminal. Tool
// activity lines (prefixed with ▶) are highlighted; tool output is dimmed;
// errors go to stderr in red. Plain assistant prose is printed verbatim so
// streaming stays smooth.
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
		fmt.Print("Type " + bold("YES") + " to confirm: ")
		input, _ := reader.ReadString('\n')
		return strings.TrimSpace(input) == "YES"
	case permissions.TierElevated.String():
		fmt.Printf("\n%s\n   %s\n", yellow(bold("⚠  ELEVATED — requires elevated privileges:")), req.Command)
		fmt.Print("Allow? [y/N]: ")
	default:
		fmt.Printf("\n%s %s\n", cyan("▶"), req.Command)
		fmt.Print("Allow? [y/N]: ")
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
