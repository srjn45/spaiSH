package session

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"spaish/internal/ai"
)

const maxMessages = 20

// ActualUsage holds cumulative API-reported token counts for a session,
// accumulated from real provider responses. Zero values indicate that no
// provider has reported usage yet (e.g. an old session or a non-Anthropic
// provider). Check HasData before using.
type ActualUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"` // tokens written to the prompt cache
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`     // tokens served from the prompt cache
}

// HasData reports whether any real API usage has been recorded.
func (u ActualUsage) HasData() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0
}

// Session holds the in-memory state for a session. Persisted to sessions/<id>/cache.json.
type Session struct {
	Messages    []ai.Message `json:"messages"`
	Summary     string       `json:"summary,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at,omitempty"`
	ActualUsage ActualUsage  `json:"actual_usage,omitempty"`
	dir         string       // unexported: sessions/<id>/ directory
}

// SessionsDir returns the directory where per-session subdirectories are stored.
func SessionsDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaish", "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaish", "sessions")
}

// cachePath returns the path to this session's cache.json.
func (s *Session) cachePath() string {
	return filepath.Join(s.dir, "cache.json")
}

// Dir returns the session directory path (exported for use by history helpers).
func (s *Session) Dir() string {
	return s.dir
}

// LoadByID loads the session for the given ID from SessionsDir.
// An empty id falls back to "default".
// Transparently migrates the old flat layout (sessions/<id>.json → sessions/<id>/cache.json).
// Returns a fresh empty session if the cache does not exist.
func LoadByID(id string) (*Session, error) {
	if id == "" {
		id = "default"
	}
	dir := filepath.Join(SessionsDir(), id)

	// Migrate flat layout if the old single-file format exists.
	flatPath := filepath.Join(SessionsDir(), id+".json")
	if _, err := os.Stat(flatPath); err == nil {
		if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
			return nil, mkErr
		}
		if mvErr := os.Rename(flatPath, filepath.Join(dir, "cache.json")); mvErr != nil {
			return nil, mvErr
		}
		// Create an empty history.md to mark the migration point.
		os.WriteFile(filepath.Join(dir, "history.md"), nil, 0600)
	}

	s := &Session{dir: dir}
	data, err := os.ReadFile(s.cachePath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("session: corrupt cache %s, starting fresh", s.cachePath())
		return &Session{dir: dir}, nil
	}
	return s, nil
}

// SaveCache writes the session cache (messages + summary) to sessions/<id>/cache.json.
func (s *Session) SaveCache() error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(s.cachePath(), data, 0600)
}

// AddExchange appends a user/assistant exchange and trims to maxMessages.
func (s *Session) AddExchange(userMsg, assistantMsg string) {
	s.Messages = append(s.Messages,
		ai.Message{Role: "user", Content: userMsg},
		ai.Message{Role: "assistant", Content: assistantMsg},
	)
	if len(s.Messages) > maxMessages {
		s.Messages = s.Messages[len(s.Messages)-maxMessages:]
	}
}

// MessagesForPrompt returns messages for the AI prompt.
// If Summary is non-empty, it is prepended as a synthetic assistant message.
func (s *Session) MessagesForPrompt() []ai.Message {
	var out []ai.Message
	if s.Summary != "" {
		out = append(out, ai.Message{Role: "assistant", Content: "[context summary]\n" + s.Summary})
	}
	out = append(out, s.Messages...)
	return out
}

// SetSummary sets the session summary field. Used by rebuild-context and tests.
func (s *Session) SetSummary(summary string) {
	s.Summary = summary
}

// Trim keeps the latest n messages, discarding older ones.
// If n <= 0 or n >= len(Messages), this is a no-op.
func (s *Session) Trim(n int) {
	if n <= 0 || len(s.Messages) <= n {
		return
	}
	s.Messages = s.Messages[len(s.Messages)-n:]
}

// Compact replaces all messages with a summary stored in the Summary field.
// MessagesForPrompt will inject it as a synthetic assistant context message.
func (s *Session) Compact(summary string) {
	s.Messages = nil
	s.Summary = summary
}

// ApproxTokens estimates the token footprint of the session's prompt context
// (summary + messages) using a ~4-chars-per-token heuristic. Used to decide
// when to auto-compact; it is an estimate, not an exact count.
func (s *Session) ApproxTokens() int {
	chars := len(s.Summary)
	for _, m := range s.Messages {
		chars += len(m.Content)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Input)
		}
		for _, tr := range m.ToolResults {
			chars += len(tr.Content)
		}
	}
	return chars / 4
}

// Usage is an estimate-based breakdown of a session's token footprint, derived
// from stored message content via the same ~4-chars-per-token heuristic as
// ApproxTokens. The split between prompt (input) and generated (output) tokens
// is approximate: user turns, tool results and the summary count as prompt;
// assistant turns and tool-call arguments count as generated.
type Usage struct {
	PromptTokens    int // user + tool-result + summary content
	GeneratedTokens int // assistant + tool-call content
}

// TotalTokens returns the combined prompt and generated token estimate.
func (u Usage) TotalTokens() int { return u.PromptTokens + u.GeneratedTokens }

// EstimateUsage returns an estimated prompt/generated token breakdown for the
// session using the ~4-chars-per-token heuristic. Its total matches
// ApproxTokens; the prompt/generated split is a best-effort classification by
// message role and is intended for reporting, not billing.
func (s *Session) EstimateUsage() Usage {
	promptChars := len(s.Summary)
	genChars := 0
	for _, m := range s.Messages {
		if m.Role == "assistant" {
			genChars += len(m.Content)
		} else {
			promptChars += len(m.Content)
		}
		for _, tc := range m.ToolCalls {
			genChars += len(tc.Input)
		}
		for _, tr := range m.ToolResults {
			promptChars += len(tr.Content)
		}
	}
	return Usage{PromptTokens: promptChars / 4, GeneratedTokens: genChars / 4}
}

// AddActualUsage accumulates API-reported token counts into the session.
// The updated total is persisted when SaveCache is next called.
func (s *Session) AddActualUsage(u ai.Usage) {
	s.ActualUsage.InputTokens += u.InputTokens
	s.ActualUsage.OutputTokens += u.OutputTokens
	s.ActualUsage.CacheCreationTokens += u.CacheCreationTokens
	s.ActualUsage.CacheReadTokens += u.CacheReadTokens
}

// CompactOlder stores summary as the session summary and keeps only the most
// recent keep messages, discarding the rest. Used by auto-compaction.
func (s *Session) CompactOlder(summary string, keep int) {
	if keep < 0 {
		keep = 0
	}
	if keep > len(s.Messages) {
		keep = len(s.Messages)
	}
	recent := append([]ai.Message(nil), s.Messages[len(s.Messages)-keep:]...)
	s.Summary = summary
	s.Messages = recent
}

// LatestSessionID returns the ID of the most recently modified session, or ""
// if there are none.
func LatestSessionID() string {
	list, err := ListSessions()
	if err != nil || len(list) == 0 {
		return ""
	}
	latest := list[0]
	for _, s := range list[1:] {
		if s.ModTime.After(latest.ModTime) {
			latest = s
		}
	}
	return latest.ID
}

// Clear removes all in-memory state and deletes the session directory from disk.
func (s *Session) Clear() error {
	s.Messages = nil
	s.Summary = ""
	return os.RemoveAll(s.dir)
}

// SessionSummary holds metadata for a session used by the `spai sessions` listing.
type SessionSummary struct {
	ID       string
	MsgCount int
	ModTime  time.Time // mtime of history.md, or dir mtime if no history.md
}

// ListSessions returns a summary of all sessions in SessionsDir.
func ListSessions() ([]SessionSummary, error) {
	dir := SessionsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var result []SessionSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		sessDir := filepath.Join(dir, id)

		// Read message count from cache.json.
		msgCount := 0
		data, err := os.ReadFile(filepath.Join(sessDir, "cache.json"))
		if err == nil {
			var cache struct {
				Messages []json.RawMessage `json:"messages"`
			}
			if json.Unmarshal(data, &cache) == nil {
				msgCount = len(cache.Messages)
			}
		}

		// ModTime: prefer history.md mtime; fall back to dir mtime.
		var modTime time.Time
		if info, err := e.Info(); err == nil {
			modTime = info.ModTime()
		}
		if hinfo, err := os.Stat(filepath.Join(sessDir, "history.md")); err == nil {
			modTime = hinfo.ModTime()
		}

		result = append(result, SessionSummary{
			ID:       id,
			MsgCount: msgCount,
			ModTime:  modTime,
		})
	}
	return result, nil
}
