package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// spaiMDMaxBytes caps SPAI.md content injected into the system prompt (32KB).
const spaiMDMaxBytes = 32 * 1024

// loadProjectContext walks upward from dir looking for a SPAI.md file. It
// checks dir itself first, then its parents. The walk stops when a SPAI.md is
// found, when the current directory contains a .git entry (after checking that
// directory for SPAI.md), or when the filesystem root is reached. A missing
// file anywhere in the walk is not an error — the function returns "" silently.
func loadProjectContext(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}

	for {
		// Check for SPAI.md before stopping at the .git boundary.
		data, err := os.ReadFile(filepath.Join(abs, "SPAI.md"))
		if err == nil {
			content := strings.TrimSpace(string(data))
			if len(content) > spaiMDMaxBytes {
				content = content[:spaiMDMaxBytes] + "\n\n[SPAI.md truncated at 32 KB]"
			}
			return content
		}

		// Stop walking once we're inside the git root (already checked above).
		if _, statErr := os.Lstat(filepath.Join(abs, ".git")); statErr == nil {
			return ""
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return "" // filesystem root
		}
		abs = parent
	}
}
