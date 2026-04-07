package session

import (
	"os"
	"path/filepath"
	"strings"
)

// PinnedPath returns the path to the pinned_session file.
func PinnedPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaios", "pinned_session")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaios", "pinned_session")
}

// ReadPinned returns the pinned session ID, or "" if no pinned session is set.
func ReadPinned() string {
	data, err := os.ReadFile(PinnedPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WritePinned writes id as the pinned session, creating parent directories as needed.
func WritePinned(id string) error {
	path := PinnedPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(id), 0600)
}

// ClearPinned removes the pinned_session file. No-op if the file does not exist.
func ClearPinned() error {
	err := os.Remove(PinnedPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
