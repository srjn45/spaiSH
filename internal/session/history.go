package session

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"spaios/internal/ai"
)

const historyRotateSize = 1 * 1024 * 1024 // 1 MB

// AppendHistory appends a markdown exchange block to history.md for this session.
// Rotates history.md to history.NNN.md if it exceeds 1 MB before writing.
// Errors are logged but never returned — history writes are always silent/background.
func (s *Session) AppendHistory(t time.Time, userMsg, assistantMsg, output string) {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		log.Printf("session: history mkdir error: %v", err)
		return
	}
	if err := s.maybeRotate(); err != nil {
		log.Printf("session: history rotate error: %v", err)
	}

	ts := t.UTC().Format("2006-01-02 15:04")
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — user\n%s\n\n", ts, userMsg)
	fmt.Fprintf(&b, "## %s — assistant\n%s\n\n", ts, assistantMsg)
	if output != "" {
		fmt.Fprintf(&b, "## %s — output\n%s\n\n", ts, output)
	}

	f, err := os.OpenFile(filepath.Join(s.dir, "history.md"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("session: history open error: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		log.Printf("session: history write error: %v", err)
	}
}

// maybeRotate renames history.md to history.NNN.md if it exceeds historyRotateSize.
func (s *Session) maybeRotate() error {
	histPath := filepath.Join(s.dir, "history.md")
	info, err := os.Stat(histPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() < historyRotateSize {
		return nil
	}

	// Find next available three-digit index.
	existing, _ := filepath.Glob(filepath.Join(s.dir, "history.???.md"))
	idx := len(existing) + 1
	dest := filepath.Join(s.dir, fmt.Sprintf("history.%03d.md", idx))
	return os.Rename(histPath, dest)
}

// ReadAllHistory concatenates all history files for the session, oldest to newest
// (history.001.md → history.002.md → ... → history.md).
// Returns an empty string if no history exists.
func (s *Session) ReadAllHistory() (string, error) {
	files, err := historyFiles(s.dir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}

	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		b.Write(data)
	}
	return b.String(), nil
}

// historyFiles returns the ordered list of history file paths for a session directory:
// history.001.md, history.002.md, ..., history.md (current).
func historyFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var numbered []string
	var current string
	for _, e := range entries {
		name := e.Name()
		if name == "history.md" {
			current = filepath.Join(dir, name)
		} else if strings.HasPrefix(name, "history.") && strings.HasSuffix(name, ".md") {
			numbered = append(numbered, filepath.Join(dir, name))
		}
	}
	sort.Strings(numbered)
	if current != "" {
		numbered = append(numbered, current)
	}
	return numbered, nil
}

// ParseHistoryMessages parses history markdown content and returns the last maxMessages
// user/assistant messages. Output sections are ignored (they are not AI context).
func ParseHistoryMessages(text string) []ai.Message {
	var msgs []ai.Message

	var currentRole string
	var currentContent strings.Builder

	flush := func() {
		if currentRole == "user" || currentRole == "assistant" {
			content := strings.TrimSpace(currentContent.String())
			if content != "" {
				msgs = append(msgs, ai.Message{Role: currentRole, Content: content})
			}
		}
		currentContent.Reset()
		currentRole = ""
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") && strings.Contains(line, " — ") {
			flush()
			parts := strings.SplitN(line, " — ", 2)
			if len(parts) == 2 {
				currentRole = strings.TrimSpace(parts[1])
			}
			continue
		}
		currentContent.WriteString(line + "\n")
	}
	flush()

	if len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	return msgs
}
