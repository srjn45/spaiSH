package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spaish/internal/ai"
	"spaish/internal/session"
)

// ---------- LoadByID — new directory layout ----------

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
	if err := s.SaveCache(); err != nil {
		t.Fatalf("SaveCache() error: %v", err)
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
	s.SaveCache()

	cachePath := filepath.Join(dir, "spaish", "sessions", "default", "cache.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache.json at %s, got error: %v", cachePath, err)
	}
}

func TestLoadByIDCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	sessDir := filepath.Join(dir, "spaish", "sessions", "corrupt")
	os.MkdirAll(sessDir, 0755)
	os.WriteFile(filepath.Join(sessDir, "cache.json"), []byte("not json{{{"), 0600)

	s, err := session.LoadByID("corrupt")
	if err != nil {
		t.Fatalf("LoadByID corrupt: should return fresh session, got error: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected empty session from corrupt file, got %d messages", len(s.Messages))
	}
}

// ---------- Migration: flat .json → directory ----------

func TestLoadByIDMigratesFlat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	// Create a flat sessions/migrate.json file
	sessionsDir := filepath.Join(dir, "spaish", "sessions")
	os.MkdirAll(sessionsDir, 0755)
	flatData, _ := json.Marshal(map[string]interface{}{
		"messages": []ai.Message{
			{Role: "user", Content: "old msg"},
			{Role: "assistant", Content: "old reply"},
		},
	})
	os.WriteFile(filepath.Join(sessionsDir, "migrate.json"), flatData, 0600)

	s, err := session.LoadByID("migrate")
	if err != nil {
		t.Fatalf("LoadByID() error: %v", err)
	}
	if len(s.Messages) != 2 {
		t.Errorf("expected 2 migrated messages, got %d", len(s.Messages))
	}

	// Flat file should be gone
	if _, err := os.Stat(filepath.Join(sessionsDir, "migrate.json")); !os.IsNotExist(err) {
		t.Error("flat .json file should have been removed after migration")
	}
	// cache.json should exist in new dir
	if _, err := os.Stat(filepath.Join(sessionsDir, "migrate", "cache.json")); err != nil {
		t.Errorf("cache.json not found after migration: %v", err)
	}
}

// ---------- AddExchange + truncation ----------

func TestSessionAddAndSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("addtest")
	s.AddExchange("fix nginx", "I found the issue and fixed it.")
	if err := s.SaveCache(); err != nil {
		t.Fatalf("SaveCache() error: %v", err)
	}

	s2, err := session.LoadByID("addtest")
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
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("trunctest")
	for i := 0; i < 15; i++ {
		s.AddExchange("query", "response")
	}
	if len(s.Messages) > 20 {
		t.Errorf("expected max 20 messages, got %d", len(s.Messages))
	}
}

// ---------- MessagesForPrompt — summary injection ----------

func TestMessagesForPromptNoSummary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("nosummary")
	s.AddExchange("hello", "hi")

	msgs := s.MessagesForPrompt()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

func TestMessagesForPromptWithSummary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("withsummary")
	s.AddExchange("new question", "new answer")

	// Manually set summary (normally set by Compact or rebuild-context)
	s.SetSummary("Prior work: fixed nginx port conflict.")

	msgs := s.MessagesForPrompt()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (summary + exchange), got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected first message role 'assistant', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "[context summary]\nPrior work: fixed nginx port conflict." {
		t.Errorf("unexpected summary message content: %q", msgs[0].Content)
	}
}

// ---------- Trim ----------

func TestSessionTrimKeepsLatestN(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("trimtest")
	for i := 0; i < 5; i++ {
		s.AddExchange("q", "a")
	}
	s.Trim(4)
	if len(s.Messages) != 4 {
		t.Errorf("expected 4 messages after Trim(4), got %d", len(s.Messages))
	}
}

func TestSessionTrimNGreaterThanLength(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("trimtest2")
	s.AddExchange("q", "a") // 2 messages
	s.Trim(100)
	if len(s.Messages) != 2 {
		t.Errorf("expected 2 messages when N > len, got %d", len(s.Messages))
	}
}

// ---------- Compact ----------

func TestSessionCompact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("compact")
	s.AddExchange("first", "response1")
	s.AddExchange("second", "response2")
	s.Compact("Summary of the session.")

	if len(s.Messages) != 0 {
		t.Errorf("expected 0 messages after Compact, got %d", len(s.Messages))
	}
	msgs := s.MessagesForPrompt()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (summary) from MessagesForPrompt, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "[context summary]\nSummary of the session." {
		t.Errorf("unexpected compact message: %+v", msgs[0])
	}
}

// ---------- Clear ----------

func TestSessionClearRemovesDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("cleartest")
	s.AddExchange("q", "a")
	s.SaveCache()

	sessDir := filepath.Join(dir, "spaish", "sessions", "cleartest")
	if _, err := os.Stat(sessDir); err != nil {
		t.Fatalf("session dir should exist before clear: %v", err)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear() error: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Error("expected empty messages after Clear")
	}
	if _, err := os.Stat(sessDir); !os.IsNotExist(err) {
		t.Error("session dir should be removed after Clear")
	}
}

func TestSessionClearNonExistentDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("notexist")
	// Never saved — clear should succeed silently
	if err := s.Clear(); err != nil {
		t.Errorf("Clear() on non-existent dir should not error: %v", err)
	}
}

// ---------- SaveCache file permissions ----------

func TestSaveCacheFilePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s, _ := session.LoadByID("perms")
	s.AddExchange("q", "a")
	s.SaveCache()

	cachePath := filepath.Join(dir, "spaish", "sessions", "perms", "cache.json")
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %v", info.Mode().Perm())
	}
}

// ---------- SaveCache stores UpdatedAt ----------

func TestSaveCacheSetsUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	before := time.Now().UTC().Add(-time.Second)
	s, _ := session.LoadByID("timestamp")
	s.AddExchange("q", "a")
	s.SaveCache()

	s2, _ := session.LoadByID("timestamp")
	if s2.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set after SaveCache")
	}
	if s2.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt %v is before test start %v", s2.UpdatedAt, before)
	}
}

// ---------- SessionsDir ----------

func TestSessionsDir(t *testing.T) {
	d := session.SessionsDir()
	if d == "" {
		t.Error("SessionsDir() must not be empty")
	}
	if !filepath.IsAbs(d) {
		t.Errorf("SessionsDir() must be absolute, got %q", d)
	}
}

// ---------- ListSessions ----------

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s1, _ := session.LoadByID("alpha")
	s1.AddExchange("q", "a")
	s1.AddExchange("q2", "a2")
	s1.SaveCache()

	s2, _ := session.LoadByID("beta")
	s2.SaveCache()

	list, err := session.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}

	byID := make(map[string]session.SessionSummary)
	for _, s := range list {
		byID[s.ID] = s
	}
	if byID["alpha"].MsgCount != 4 {
		t.Errorf("expected 4 messages for alpha, got %d", byID["alpha"].MsgCount)
	}
	if byID["beta"].MsgCount != 0 {
		t.Errorf("expected 0 messages for beta, got %d", byID["beta"].MsgCount)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	list, err := session.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() on empty dir error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(list))
	}
}

func TestApproxTokensAndCompactOlder(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("compact")
	for i := 0; i < 6; i++ {
		s.AddExchange("question number with some length", "answer with some content here too")
	}
	if s.ApproxTokens() == 0 {
		t.Error("expected non-zero token estimate")
	}
	before := len(s.Messages)
	s.CompactOlder("summary of older turns", 2)
	if len(s.Messages) != 2 {
		t.Errorf("expected 2 retained messages, got %d (was %d)", len(s.Messages), before)
	}
	if s.Summary != "summary of older turns" {
		t.Errorf("summary not set: %q", s.Summary)
	}
	// The summary must surface in the prompt context.
	prompt := s.MessagesForPrompt()
	if len(prompt) != 3 || prompt[0].Role != "assistant" {
		t.Errorf("expected summary + 2 messages in prompt, got %d", len(prompt))
	}
}

func TestLatestSessionID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, _ := session.LoadByID("old")
	a.AddExchange("q", "a")
	a.SaveCache()
	b, _ := session.LoadByID("new")
	b.AddExchange("q", "a")
	b.SaveCache()
	if got := session.LatestSessionID(); got == "" {
		t.Error("expected a latest session id")
	}
}

// ---------- EstimateUsage ----------

func TestEstimateUsage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("usage")
	s.SetSummary(strings.Repeat("s", 40))                 // 40 chars → prompt
	s.AddExchange(strings.Repeat("u", 80), strings.Repeat("a", 120)) // 80 prompt, 120 gen
	s.Messages = append(s.Messages, ai.Message{
		Role:        "assistant",
		ToolCalls:   []ai.ToolCall{{Input: json.RawMessage(strings.Repeat("x", 40))}},
		ToolResults: []ai.ToolResult{{Content: strings.Repeat("r", 20)}},
	})

	u := s.EstimateUsage()
	// prompt: 40 (summary) + 80 (user) + 20 (tool result) = 140 → 35 tokens
	if u.PromptTokens != 35 {
		t.Errorf("PromptTokens = %d, want 35", u.PromptTokens)
	}
	// generated: 120 (assistant) + 40 (tool call) = 160 → 40 tokens
	if u.GeneratedTokens != 40 {
		t.Errorf("GeneratedTokens = %d, want 40", u.GeneratedTokens)
	}
	if u.TotalTokens() != 75 {
		t.Errorf("TotalTokens = %d, want 75", u.TotalTokens())
	}
	// Total should track ApproxTokens (both use the same char basis).
	if u.TotalTokens() != s.ApproxTokens() {
		t.Errorf("TotalTokens %d != ApproxTokens %d", u.TotalTokens(), s.ApproxTokens())
	}
}
