package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"

	"spaish/internal/agent"
	"spaish/internal/app"
	"spaish/internal/config"
	"spaish/internal/protocol"
)

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
		AutoComplete:           newCompleter(r.cwd, r.commandNames()...),
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
