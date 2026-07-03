package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ProjectSettings holds per-project overrides loaded from .spai/settings.toml.
// The TOML shape mirrors the global config's [permissions] section so users
// can copy/paste between the two files.
type ProjectSettings struct {
	Permissions PermissionsConfig `toml:"permissions"`
}

// findProjectSettings walks up from dir looking for .spai/settings.toml.
// It checks dir itself first, then each parent in turn, stopping after the
// directory that contains a .git entry or when the filesystem root is reached.
// A missing file is a silent no-op; the returned pointer is nil in that case.
func findProjectSettings(dir string) (*ProjectSettings, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil //nolint:nilerr // bad dir → treat as no settings
	}

	for {
		candidate := filepath.Join(abs, ".spai", "settings.toml")
		data, readErr := os.ReadFile(candidate)
		if readErr == nil {
			var ps ProjectSettings
			if _, decErr := toml.Decode(string(data), &ps); decErr != nil {
				return nil, decErr
			}
			return &ps, nil
		}

		// Stop after the git root (already checked that directory above).
		if _, statErr := os.Lstat(filepath.Join(abs, ".git")); statErr == nil {
			return nil, nil
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, nil // filesystem root
		}
		abs = parent
	}
}

// MergeProjectPermissions overlays project-level permission overrides on top of
// the global values. Project entries win on a per-key basis; global-only keys
// survive. AllowCommands is a union (global list first, project appended).
func MergeProjectPermissions(
	globalTools, globalMCP map[string]string,
	globalCmds []string,
	workingDir string,
) (tools, mcp map[string]string, cmds []string) {
	tools = globalTools
	mcp = globalMCP
	cmds = globalCmds

	ps, err := findProjectSettings(workingDir)
	if err != nil || ps == nil {
		return
	}

	p := ps.Permissions

	if len(p.Tools) > 0 {
		merged := make(map[string]string, len(globalTools)+len(p.Tools))
		for k, v := range globalTools {
			merged[k] = v
		}
		for k, v := range p.Tools {
			merged[k] = v
		}
		tools = merged
	}

	if len(p.MCPServers) > 0 {
		merged := make(map[string]string, len(globalMCP)+len(p.MCPServers))
		for k, v := range globalMCP {
			merged[k] = v
		}
		for k, v := range p.MCPServers {
			merged[k] = v
		}
		mcp = merged
	}

	if len(p.AllowCommands) > 0 {
		union := make([]string, 0, len(globalCmds)+len(p.AllowCommands))
		union = append(union, globalCmds...)
		union = append(union, p.AllowCommands...)
		cmds = union
	}

	return
}
