package spaish

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"spaios/internal/protocol"
	"spaios/internal/socket"
)

// dissatisfactionPhrases trigger a full-history rethink when found in user replies.
var dissatisfactionPhrases = []string{
	"wrong", "doesn't work", "not right", "try again",
	"that's not", "still failing", "not working", "doesn't help",
	"no,", "no that", "incorrect",
}

// runPhrases trigger execution of the last suggested command.
var runPhrases = []string{"run it", "try that", "yes", "do it", "execute it", "run that"}

// Conversation manages a single interactive AI exchange session in the shell.
type Conversation struct {
	client    *socket.Client
	sessionID string
	sessDir   string // path to the session directory for reading history.md
	lastReply string // the most recent AI text, used to extract run candidates
}

// NewConversation creates a Conversation backed by the given socket client.
// sessDir is the session directory (e.g. ~/.local/share/spaios/sessions/spaish_<pid>).
func NewConversation(client *socket.Client, sessionID, sessDir string) *Conversation {
	return &Conversation{
		client:    client,
		sessionID: sessionID,
		sessDir:   sessDir,
	}
}

// IsDissatisfied returns true if msg signals the AI response was wrong or incomplete.
func IsDissatisfied(msg string) bool {
	lower := strings.ToLower(msg)
	for _, p := range dissatisfactionPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// IsRunRequest returns true if msg asks spaiSH to execute the last suggestion.
func IsRunRequest(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	for _, p := range runPhrases {
		if lower == p {
			return true
		}
	}
	return false
}

// Start initiates an AI conversation with the given initial event.
// It streams the first AI reply, then enters a follow-up loop until the user exits.
// The caller must have restored cooked terminal mode before calling Start.
func (c *Conversation) Start(initialEvent *protocol.ShellEvent) error {
	if initialEvent.Trigger == "error" {
		fmt.Printf("\n\033[2m[exit %d]\033[0m\n", initialEvent.ExitCode)
	}

	if err := c.sendEvent(initialEvent); err != nil {
		return err
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n \033[2myou\033[0m \033[1m▶\033[0m ")
		if !scanner.Scan() {
			return nil
		}
		line := strings.TrimSpace(scanner.Text())

		// Empty line or "done" exits conversation
		if line == "" || strings.EqualFold(line, "done") {
			return nil
		}

		// Run the last suggested command
		if IsRunRequest(line) {
			cmd := extractCommand(c.lastReply)
			if cmd != "" {
				fmt.Printf("$ %s\n", cmd)
				runInline(cmd)
			} else {
				fmt.Println("  (no command to run)")
			}
			continue
		}

		// Build the follow-up event
		ev := &protocol.ShellEvent{
			Trigger: "prompt",
			Query:   line,
			CWD:     initialEvent.CWD,
		}

		if IsDissatisfied(line) {
			ev.Trigger = "rethink"
			fmt.Printf("  \033[2m[reconsidering with full context]\033[0m\n")
			ev.FullHistory = c.readFullHistory()
		}

		if err := c.sendEvent(ev); err != nil {
			return err
		}
	}
}

// sendEvent sends a ShellEvent to spaid and streams the response to stdout.
func (c *Conversation) sendEvent(ev *protocol.ShellEvent) error {
	req := &protocol.Request{
		Type:      "shell",
		SessionID: c.sessionID,
		Shell:     ev,
	}

	fmt.Printf("\n \033[1mspaiSH\033[0m  ")
	var fullReply strings.Builder
	err := c.client.Send(req, func(resp protocol.Response) error {
		switch resp.Type {
		case "text":
			fmt.Print(resp.Content)
			fullReply.WriteString(resp.Content)
		case "error":
			fmt.Printf("\n[error: %s]\n", resp.Content)
		}
		return nil
	})
	fmt.Println()
	if err == nil {
		c.lastReply = fullReply.String()
	}
	return err
}

// extractCommand returns the first backtick-quoted string in reply.
// Returns empty string if no backtick command is found.
func extractCommand(reply string) string {
	start := strings.Index(reply, "`")
	if start < 0 {
		return ""
	}
	end := strings.Index(reply[start+1:], "`")
	if end < 0 {
		return ""
	}
	return reply[start+1 : start+1+end]
}

// runInline executes cmd synchronously with stdout/stderr connected to the terminal.
func runInline(cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	c.Run()
}

// readFullHistory reads history.md from the session directory.
// Returns empty string if the file does not exist or cannot be read.
func (c *Conversation) readFullHistory() string {
	data, err := os.ReadFile(filepath.Join(c.sessDir, "history.md"))
	if err != nil {
		return ""
	}
	return string(data)
}
