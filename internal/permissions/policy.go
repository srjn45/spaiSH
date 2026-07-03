package permissions

import "strings"

// Decision is the outcome of consulting the permission policy for a tool call.
// It is layered on top of, and takes precedence over, the tier-based gating.
type Decision int

const (
	// DecisionDefault means the policy has no opinion; fall back to the
	// tier-based behavior (confirm Write/Elevated/Destructive in manual mode).
	DecisionDefault Decision = iota
	// DecisionAllow runs the tool without confirmation, even in manual mode.
	DecisionAllow
	// DecisionConfirm forces the tier-based behavior. As an explicit entry it
	// overrides a broader allow (e.g. a per-server allow) for this tool.
	DecisionConfirm
	// DecisionDeny blocks the tool: it never executes, in any mode.
	DecisionDeny
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionConfirm:
		return "confirm"
	case DecisionDeny:
		return "deny"
	default:
		return "default"
	}
}

// Policy is a configurable gating layer resolved before the tier-based confirm
// gate. The zero value is a valid empty policy that always returns
// DecisionDefault, so an absent config reproduces exactly the legacy behavior.
type Policy struct {
	tools      map[string]Decision // tool name -> decision
	mcpServers map[string]Decision // MCP server name -> decision
	allowCmds  []string            // bash command prefixes that bypass confirm
}

// NewPolicy builds a Policy from raw config values. The tools and mcpServers
// maps map names to "allow" | "confirm" | "deny" (case-insensitive);
// unrecognized or empty values are ignored (treated as no entry). allowCommands
// is a list of bash command prefixes that bypass confirmation.
func NewPolicy(tools, mcpServers map[string]string, allowCommands []string) Policy {
	p := Policy{
		tools:      parseDecisions(tools),
		mcpServers: parseDecisions(mcpServers),
	}
	for _, c := range allowCommands {
		if c = strings.TrimSpace(c); c != "" {
			p.allowCmds = append(p.allowCmds, c)
		}
	}
	return p
}

func parseDecisions(m map[string]string) map[string]Decision {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]Decision, len(m))
	for name, v := range m {
		if d, ok := parseDecision(v); ok {
			out[name] = d
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseDecision(v string) (Decision, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "allow":
		return DecisionAllow, true
	case "confirm":
		return DecisionConfirm, true
	case "deny":
		return DecisionDeny, true
	default:
		return DecisionDefault, false
	}
}

// Decide resolves the policy decision for a tool call. bashCmd is the shell
// command for the "bash" tool (ignored otherwise). Precedence, highest first:
//
//  1. explicit per-tool entry
//  2. per-MCP-server entry (for mcp__<server>__<tool> tools)
//  3. bash command allowlist match (for the bash tool)
//  4. DecisionDefault (tier-based behavior)
func (p Policy) Decide(toolName, bashCmd string) Decision {
	if d, ok := p.tools[toolName]; ok {
		return d
	}
	if server, ok := mcpServerName(toolName); ok {
		if d, ok := p.mcpServers[server]; ok {
			return d
		}
	}
	if toolName == "bash" && p.matchesAllowCommand(bashCmd) {
		return DecisionAllow
	}
	return DecisionDefault
}

// matchesAllowCommand reports whether cmd starts with one of the allowlisted
// prefixes on a word boundary (exact match or prefix followed by whitespace),
// so "git status" does not match "git status-hack" but does match "git status -s".
func (p Policy) matchesAllowCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	for _, prefix := range p.allowCmds {
		if cmd == prefix {
			return true
		}
		if strings.HasPrefix(cmd, prefix) {
			if r := cmd[len(prefix)]; r == ' ' || r == '\t' {
				return true
			}
		}
	}
	return false
}

// mcpServerName extracts the server segment from an mcp__<server>__<tool> tool
// name. It returns ("", false) for non-MCP tool names.
func mcpServerName(toolName string) (string, bool) {
	const prefix = "mcp__"
	if !strings.HasPrefix(toolName, prefix) {
		return "", false
	}
	rest := toolName[len(prefix):]
	i := strings.Index(rest, "__")
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}
