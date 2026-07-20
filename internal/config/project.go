package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Command is a user-defined slash command discovered from a
// .spai/commands/<name>.md file. Its Template body is expanded (see
// ExpandCommand) and run as a normal agent turn.
type Command struct {
	Name     string // command name for /<name>: the file stem, lower-cased
	Template string // raw markdown body of the command file
}

// DiscoverCommands walks up from workingDir to the git root, reading every
// .spai/commands/*.md file it finds and turning each into a Command. The walk
// bounds mirror findProjectSettings and the SPAI.md discovery: it checks
// workingDir first, then each parent, stopping after the directory that holds a
// .git entry. Nearer directories win on a name collision, so a command in the
// working directory shadows one of the same name higher up. A missing directory
// is a silent no-op.
func DiscoverCommands(workingDir string) ([]Command, error) {
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, nil //nolint:nilerr // bad dir → no commands
	}

	seen := make(map[string]bool)
	var cmds []Command
	for {
		dir := filepath.Join(abs, ".spai", "commands")
		if entries, readErr := os.ReadDir(dir); readErr == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if !strings.EqualFold(filepath.Ext(name), ".md") {
					continue
				}
				stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
				if stem == "" || seen[stem] {
					continue // a nearer directory already defined this command
				}
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					continue
				}
				seen[stem] = true
				cmds = append(cmds, Command{Name: stem, Template: string(data)})
			}
		}

		// Stop after the git root (already scanned that directory above).
		if _, statErr := os.Lstat(filepath.Join(abs, ".git")); statErr == nil {
			break
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break // filesystem root
		}
		abs = parent
	}
	return cmds, nil
}

// ExpandCommand substitutes the argument placeholders in a custom command
// template:
//
//   - $ARGUMENTS   → all args joined by a single space.
//   - $1, $2, …    → the 1-indexed positional arg; out-of-range → "".
//   - $$           → a literal '$'.
//
// Any other $-sequence is left untouched, so shell snippets and prose survive
// verbatim.
func ExpandCommand(template string, args []string) string {
	var b strings.Builder
	for i := 0; i < len(template); i++ {
		if template[i] != '$' || i+1 >= len(template) {
			b.WriteByte(template[i])
			continue
		}
		rest := template[i+1:]
		switch {
		case rest[0] == '$':
			b.WriteByte('$')
			i++ // consume the escaped '$'
		case strings.HasPrefix(rest, "ARGUMENTS"):
			b.WriteString(strings.Join(args, " "))
			i += len("ARGUMENTS")
		case rest[0] >= '1' && rest[0] <= '9':
			j := 0
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			if n, err := strconv.Atoi(rest[:j]); err == nil && n >= 1 && n <= len(args) {
				b.WriteString(args[n-1])
			}
			i += j // out-of-range positionals expand to ""
		default:
			b.WriteByte('$') // untouched $-sequence
		}
	}
	return b.String()
}

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
