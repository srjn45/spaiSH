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

func TestSessionsDir(t *testing.T) {
	dir := session.SessionsDir()
	if dir == "" {
		t.Error("SessionsDir() must not be empty")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("SessionsDir() must be absolute, got %q", dir)
	}
}

func TestLoadByIDNewSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, err := session.LoadByID("testshell")
	if err != nil {
		t.Fatalf("LoadByID() error: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected empty session, got %d messages", len(s.Messages))
	}
}

func TestLoadByIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("roundtrip")
	s.AddExchange("hello", "world")
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	s2, err := session.LoadByID("roundtrip")
	if err != nil {
		t.Fatalf("LoadByID() reload error: %v", err)
	}
	if len(s2.Messages) != 2 {
		t.Errorf("expected 2 messages after reload, got %d", len(s2.Messages))
	}
}

func TestLoadByIDEmptyIDUsesDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, err := session.LoadByID("")
	if err != nil {
		t.Fatalf("LoadByID('') error: %v", err)
	}
	s.AddExchange("q", "a")
	s.Save()

	defaultPath := filepath.Join(dir, "spaios", "sessions", "default.json")
	if _, err := os.Stat(defaultPath); err != nil {
		t.Errorf("expected file at %s, got error: %v", defaultPath, err)
	}
}

func TestLoadByIDCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	sessDir := filepath.Join(dir, "spaios", "sessions")
	os.MkdirAll(sessDir, 0755)
	os.WriteFile(filepath.Join(sessDir, "corrupt.json"), []byte("not json{{{"), 0600)

	s, err := session.LoadByID("corrupt")
	if err != nil {
		t.Fatalf("LoadByID corrupt: should return fresh session, got error: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected empty session from corrupt file, got %d messages", len(s.Messages))
	}
}

func TestSessionTrimKeepsLatestN(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "s.json"))

	for i := 0; i < 5; i++ {
		s.AddExchange("q", "a")
	}
	// 10 messages total; trim to 4
	s.Trim(4)
	if len(s.Messages) != 4 {
		t.Errorf("expected 4 messages after Trim(4), got %d", len(s.Messages))
	}
}

func TestSessionTrimNGreaterThanLength(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "s.json"))
	s.AddExchange("q", "a") // 2 messages

	s.Trim(100)
	if len(s.Messages) != 2 {
		t.Errorf("expected 2 messages when N > len, got %d", len(s.Messages))
	}
}

func TestSessionCompact(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "s.json"))
	s.AddExchange("first", "response1")
	s.AddExchange("second", "response2")

	s.Compact("Summary of the session.")

	if len(s.Messages) != 2 {
		t.Fatalf("expected 2 messages after Compact, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != "user" || s.Messages[0].Content != "[session summary request]" {
		t.Errorf("unexpected first message: %+v", s.Messages[0])
	}
	if s.Messages[1].Role != "assistant" || s.Messages[1].Content != "Summary of the session." {
		t.Errorf("unexpected second message: %+v", s.Messages[1])
	}
}
