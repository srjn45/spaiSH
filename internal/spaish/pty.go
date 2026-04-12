package spaish

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// ShellOutput is emitted after each shell command completes.
type ShellOutput struct {
	Command  string
	Output   string
	ExitCode int
	CWD      string
}

// PTY wraps a shell process in a pseudo-terminal.
type PTY struct {
	ptmx      *os.File
	cmd       *exec.Cmd
	events    chan ShellOutput
	mu        sync.Mutex
	lastCmd   string
	lastCWD   string
	stopCh    chan struct{}
	closeOnce sync.Once
}

// New starts shellBin in a PTY and injects the spaiSH hook.
// Raw mode is NOT set here — the caller (cmd/spaish/main.go) sets raw mode after New returns.
func New(shellBin string) (*PTY, error) {
	if shellBin == "" {
		shellBin = os.Getenv("SHELL")
		if shellBin == "" {
			shellBin = "/bin/bash"
		}
	}

	cmd := exec.Command(shellBin)
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	// Match terminal size
	if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
		_ = err // non-fatal: cosmetic only
	}

	p := &PTY{
		ptmx:   ptmx,
		cmd:    cmd,
		events: make(chan ShellOutput, 16),
		stopCh: make(chan struct{}),
	}

	// Forward SIGWINCH (terminal resize) to the PTY slave
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-winch:
				pty.InheritSize(os.Stdin, ptmx)
			case <-p.stopCh:
				signal.Stop(winch)
				return
			}
		}
	}()

	// Inject the hook by writing to PTY stdin
	hook := HookScript(shellBin)
	ptmx.Write([]byte(hook + "\n"))

	go p.readLoop()

	return p, nil
}

// Events returns the channel of shell output events.
func (p *PTY) Events() <-chan ShellOutput {
	return p.events
}

// Write sends bytes to the PTY (forwards keyboard input).
func (p *PTY) Write(b []byte) (int, error) {
	return p.ptmx.Write(b)
}

// SetLastCommand stores the command last entered by the user.
// Called from the input loop in cmd/spaish/main.go on Enter.
func (p *PTY) SetLastCommand(cmd string) {
	p.mu.Lock()
	p.lastCmd = cmd
	p.mu.Unlock()
}

// LastCWD returns the working directory from the most recent SPAISH marker.
func (p *PTY) LastCWD() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastCWD
}

// ClearLine sends Ctrl+U to the PTY, erasing the current readline buffer.
func (p *PTY) ClearLine() {
	p.ptmx.Write([]byte{21}) // Ctrl+U
}

// Close terminates the shell process and cleans up.
func (p *PTY) Close() error {
	p.closeOnce.Do(func() {
		close(p.stopCh)
	})
	p.ptmx.Close()
	return p.cmd.Process.Kill()
}

// readLoop reads PTY output, strips SPAISH markers, writes to stdout,
// and emits ShellOutput events on the events channel.
func (p *PTY) readLoop() {
	defer close(p.events)

	buf := make([]byte, 4096)
	var pending []byte
	var outputBuf strings.Builder

	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			pending = p.processChunk(pending, &outputBuf)
		}
		if err != nil {
			if len(pending) > 0 {
				os.Stdout.Write(pending)
			}
			return
		}
	}
}

// processChunk handles a chunk of PTY output, returning any leftover bytes
// that may be part of an incomplete marker.
func (p *PTY) processChunk(data []byte, outputBuf *strings.Builder) []byte {
	for len(data) > 0 {
		idx := bytes.IndexByte(data, 0x00)
		if idx < 0 {
			// No null byte — forward all and buffer for output tracking
			os.Stdout.Write(data)
			outputBuf.Write(data)
			return nil
		}

		// Forward bytes before the null
		if idx > 0 {
			os.Stdout.Write(data[:idx])
			outputBuf.Write(data[:idx])
			data = data[idx:]
		}

		// data[0] == 0x00 — find the closing null
		end := bytes.IndexByte(data[1:], 0x00)
		if end < 0 {
			// Incomplete marker — hold in pending
			return data
		}

		// Extract and parse the marker
		marker := string(data[1 : end+1])
		data = data[end+2:]

		exitCode, cwd, ok := ParseMarker(marker)
		if ok {
			p.mu.Lock()
			cmd := p.lastCmd
			p.lastCmd = ""
			p.lastCWD = cwd
			p.mu.Unlock()

			output := TailTrim(strings.TrimRight(outputBuf.String(), "\r\n"), 8*1024)
			outputBuf.Reset()

			so := ShellOutput{
				Command:  cmd,
				Output:   output,
				ExitCode: exitCode,
				CWD:      cwd,
			}
			select {
			case p.events <- so:
			default:
				// consumer not keeping up; drop event to avoid stalling the PTY
			}
		}
		// Unknown null-delimited content: discard silently
	}
	return nil
}

// ParseMarker parses a "SPAISH:exitCode:cwd" string.
// Returns ok=false if the format is invalid.
func ParseMarker(s string) (exitCode int, cwd string, ok bool) {
	if !strings.HasPrefix(s, "SPAISH:") {
		return 0, "", false
	}
	rest := s[7:]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return 0, "", false
	}
	ec, err := strconv.Atoi(rest[:colon])
	if err != nil {
		return 0, "", false
	}
	return ec, rest[colon+1:], true
}

// TailTrim returns the last maxBytes bytes of s.
// If len(s) <= maxBytes, s is returned unchanged.
func TailTrim(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[len(s)-maxBytes:]
}

// HookScript returns the shell hook command to inject into the given shell.
// The hook emits \x00SPAISH:exitCode:cwd\x00 after every prompt.
func HookScript(shellBin string) string {
	base := filepath.Base(shellBin)
	switch base {
	case "zsh":
		return `stty -echo; __spaish_hook() { printf '\x00SPAISH:%d:%s\x00' "$?" "$PWD"; }; precmd_functions+=(__spaish_hook); stty echo; clear`
	default: // bash and sh
		return `stty -echo; __spaish_hook() { printf '\x00SPAISH:%d:%s\x00' "$?" "$PWD"; }; PROMPT_COMMAND="__spaish_hook;${PROMPT_COMMAND:-}"; stty echo; clear`
	}
}
