package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"

	"spaios/internal/config"
	"spaios/internal/protocol"
	"spaios/internal/session"
	"spaios/internal/socket"
	"spaios/internal/spaish"
)

func sockPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaios", "spaid.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaios", "spaid.sock")
}

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaios", "spaid.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaios", "spaid.toml")
}

func main() {
	shellFlag := flag.String("shell", "", "shell to wrap (defaults to $SHELL or bash)")
	flag.Parse()

	cfg, err := config.Load(configPath())
	if err != nil {
		// Config file may not exist; use defaults.
		cfg = &config.Config{}
	}

	shellBin := *shellFlag
	if shellBin == "" {
		shellBin = cfg.Spaish.Shell
	}
	if shellBin == "" {
		shellBin = os.Getenv("SHELL")
	}
	if shellBin == "" {
		shellBin = "/bin/bash"
	}

	// Ensure spaid is running
	sock := sockPath()
	if err := socket.EnsureRunning(sock, "spaid"); err != nil {
		log.Fatalf("spaid: %v", err)
	}
	client := socket.NewClient(sock)

	// Session ID: unique per spaiSH process
	sessID := fmt.Sprintf("spaish_%d", os.Getpid())
	sessDir := filepath.Join(session.SessionsDir(), sessID)

	// Start PTY (before raw mode — pty.Start needs normal terminal state)
	p, err := spaish.New(shellBin)
	if err != nil {
		log.Fatalf("pty: %v", err)
	}

	// Set raw mode on stdin
	rawState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		p.Close()
		log.Fatalf("raw mode: %v", err)
	}
	restore := func() {
		term.Restore(int(os.Stdin.Fd()), rawState)
	}
	defer restore()

	// Handle Ctrl+C / SIGTERM: restore terminal before exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		restore()
		p.Close()
		os.Exit(0)
	}()

	conv := spaish.NewConversation(client, sessID, sessDir)
	ring := &spaish.Ring{}
	suppressed := make(map[string]bool)
	errorThreshold := cfg.Spaish.ErrorThreshold
	if errorThreshold == 0 {
		errorThreshold = 1
	}
	patternMin := cfg.Spaish.PatternMinCount
	if patternMin == 0 {
		patternMin = 3
	}

	// PTY event loop — runs in a goroutine, main goroutine handles input
	eventDone := make(chan struct{})
	go func() {
		defer close(eventDone)
		for ev := range p.Events() {
			if ev.Command == "" {
				continue // startup / hook-only events
			}

			if ev.ExitCode >= errorThreshold {
				// Switch to cooked mode for the conversation UI
				restore()
				convEv := &protocol.ShellEvent{
					Trigger:  "error",
					Command:  ev.Command,
					Output:   ev.Output,
					ExitCode: ev.ExitCode,
					CWD:      ev.CWD,
				}
				if err := conv.Start(convEv); err != nil {
					fmt.Fprintf(os.Stderr, "spaiSH: %v\n", err)
				}
				// Return to raw mode
				rawState, _ = term.MakeRaw(int(os.Stdin.Fd()))
				continue
			}

			ring.Add(ev.Command)
			result := ring.Detect(patternMin)
			if result == nil {
				continue
			}
			key := strings.Join(result.Commands, "|")
			if suppressed[key] {
				continue
			}

			suggestion := patternSuggestion(result)
			// Switch to cooked mode to read y/n
			restore()
			fmt.Printf("  \033[2m\U0001f4a1 %s (y/n)\033[0m ", suggestion)
			var yn string
			fmt.Scanln(&yn)
			if strings.ToLower(strings.TrimSpace(yn)) == "y" {
				convEv := &protocol.ShellEvent{
					Trigger: "pattern",
					Command: strings.Join(result.Commands, "\n"),
					CWD:     ev.CWD,
				}
				conv.Start(convEv)
			} else {
				suppressed[key] = true
			}
			rawState, _ = term.MakeRaw(int(os.Stdin.Fd()))
		}
	}()

	// Input loop — intercepts Enter to check for ? prefix or natural language
	inputLoop(p, conv, &rawState)

	<-eventDone
}

// inputLoop reads stdin in raw mode, forwarding characters to the PTY.
// On Enter, checks if the buffered line is a ? prompt or natural language.
func inputLoop(p *spaish.PTY, conv *spaish.Conversation, rawStatePtr **term.State) {
	var lineBuf strings.Builder
	buf := make([]byte, 32)

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}

		b := buf[0]

		switch {
		case (b == '\r' || b == '\n') && n == 1:
			line := lineBuf.String()
			lineBuf.Reset()

			if strings.HasPrefix(line, "?") {
				// Explicit AI prompt — clear PTY line, enter conversation
				p.ClearLine()
				query := strings.TrimSpace(strings.TrimPrefix(line, "?"))
				term.Restore(int(os.Stdin.Fd()), *rawStatePtr)
				ev := &protocol.ShellEvent{
					Trigger: "prompt",
					Query:   query,
					CWD:     p.LastCWD(),
				}
				conv.Start(ev)
				*rawStatePtr, _ = term.MakeRaw(int(os.Stdin.Fd()))
			} else if spaish.IsNaturalLanguage(line) {
				// Natural language — clear PTY line, enter conversation
				p.ClearLine()
				term.Restore(int(os.Stdin.Fd()), *rawStatePtr)
				ev := &protocol.ShellEvent{
					Trigger: "prompt",
					Query:   line,
					CWD:     p.LastCWD(),
				}
				conv.Start(ev)
				*rawStatePtr, _ = term.MakeRaw(int(os.Stdin.Fd()))
			} else {
				// Regular shell command — record and forward
				p.SetLastCommand(line)
				p.Write(buf[:n])
			}

		case b == 127 || b == 8: // DEL/backspace
			if lineBuf.Len() > 0 {
				s := lineBuf.String()
				lineBuf.Reset()
				lineBuf.WriteString(s[:len(s)-1])
			}
			p.Write(buf[:n])

		default:
			// Printable char or escape sequence — buffer printable, forward all
			if n == 1 && b >= 32 {
				lineBuf.WriteByte(b)
			}
			p.Write(buf[:n])
		}
	}
}

func patternSuggestion(r *spaish.PatternResult) string {
	switch r.Kind {
	case "alias", "long":
		return fmt.Sprintf("You've run this command %d times. Want an alias?", r.Count)
	case "script":
		return fmt.Sprintf("You've run this %d-command sequence %d times. Want a script?", len(r.Commands), r.Count)
	default:
		return fmt.Sprintf("Repeated pattern detected (%d times). Automate it?", r.Count)
	}
}
