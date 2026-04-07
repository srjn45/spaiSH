package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaios/internal/config"
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

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/spaid.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFuseConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	os.WriteFile(path, []byte(`
[provider]
endpoint = "https://api.example.com/v1"
api_key_env = "SPAI_API_KEY"
model = "test-model"

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder"

[routing]
passthrough_commands = ["cd"]
prefer_local = false

[permissions]
sudo_session_timeout = 300

[fuse]
auto_mount = true
mountpoint = "/ai"
timeout_seconds = 90
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Fuse.AutoMount {
		t.Error("expected auto_mount = true")
	}
	if cfg.Fuse.Mountpoint != "/ai" {
		t.Errorf("mountpoint: got %q want %q", cfg.Fuse.Mountpoint, "/ai")
	}
	if cfg.Fuse.TimeoutSeconds != 90 {
		t.Errorf("timeout_seconds: got %d want 90", cfg.Fuse.TimeoutSeconds)
	}
}
