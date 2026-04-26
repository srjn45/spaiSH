package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spaish/internal/ai"
	"spaish/internal/session"
)

func TestAppendHistoryCreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("hist")
	ts := time.Date(2026, 4, 5, 14, 32, 0, 0, time.UTC)
	s.AppendHistory(ts, "why is nginx down", "Port conflict on :80.", "")

	histPath := filepath.Join(dir, "spaish", "sessions", "hist", "history.md")
	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("history.md not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "## 2026-04-05 14:32 — user") {
		t.Errorf("expected user header, got:\n%s", content)
	}
	if !strings.Contains(content, "why is nginx down") {
		t.Errorf("expected user message, got:\n%s", content)
	}
	if !strings.Contains(content, "## 2026-04-05 14:32 — assistant") {
		t.Errorf("expected assistant header, got:\n%s", content)
	}
	if !strings.Contains(content, "Port conflict on :80.") {
		t.Errorf("expected assistant message, got:\n%s", content)
	}
}

func TestAppendHistoryOutputSectionOnlyWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("histout")
	ts := time.Date(2026, 4, 5, 14, 32, 0, 0, time.UTC)

	// No output
	s.AppendHistory(ts, "q", "a", "")
	histPath := filepath.Join(dir, "spaish", "sessions", "histout", "history.md")
	data, _ := os.ReadFile(histPath)
	if strings.Contains(string(data), "— output") {
		t.Error("output section should not appear when output is empty")
	}

	// With output
	s.AppendHistory(ts, "q2", "a2", "some command output")
	data, _ = os.ReadFile(histPath)
	if !strings.Contains(string(data), "— output") {
		t.Error("output section should appear when output is non-empty")
	}
	if !strings.Contains(string(data), "some command output") {
		t.Error("output content missing")
	}
}

func TestAppendHistoryAccumulates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("accum")
	ts := time.Date(2026, 4, 5, 14, 32, 0, 0, time.UTC)
	s.AppendHistory(ts, "first query", "first answer", "")
	s.AppendHistory(ts, "second query", "second answer", "")

	histPath := filepath.Join(dir, "spaish", "sessions", "accum", "history.md")
	data, _ := os.ReadFile(histPath)
	content := string(data)
	if !strings.Contains(content, "first query") || !strings.Contains(content, "second query") {
		t.Error("both exchanges should be in history.md")
	}
}

func TestAppendHistoryRotatesAt1MB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("rotate")
	sessDir := filepath.Join(dir, "spaish", "sessions", "rotate")
	os.MkdirAll(sessDir, 0755)

	// Pre-fill history.md just above 1 MB
	big := make([]byte, 1*1024*1024+1)
	for i := range big {
		big[i] = 'x'
	}
	os.WriteFile(filepath.Join(sessDir, "history.md"), big, 0600)

	ts := time.Date(2026, 4, 5, 14, 32, 0, 0, time.UTC)
	s.AppendHistory(ts, "trigger rotation", "response", "")

	// history.001.md should now exist (the rotated file)
	if _, err := os.Stat(filepath.Join(sessDir, "history.001.md")); err != nil {
		t.Error("history.001.md should exist after rotation")
	}
	// history.md should be a new small file
	info, err := os.Stat(filepath.Join(sessDir, "history.md"))
	if err != nil {
		t.Fatal("history.md should exist after rotation")
	}
	if info.Size() >= 1*1024*1024 {
		t.Error("history.md should be small after rotation")
	}
}

func TestAppendHistoryRotationIncrementsIndex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("rotidx")
	sessDir := filepath.Join(dir, "spaish", "sessions", "rotidx")
	os.MkdirAll(sessDir, 0755)

	// Simulate history.001.md already exists
	os.WriteFile(filepath.Join(sessDir, "history.001.md"), []byte("old"), 0600)

	big := make([]byte, 1*1024*1024+1)
	os.WriteFile(filepath.Join(sessDir, "history.md"), big, 0600)

	ts := time.Date(2026, 4, 5, 14, 32, 0, 0, time.UTC)
	s.AppendHistory(ts, "q", "a", "")

	if _, err := os.Stat(filepath.Join(sessDir, "history.002.md")); err != nil {
		t.Error("history.002.md should exist when 001 already present")
	}
}

func TestReadAllHistoryEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("nohistory")
	content, err := s.ReadAllHistory()
	if err != nil {
		t.Fatalf("ReadAllHistory() error: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty, got %q", content)
	}
}

func TestReadAllHistoryOrdering(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	sessDir := filepath.Join(dir, "spaish", "sessions", "ordering")
	os.MkdirAll(sessDir, 0755)
	os.WriteFile(filepath.Join(sessDir, "history.001.md"), []byte("first\n"), 0600)
	os.WriteFile(filepath.Join(sessDir, "history.002.md"), []byte("second\n"), 0600)
	os.WriteFile(filepath.Join(sessDir, "history.md"), []byte("third\n"), 0600)

	s, _ := session.LoadByID("ordering")
	content, err := s.ReadAllHistory()
	if err != nil {
		t.Fatalf("ReadAllHistory() error: %v", err)
	}
	if !strings.HasPrefix(content, "first\n") {
		t.Errorf("expected 'first' at start, got: %q", content[:20])
	}
	if !strings.HasSuffix(content, "third\n") {
		t.Errorf("expected 'third' at end, got: %q", content[len(content)-20:])
	}
}

func TestParseHistoryMessages(t *testing.T) {
	histText := `## 2026-04-05 14:32 — user
why is nginx down

## 2026-04-05 14:32 — assistant
Port conflict on :80.

## 2026-04-05 14:32 — output
nginx failed

## 2026-04-05 14:33 — user
how do I fix it

## 2026-04-05 14:33 — assistant
Run: sudo systemctl restart nginx

`
	msgs := session.ParseHistoryMessages(histText)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (2 user + 2 assistant), got %d", len(msgs))
	}
	if msgs[0] != (ai.Message{Role: "user", Content: "why is nginx down"}) {
		t.Errorf("unexpected msg[0]: %+v", msgs[0])
	}
	if msgs[1] != (ai.Message{Role: "assistant", Content: "Port conflict on :80."}) {
		t.Errorf("unexpected msg[1]: %+v", msgs[1])
	}
	if msgs[2] != (ai.Message{Role: "user", Content: "how do I fix it"}) {
		t.Errorf("unexpected msg[2]: %+v", msgs[2])
	}
}

func TestParseHistoryMessagesLimitTo20(t *testing.T) {
	var sb strings.Builder
	ts := "2026-04-05 14:32"
	for i := 0; i < 15; i++ {
		sb.WriteString("## " + ts + " — user\nq\n\n")
		sb.WriteString("## " + ts + " — assistant\na\n\n")
	}
	msgs := session.ParseHistoryMessages(sb.String())
	if len(msgs) > 20 {
		t.Errorf("expected max 20 messages, got %d", len(msgs))
	}
}
