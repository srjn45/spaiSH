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

// Session holds the in-memory state for a session. Persisted to sessions/<id>/cache.json.
type Session struct {
	Messages  []ai.Message `json:"messages"`
	Summary   string       `json:"summary,omitempty"`
	UpdatedAt time.Time    `json:"updated_at,omitempty"`
	dir       string       // unexported: sessions/<id>/ directory
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
