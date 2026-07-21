package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/protocol"
)

func TestConfigPath(t *testing.T) {
	t.Run("with XDG_CONFIG_HOME", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		want := filepath.Join(dir, "spaish", "spaid.toml")
		if got := ConfigPath(); got != want {
			t.Errorf("ConfigPath() = %q, want %q", got, want)
		}
	})

	t.Run("without XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := ConfigPath()
		if !strings.HasSuffix(got, filepath.Join(".config", "spaish", "spaid.toml")) {
			t.Errorf("ConfigPath() = %q, want a path ending in .config/spaish/spaid.toml", got)
		}
	})
}

func TestNewAppliesDefaultsWithoutConfig(t *testing.T) {
	// Point config/data dirs at empty temp dirs so no real config is read and
	// nothing is written under the user's home.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
	// With no config file, the Anthropic defaults apply.
	if a.activeName != "anthropic" {
		t.Errorf("activeName = %q, want anthropic", a.activeName)
	}
	if a.activeModel != ai.DefaultAnthropicModel {
		t.Errorf("activeModel = %q, want %q", a.activeModel, ai.DefaultAnthropicModel)
	}
	if a.cloud == nil || a.local == nil || a.llmMgr == nil {
		t.Error("expected cloud, local, and llmMgr to be wired")
	}
	if a.cloud.Name() != "anthropic" {
		t.Errorf("cloud provider = %q, want anthropic", a.cloud.Name())
	}
}

func TestCloudNameModel(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		wantName  string
		wantModel string
	}{
		{
			name: "openai kind",
			cfg: &config.Config{Provider: config.ProviderConfig{
				Kind: "openai", Model: "gpt-4o",
			}},
			wantName:  "openai",
			wantModel: "gpt-4o",
		},
		{
			name: "anthropic explicit model",
			cfg: &config.Config{Provider: config.ProviderConfig{
				Kind: "anthropic", Model: "claude-custom",
			}},
			wantName:  "anthropic",
			wantModel: "claude-custom",
		},
		{
			name:      "anthropic empty model falls back to default",
			cfg:       &config.Config{},
			wantName:  "anthropic",
			wantModel: ai.DefaultAnthropicModel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, model := cloudNameModel(tt.cfg)
			if name != tt.wantName || model != tt.wantModel {
				t.Errorf("cloudNameModel() = %q/%q, want %q/%q", name, model, tt.wantName, tt.wantModel)
			}
		})
	}
}

func TestBuildCloudProvider(t *testing.T) {
	t.Run("openai kind", func(t *testing.T) {
		cfg := &config.Config{Provider: config.ProviderConfig{
			Kind: "openai", Endpoint: "https://example/v1", Model: "gpt-4o",
		}}
		p := buildCloudProvider(cfg)
		if p.Name() != "openai" {
			t.Errorf("provider = %q, want openai", p.Name())
		}
	})

	t.Run("anthropic default kind", func(t *testing.T) {
		cfg := &config.Config{}
		p := buildCloudProvider(cfg)
		if p.Name() != "anthropic" {
			t.Errorf("provider = %q, want anthropic", p.Name())
		}
	})

	t.Run("key falls back to ANTHROPIC_API_KEY env", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "sk-from-env")
		cfg := &config.Config{} // no APIKeyEnv configured, so cfg.APIKey() == ""
		p := buildCloudProvider(cfg)
		if !p.Available() {
			t.Error("expected provider to be available via ANTHROPIC_API_KEY fallback")
		}
	})

	t.Run("anthropic without any key is unavailable", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "")
		cfg := &config.Config{}
		p := buildCloudProvider(cfg)
		if p.Available() {
			t.Error("expected provider to be unavailable with no key")
		}
	})
}

func TestActiveProvider(t *testing.T) {
	a := newTestApp()
	if a.activeProvider() != a.cloud {
		t.Error("expected activeProvider() to return cloud when no override is set")
	}

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	if _, err := a.SetModel([]string{"anthropic", "claude-override"}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if a.activeProvider() != a.override {
		t.Error("expected activeProvider() to return the override once set")
	}
}

func TestProviderInfo(t *testing.T) {
	t.Run("ready with model", func(t *testing.T) {
		a := newTestApp() // cloud has key "sk-test" → available
		info := a.ProviderInfo()
		if !strings.Contains(info, "anthropic") || !strings.Contains(info, "ready") {
			t.Errorf("ProviderInfo() = %q, want it to mention anthropic and ready", info)
		}
		if !strings.Contains(info, "/") {
			t.Errorf("ProviderInfo() = %q, want provider/model form", info)
		}
	})

	t.Run("not configured with empty model", func(t *testing.T) {
		cfg := &config.Config{}
		a := &App{
			cfg:         cfg,
			cloud:       ai.NewAnthropicProvider("", "", ai.RetryConfig{}), // no key → not configured
			activeName:  "anthropic",
			activeModel: "", // empty model → no "/model" segment
		}
		info := a.ProviderInfo()
		if !strings.Contains(info, "not configured") {
			t.Errorf("ProviderInfo() = %q, want not configured", info)
		}
		if strings.Contains(info, "/") {
			t.Errorf("ProviderInfo() = %q, want no model segment when activeModel is empty", info)
		}
	})
}

func TestProviders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Routing.PreferLocal = true
	cfg.Provider.APIKeyEnv = "MY_KEY_ENV"
	a := newTestApp()
	a.cfg = cfg

	set := a.providers()
	if set.Cloud != a.cloud {
		t.Error("providers().Cloud mismatch")
	}
	if set.Local != a.local {
		t.Error("providers().Local mismatch")
	}
	if set.Override != nil {
		t.Error("providers().Override should be nil before any /model switch")
	}
	if !set.PreferLocal {
		t.Error("providers().PreferLocal should reflect cfg.Routing.PreferLocal")
	}
	if set.APIKeyEnv != "MY_KEY_ENV" {
		t.Errorf("providers().APIKeyEnv = %q, want MY_KEY_ENV", set.APIKeyEnv)
	}

	// After an override is set, it should surface in the ProviderSet.
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	if _, err := a.SetModel([]string{"anthropic", "claude-x"}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if a.providers().Override == nil {
		t.Error("providers().Override should be set after SetModel")
	}
}

func TestLoadSession(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	t.Run("empty id falls back to default", func(t *testing.T) {
		sess := loadSession("")
		if sess == nil {
			t.Fatal("loadSession(\"\") returned nil")
		}
		if len(sess.Messages) != 0 {
			t.Errorf("expected fresh empty session, got %d messages", len(sess.Messages))
		}
	})

	t.Run("nonexistent id returns fresh session", func(t *testing.T) {
		sess := loadSession("does-not-exist")
		if sess == nil {
			t.Fatal("loadSession returned nil for nonexistent id")
		}
		if len(sess.Messages) != 0 {
			t.Errorf("expected fresh empty session, got %d messages", len(sess.Messages))
		}
	})
}

func TestMCPZeroServers(t *testing.T) {
	a := newTestApp() // cfg has no MCP servers

	if got := a.MCPServerCount(); got != 0 {
		t.Errorf("MCPServerCount() = %d, want 0", got)
	}
	if a.MCPLoaded() {
		t.Error("MCPLoaded() should be false before the first discovery")
	}

	status := a.MCPStatus()
	if len(status) != 0 {
		t.Errorf("MCPStatus() = %v, want empty", status)
	}
	if !a.MCPLoaded() {
		t.Error("MCPLoaded() should flip to true after MCPStatus()")
	}
}

func TestCloseNoServers(t *testing.T) {
	a := newTestApp() // no MCP servers spawned; mcpMgr/mcpCancel are nil
	if err := a.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
	// Safe to call more than once.
	if err := a.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}

func TestActiveModelGetter(t *testing.T) {
	a := newTestApp()
	if got := a.ActiveModel(); got != a.activeModel {
		t.Errorf("ActiveModel() = %q, want %q", got, a.activeModel)
	}
}

// TestRunAgentFailsClosedOnBadHook verifies a misconfigured [[hooks]] entry
// aborts RunAgent rather than silently dropping the guard rail (fail closed,
// mirroring the sandbox-init behaviour).
func TestRunAgentFailsClosedOnBadHook(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := newTestApp() // cloud provider is available (has API key)
	a.cfg.Hooks = []config.HookSpec{
		{Event: "pre_tool", Match: "[", Command: "true"}, // "[" is an invalid glob
	}
	err := a.RunAgent(
		context.Background(),
		&protocol.Request{Agent: &protocol.AgentRequest{Query: "hi"}, WorkingDir: t.TempDir()},
		func(protocol.ConfirmRequest) bool { return true },
		func(protocol.Response) {},
	)
	if err == nil || !strings.Contains(err.Error(), "hooks init") {
		t.Errorf("RunAgent with a bad hook = %v, want a hooks init error", err)
	}
}
