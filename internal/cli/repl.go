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

	"github.com/chzyer/readline"

	"spaish/internal/agent"
	"spaish/internal/app"
	"spaish/internal/protocol"
)

// REPL holds interactive-session state.
type REPL struct {
	app       *app.App
	sessionID string
	cwd       string
	gitBranch string
	mode      string
}

// NewREPL creates an interactive session driver.
func NewREPL(a *app.App, sessionID, cwd, gitBranch string) *REPL {
	return &REPL{app: a, sessionID: sessionID, cwd: cwd, gitBranch: gitBranch, mode: agent.ModeManual}
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
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 r.prompt(),
		HistoryFile:            historyFile(),
		HistorySearchFold:      true,
		DisableAutoSaveHistory: false,
		AutoComplete:           completer(),
		InterruptPrompt:        "^C",
		EOFPrompt:              "exit",
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

// runTurn sends a query to the agent, rendering the response. Ctrl+C cancels the
// in-flight turn without exiting the REPL.
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

	fmt.Println()
	if err := RunOneShot(ctx, r.app, req); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", red("error: "+err.Error()))
	}
	fmt.Println()
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
