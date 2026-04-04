package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaios/internal/ai"
	"spaios/internal/session"
)

func TestSessionLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := session.LoadFrom(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected empty messages, got %d", len(s.Messages))
	}
}

func TestSessionAddAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	s, _ := session.LoadFrom(path)
	s.AddExchange("fix nginx", "I found the issue and fixed it.")

	if err := s.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Reload and verify
	s2, err := session.LoadFrom(path)
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if len(s2.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(s2.Messages))
	}
	if s2.Messages[0].Role != "user" || s2.Messages[0].Content != "fix nginx" {
		t.Errorf("unexpected first message: %+v", s2.Messages[0])
	}
}

func TestSessionTruncates(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "session.json"))

	// Add 15 exchanges = 30 messages, should truncate to 20
	for i := 0; i < 15; i++ {
		s.AddExchange("query", "response")
	}
	if len(s.Messages) > 20 {
		t.Errorf("expected max 20 messages, got %d", len(s.Messages))
	}
}

func TestSessionMessages(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "session.json"))
	s.AddExchange("hello", "hi there")

	msgs := s.MessagesForPrompt()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0] != (ai.Message{Role: "user", Content: "hello"}) {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

func TestSessionFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	s, _ := session.LoadFrom(path)
	s.AddExchange("q", "a")
	s.Save()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %v", info.Mode().Perm())
	}
}
