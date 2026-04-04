package session

import (
	"encoding/json"
	"os"
	"path/filepath"

	"spaios/internal/ai"
)

const maxMessages = 20

// Session holds the conversation history for the current daemon session.
type Session struct {
	Messages []ai.Message `json:"messages"`
	path     string
}

// DefaultPath returns the default session file path.
func DefaultPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaios", "session.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaios", "session.json")
}

// LoadFrom loads a session from the given file path.
// If the file does not exist, an empty session is returned without error.
func LoadFrom(path string) (*Session, error) {
	s := &Session{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		// Corrupted session — start fresh
		return &Session{path: path}, nil
	}
	return s, nil
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

// MessagesForPrompt returns the session messages for inclusion in the AI prompt.
func (s *Session) MessagesForPrompt() []ai.Message {
	out := make([]ai.Message, len(s.Messages))
	copy(out, s.Messages)
	return out
}

// Save writes the session to disk with mode 0600.
func (s *Session) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Clear removes all messages from the session.
func (s *Session) Clear() {
	s.Messages = nil
}
