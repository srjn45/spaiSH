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
