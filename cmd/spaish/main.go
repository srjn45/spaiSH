package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"spaios/internal/config"
	"spaios/internal/protocol"
	"spaios/internal/session"
	"spaios/internal/socket"
	"spaios/internal/spaish"
)

// rawMode guards concurrent terminal mode transitions.
type rawMode struct {
	mu    sync.Mutex
	state *term.State
	fd    int
}

func newRawMode(fd int) (*rawMode, error) {
	s, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return &rawMode{state: s, fd: fd}, nil
}

func (r *rawMode) restore() {
	r.mu.Lock()
	defer r.mu.Unlock()
	term.Restore(r.fd, r.state)
}

func (r *rawMode) reenter() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state, _ = term.MakeRaw(r.fd)
}

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

// convRequest carries a conversation trigger from the event goroutine to inputLoop.
type convRequest struct {
	ev         *protocol.ShellEvent
	patternKey string              // non-empty for pattern triggers
	pattern    *spaish.PatternResult
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
	rm, err := newRawMode(int(os.Stdin.Fd()))
	if err != nil {
		p.Close()
		log.Fatalf("raw mode: %v", err)
	}
	defer rm.restore()

	// Handle Ctrl+C / SIGTERM: restore terminal before exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		rm.restore()
		p.Close()
		os.Exit(0)
	}()

	conv := spaish.NewConversation(client, sessID, sessDir)
	ring := &spaish.Ring{}
	errorThreshold := cfg.Spaish.ErrorThreshold
	if errorThreshold == 0 {
		errorThreshold = 1
	}
	patternMin := cfg.Spaish.PatternMinCount
	if patternMin == 0 {
		patternMin = 3
	}

	// stdinCh is the single owner of os.Stdin bytes.
	// All consumers (inputLoop, conversations) read from here — never from
	// os.Stdin directly — so there is exactly one goroutine calling Read.
	stdinCh := make(chan []byte, 32)
	go func() {
		buf := make([]byte, 32)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				stdinCh <- cp
			}
			if err != nil {
				close(stdinCh)
				return
			}
		}
	}()

	// convReqCh carries requests from the PTY event goroutine to inputLoop.
	// inputLoop is the sole stdin owner, so all interactive I/O happens there.
	convReqCh := make(chan convRequest, 4)

	// PTY event loop — detects errors and patterns, sends to convReqCh.
	eventDone := make(chan struct{})
	suppressed := make(map[string]bool)
	go func() {
		defer close(eventDone)
		for ev := range p.Events() {
			if ev.Command == "" {
				continue // startup / hook-only events
			}

			// Signal exits (128 + signal number) mean the command was
			// killed externally (Ctrl+C → 130, kill → 137, etc.).
			// These are intentional interruptions, not command failures —
			// do not trigger AI for them.
			isSignalExit := ev.ExitCode > 128
			if ev.ExitCode >= errorThreshold && !isSignalExit {
				convReqCh <- convRequest{
					ev: &protocol.ShellEvent{
						Trigger:  "error",
						Command:  ev.Command,
						Output:   ev.Output,
						ExitCode: ev.ExitCode,
						CWD:      ev.CWD,
					},
				}
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
			convReqCh <- convRequest{
				ev: &protocol.ShellEvent{
					Trigger: "pattern",
					Command: strings.Join(result.Commands, "\n"),
					CWD:     ev.CWD,
				},
				patternKey: key,
				pattern:    result,
			}
		}
	}()

	inputLoop(p, conv, rm, stdinCh, convReqCh, suppressed)
	<-eventDone
}

// inputLoop is the single owner of stdinCh. It forwards keystrokes to the PTY
// and — when the shell is at a prompt — detects "?" prefixes and natural-language
// input. Conversation requests from the event goroutine arrive via convReqCh.
func inputLoop(
	p *spaish.PTY,
	conv *spaish.Conversation,
	rm *rawMode,
	stdinCh <-chan []byte,
	convReqCh <-chan convRequest,
	suppressed map[string]bool,
) {
	var lineBuf strings.Builder
	// inEscSeq tracks whether we're inside a terminal escape sequence
	// (ESC [ ... letter). Bytes inside escape sequences must not be buffered
	// in lineBuf — they are not typed text and would corrupt NL detection.
	var inEscSeq bool

	for {
		select {

		case req, ok := <-convReqCh:
			if !ok {
				convReqCh = nil
				continue
			}
			handleConvReq(req, p, conv, rm, stdinCh, suppressed)

		case data, ok := <-stdinCh:
			if !ok {
				return
			}
			if len(data) == 0 {
				continue
			}

			// When not at the shell prompt (a command is running or a subprocess
			// is waiting for input such as sudo password), forward everything raw
			// without buffering or NL analysis.
			if !p.AtPrompt() {
				p.Write(data)
				continue
			}

			// Process the chunk byte-by-byte so that both interactive input
			// (one byte per read in raw mode) and automated/pasted input
			// (whole line delivered as a single chunk) are handled correctly.
			// We look for the first newline; bytes before it are accumulated
			// into lineBuf using the same escape-sequence filtering as before.
			nlPos := -1
			for i, b := range data {
				if b == '\r' || b == '\n' {
					nlPos = i
					break
				}
			}

			if nlPos < 0 {
				// No newline in this chunk — buffer printable chars and forward.
				for _, b := range data {
					switch {
					case b == 0x1b:
						inEscSeq = true
					case inEscSeq:
						if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
							inEscSeq = false
						}
					case b == 127 || b == 8:
						if lineBuf.Len() > 0 {
							s := lineBuf.String()
							lineBuf.Reset()
							lineBuf.WriteString(s[:len(s)-1])
						}
					case b >= 32:
						lineBuf.WriteByte(b)
					}
				}
				p.Write(data)
				continue
			}

			// Newline found at nlPos.
			// Accumulate characters before the newline into lineBuf.
			for _, b := range data[:nlPos] {
				switch {
				case b == 0x1b:
					inEscSeq = true
				case inEscSeq:
					if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
						inEscSeq = false
					}
				case b == 127 || b == 8:
					if lineBuf.Len() > 0 {
						s := lineBuf.String()
						lineBuf.Reset()
						lineBuf.WriteString(s[:len(s)-1])
					}
				case b >= 32:
					lineBuf.WriteByte(b)
				}
			}

			line := lineBuf.String()
			lineBuf.Reset()
			inEscSeq = false

			if strings.HasPrefix(line, "?") {
				// Explicit AI prompt — clear PTY line, enter conversation
				p.ClearLine()
				query := strings.TrimSpace(strings.TrimPrefix(line, "?"))
				rm.restore()
				ev := &protocol.ShellEvent{
					Trigger: "prompt",
					Query:   query,
					CWD:     p.LastCWD(),
				}
				if err := conv.Start(ev, stdinCh); err != nil {
					fmt.Fprintf(os.Stderr, "spaiSH: %v\n", err)
				}
				rm.reenter()
				// Trigger the shell to redisplay its prompt so the user
				// has a clear visual anchor after leaving conversation mode.
				p.Write([]byte{'\n'})

			} else if spaish.IsNaturalLanguage(line) {
				// Natural language — clear PTY line, enter conversation
				p.ClearLine()
				rm.restore()
				ev := &protocol.ShellEvent{
					Trigger: "prompt",
					Query:   line,
					CWD:     p.LastCWD(),
				}
				if err := conv.Start(ev, stdinCh); err != nil {
					fmt.Fprintf(os.Stderr, "spaiSH: %v\n", err)
				}
				rm.reenter()
				p.Write([]byte{'\n'})

			} else {
				// Regular shell command — record and forward through newline.
				p.SetLastCommand(line)
				p.Write(data[:nlPos+1])
			}
			// Any bytes after the newline in the chunk are rare in practice
			// (a sendline delivers exactly one command) and are discarded.
		}
	}
}

// handleConvReq processes a single convRequest from the event goroutine.
// It runs in the inputLoop goroutine (main goroutine), which owns stdinCh.
func handleConvReq(
	req convRequest,
	p *spaish.PTY,
	conv *spaish.Conversation,
	rm *rawMode,
	stdinCh <-chan []byte,
	suppressed map[string]bool,
) {
	if req.pattern != nil {
		// Pattern detected — ask y/n before entering conversation.
		suggestion := patternSuggestion(req.pattern)
		rm.restore()
		fmt.Printf("  \033[2m\U0001f4a1 %s (y/n)\033[0m ", suggestion)
		yn, ok := spaish.ReadLine(stdinCh)
		if !ok || strings.ToLower(strings.TrimSpace(yn)) != "y" {
			suppressed[req.patternKey] = true
			rm.reenter()
			p.Write([]byte{'\n'})
			return
		}
		if err := conv.Start(req.ev, stdinCh); err != nil {
			fmt.Fprintf(os.Stderr, "spaiSH: %v\n", err)
		}
		rm.reenter()
		p.Write([]byte{'\n'})
		return
	}

	// Error trigger — show AI response immediately.
	rm.restore()
	if err := conv.Start(req.ev, stdinCh); err != nil {
		fmt.Fprintf(os.Stderr, "spaiSH: %v\n", err)
	}
	rm.reenter()
	p.Write([]byte{'\n'})
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
