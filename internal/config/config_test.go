package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaish/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	// Write a minimal config
	os.WriteFile(path, []byte(`
[provider]
endpoint = "https://api.example.com/v1"
api_key_env = "SPAI_API_KEY"
model = "test-model"

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder"

[routing]
passthrough_commands = ["cd", "ls"]
prefer_local = false

[permissions]
sudo_session_timeout = 300
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("got endpoint %q", cfg.Provider.Endpoint)
	}
	if cfg.Provider.Model != "test-model" {
		t.Errorf("got model %q", cfg.Provider.Model)
	}
	if cfg.Local.LocalModel != "qwen2.5-coder" {
		t.Errorf("got local model %q", cfg.Local.LocalModel)
	}
	if len(cfg.Routing.PassthroughCommands) != 2 {
		t.Errorf("got %d passthrough commands", len(cfg.Routing.PassthroughCommands))
	}
	if cfg.Permissions.SudoSessionTimeout != 300 {
		t.Errorf("got timeout %d", cfg.Permissions.SudoSessionTimeout)
	}
}

func TestLoadPermissionPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte(`
[permissions]
sudo_session_timeout = 300
allow_commands = ["git status", "go test"]

[permissions.tools]
write_file = "deny"
read_file = "allow"

[permissions.mcp_servers]
fs = "allow"
git = "confirm"
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Permissions.Tools["write_file"] != "deny" {
		t.Errorf("tools.write_file = %q, want deny", cfg.Permissions.Tools["write_file"])
	}
	if cfg.Permissions.Tools["read_file"] != "allow" {
		t.Errorf("tools.read_file = %q, want allow", cfg.Permissions.Tools["read_file"])
	}
	if cfg.Permissions.MCPServers["fs"] != "allow" {
		t.Errorf("mcp_servers.fs = %q, want allow", cfg.Permissions.MCPServers["fs"])
	}
	if cfg.Permissions.MCPServers["git"] != "confirm" {
		t.Errorf("mcp_servers.git = %q, want confirm", cfg.Permissions.MCPServers["git"])
	}
	if len(cfg.Permissions.AllowCommands) != 2 || cfg.Permissions.AllowCommands[0] != "git status" {
		t.Errorf("allow_commands = %v, want [git status go test]", cfg.Permissions.AllowCommands)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/spaid.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSpaishConfig(t *testing.T) {
	content := `
[provider]
endpoint = "https://api.example.com/v1"
api_key_env = "KEY"
model = "test-model"

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder"

[routing]
passthrough_commands = []
prefer_local = false

[permissions]
sudo_session_timeout = 300

[spaish]
shell = "/bin/bash"
error_threshold = 2
pattern_min_count = 5
context_window = 30`

	f, err := os.CreateTemp(t.TempDir(), "spaid*.toml")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Spaish.Shell != "/bin/bash" {
		t.Errorf("Shell: got %q, want %q", cfg.Spaish.Shell, "/bin/bash")
	}
	if cfg.Spaish.ErrorThreshold != 2 {
		t.Errorf("ErrorThreshold: got %d, want 2", cfg.Spaish.ErrorThreshold)
	}
	if cfg.Spaish.PatternMinCount != 5 {
		t.Errorf("PatternMinCount: got %d, want 5", cfg.Spaish.PatternMinCount)
	}
	if cfg.Spaish.ContextWindow != 30 {
		t.Errorf("ContextWindow: got %d, want 30", cfg.Spaish.ContextWindow)
	}
}

// TestLoadSandboxSection verifies the [sandbox] section parses into SandboxConfig.
func TestLoadSandboxSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte(`
[sandbox]
enabled = true
allow_network = false
allow_paths = ["/tmp", "/work"]
backend = "bwrap"
trust_allowlisted_commands = true
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Sandbox.Enabled {
		t.Errorf("Sandbox.Enabled: got false, want true")
	}
	if cfg.Sandbox.AllowNetwork {
		t.Errorf("Sandbox.AllowNetwork: got true, want false")
	}
	if len(cfg.Sandbox.AllowPaths) != 2 || cfg.Sandbox.AllowPaths[0] != "/tmp" {
		t.Errorf("Sandbox.AllowPaths: got %v", cfg.Sandbox.AllowPaths)
	}
	if cfg.Sandbox.Backend != "bwrap" {
		t.Errorf("Sandbox.Backend: got %q, want bwrap", cfg.Sandbox.Backend)
	}
	if !cfg.Sandbox.TrustAllowlistedCommands {
		t.Errorf("Sandbox.TrustAllowlistedCommands: got false, want true")
	}
}

// TestLoadSubagentSection verifies [[subagent.profiles]] entries parse into
// SubagentConfig.Profiles with all fields populated.
func TestLoadSubagentSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte(`
[[subagent.profiles]]
name         = "reviewer"
description  = "Read-only code reviewer."
system_prompt = "You are a reviewer."
tools        = ["read_file", "grep"]

[[subagent.profiles]]
name         = "deployer"
description  = "Deployment expert."
system_prompt = "You are a deployer."
tools        = ["bash", "git"]
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Subagent.Profiles) != 2 {
		t.Fatalf("Subagent.Profiles len = %d, want 2", len(cfg.Subagent.Profiles))
	}

	r := cfg.Subagent.Profiles[0]
	if r.Name != "reviewer" {
		t.Errorf("Profiles[0].Name = %q, want reviewer", r.Name)
	}
	if r.SystemPrompt != "You are a reviewer." {
		t.Errorf("Profiles[0].SystemPrompt = %q", r.SystemPrompt)
	}
	if len(r.Tools) != 2 || r.Tools[0] != "read_file" || r.Tools[1] != "grep" {
		t.Errorf("Profiles[0].Tools = %v", r.Tools)
	}

	d := cfg.Subagent.Profiles[1]
	if d.Name != "deployer" {
		t.Errorf("Profiles[1].Name = %q, want deployer", d.Name)
	}
}

// TestSubagentDefaultsEmpty verifies an absent [subagent] section yields a zero
// value — no profiles — which is valid and falls back to built-in defaults.
func TestSubagentDefaultsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte("[provider]\nmodel = \"x\"\n"), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Subagent.Profiles) != 0 {
		t.Errorf("absent [subagent] should yield 0 profiles, got %d", len(cfg.Subagent.Profiles))
	}
}

// TestSandboxDefaultsDisabled verifies an absent [sandbox] section yields the
// disabled zero value.
func TestSandboxDefaultsDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte("[provider]\nmodel = \"x\"\n"), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Sandbox.Enabled {
		t.Errorf("absent [sandbox] should be disabled, got Enabled=true")
	}
	if cfg.Sandbox.Backend != "" {
		t.Errorf("absent [sandbox] Backend should be empty, got %q", cfg.Sandbox.Backend)
	}
}

// TestLoadHooks verifies [[hooks]] array-of-tables parses into HookSpec entries
// with all fields, and that an absent [[hooks]] section yields no hooks.
func TestLoadHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte(`
[provider]
model = "x"

[[hooks]]
event       = "pre_tool"
match       = "write_file"
input_field = "path"
input_match = "^vendor/"
command     = "echo nope >&2; exit 1"
timeout_ms  = 5000

[[hooks]]
event   = "post_tool"
match   = "edit_file"
command = "gofmt -w foo.go"
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("got %d hooks, want 2", len(cfg.Hooks))
	}
	h := cfg.Hooks[0]
	if h.Event != "pre_tool" || h.Match != "write_file" || h.InputField != "path" ||
		h.InputMatch != "^vendor/" || h.Command != "echo nope >&2; exit 1" || h.TimeoutMS != 5000 {
		t.Errorf("hooks[0] = %+v", h)
	}
	if cfg.Hooks[1].Event != "post_tool" || cfg.Hooks[1].Match != "edit_file" {
		t.Errorf("hooks[1] = %+v", cfg.Hooks[1])
	}
}

// TestHooksDefaultsEmpty verifies an absent [[hooks]] section yields no hooks,
// i.e. behaviour identical to today.
func TestHooksDefaultsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte("[provider]\nmodel = \"x\"\n"), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hooks) != 0 {
		t.Errorf("absent [[hooks]] should yield 0 hooks, got %d", len(cfg.Hooks))
	}
}
