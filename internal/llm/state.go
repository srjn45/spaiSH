package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultModel = "qwen2.5-coder:7b"

// RuntimeInfo records what spaiOS knows about an installed runtime.
type RuntimeInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Endpoint  string `json:"endpoint"`
}

// State holds LLM manager state, persisted to ~/.config/spaios/llm-state.json.
type State struct {
	ActiveRuntime string                 `json:"active_runtime"`
	ActiveModel   string                 `json:"active_model"`
	Runtimes      map[string]RuntimeInfo `json:"runtimes"`
	LastUpdated   time.Time              `json:"last_updated"`

	path string
	mu   sync.Mutex
}

// DefaultStatePath returns the canonical state file location.
func DefaultStatePath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaios", "llm-state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaios", "llm-state.json")
}

// LoadState reads state from path. Returns a default state if the file does not exist.
func LoadState(path string) (*State, error) {
	s := &State{
		path:          path,
		ActiveRuntime: "ollama",
		ActiveModel:   defaultModel,
		Runtimes:      make(map[string]RuntimeInfo),
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		// Corrupted — return a fresh default state without error
		s.Runtimes = make(map[string]RuntimeInfo)
		return s, nil
	}
	if s.Runtimes == nil {
		s.Runtimes = make(map[string]RuntimeInfo)
	}
	return s, nil
}

// Save writes state to disk with mode 0600.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdated = time.Now()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// SetRuntime records a runtime as installed.
func (s *State) SetRuntime(name, version, endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Runtimes[name] = RuntimeInfo{Installed: true, Version: version, Endpoint: endpoint}
	s.ActiveRuntime = name
}

// SetActiveModel sets the active model name.
func (s *State) SetActiveModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ActiveModel = model
}
