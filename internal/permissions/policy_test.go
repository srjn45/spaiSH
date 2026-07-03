package permissions

import "testing"

func TestPolicyDecide(t *testing.T) {
	tests := []struct {
		name       string
		tools      map[string]string
		mcpServers map[string]string
		allowCmds  []string
		toolName   string
		bashCmd    string
		want       Decision
	}{
		{
			name: "empty policy is default",
			want: DecisionDefault,
			// zero-value maps/slice
			toolName: "write_file",
		},
		{
			name:     "per-tool allow",
			tools:    map[string]string{"write_file": "allow"},
			toolName: "write_file",
			want:     DecisionAllow,
		},
		{
			name:     "per-tool deny",
			tools:    map[string]string{"bash": "deny"},
			toolName: "bash",
			bashCmd:  "rm -rf /",
			want:     DecisionDeny,
		},
		{
			name:     "per-tool confirm",
			tools:    map[string]string{"read_file": "confirm"},
			toolName: "read_file",
			want:     DecisionConfirm,
		},
		{
			name:     "unrecognized value ignored",
			tools:    map[string]string{"write_file": "maybe"},
			toolName: "write_file",
			want:     DecisionDefault,
		},
		{
			name:       "mcp server default applies to its tools",
			mcpServers: map[string]string{"fs": "allow"},
			toolName:   "mcp__fs__read",
			want:       DecisionAllow,
		},
		{
			name:       "mcp server deny",
			mcpServers: map[string]string{"git": "deny"},
			toolName:   "mcp__git__commit",
			want:       DecisionDeny,
		},
		{
			name:       "per-tool overrides mcp server default",
			tools:      map[string]string{"mcp__fs__write": "deny"},
			mcpServers: map[string]string{"fs": "allow"},
			toolName:   "mcp__fs__write",
			want:       DecisionDeny,
		},
		{
			name:       "per-tool confirm overrides server allow",
			tools:      map[string]string{"mcp__fs__write": "confirm"},
			mcpServers: map[string]string{"fs": "allow"},
			toolName:   "mcp__fs__write",
			want:       DecisionConfirm,
		},
		{
			name:       "mcp server default does not leak to other server",
			mcpServers: map[string]string{"fs": "allow"},
			toolName:   "mcp__git__status",
			want:       DecisionDefault,
		},
		{
			name:      "bash allowlist exact match",
			allowCmds: []string{"git status"},
			toolName:  "bash",
			bashCmd:   "git status",
			want:      DecisionAllow,
		},
		{
			name:      "bash allowlist prefix with args",
			allowCmds: []string{"go test"},
			toolName:  "bash",
			bashCmd:   "go test ./...",
			want:      DecisionAllow,
		},
		{
			name:      "bash allowlist word-boundary guard",
			allowCmds: []string{"go test"},
			toolName:  "bash",
			bashCmd:   "go testx",
			want:      DecisionDefault,
		},
		{
			name:      "bash allowlist no match",
			allowCmds: []string{"git status"},
			toolName:  "bash",
			bashCmd:   "rm -rf /",
			want:      DecisionDefault,
		},
		{
			name:      "allowlist only applies to bash tool",
			allowCmds: []string{"git status"},
			toolName:  "write_file",
			bashCmd:   "git status",
			want:      DecisionDefault,
		},
		{
			name:      "per-tool bash deny beats allowlist",
			tools:     map[string]string{"bash": "deny"},
			allowCmds: []string{"git status"},
			toolName:  "bash",
			bashCmd:   "git status",
			want:      DecisionDeny,
		},
		{
			name:      "leading whitespace still matches allowlist",
			allowCmds: []string{"git status"},
			toolName:  "bash",
			bashCmd:   "  git status -s",
			want:      DecisionAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPolicy(tt.tools, tt.mcpServers, tt.allowCmds)
			if got := p.Decide(tt.toolName, tt.bashCmd); got != tt.want {
				t.Errorf("Decide(%q, %q) = %v, want %v", tt.toolName, tt.bashCmd, got, tt.want)
			}
		})
	}
}

func TestZeroPolicyIsDefault(t *testing.T) {
	var p Policy
	if got := p.Decide("write_file", ""); got != DecisionDefault {
		t.Errorf("zero Policy Decide = %v, want DecisionDefault", got)
	}
	if got := p.Decide("mcp__fs__read", ""); got != DecisionDefault {
		t.Errorf("zero Policy Decide (mcp) = %v, want DecisionDefault", got)
	}
	if got := p.Decide("bash", "git status"); got != DecisionDefault {
		t.Errorf("zero Policy Decide (bash) = %v, want DecisionDefault", got)
	}
}

func TestMCPServerName(t *testing.T) {
	tests := []struct {
		in     string
		server string
		ok     bool
	}{
		{"mcp__fs__read", "fs", true},
		{"mcp__git__log", "git", true},
		{"mcp__fs__sub__tool", "fs", true}, // tool part may contain __
		{"read_file", "", false},
		{"mcp__", "", false},
		{"mcp__fs", "", false},
		{"mcp____read", "", false}, // empty server segment
	}
	for _, tt := range tests {
		server, ok := mcpServerName(tt.in)
		if ok != tt.ok || server != tt.server {
			t.Errorf("mcpServerName(%q) = (%q, %v), want (%q, %v)", tt.in, server, ok, tt.server, tt.ok)
		}
	}
}

func TestDecisionString(t *testing.T) {
	cases := map[Decision]string{
		DecisionDefault: "default",
		DecisionAllow:   "allow",
		DecisionConfirm: "confirm",
		DecisionDeny:    "deny",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("Decision(%d).String() = %q, want %q", d, got, want)
		}
	}
}
