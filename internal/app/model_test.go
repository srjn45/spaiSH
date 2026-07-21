package app

import (
	"testing"

	"spaish/internal/ai"
	"spaish/internal/config"
)

// newTestApp builds an App with an Anthropic cloud provider (available without
// any network access, since availability is just "has API key").
func newTestApp() *App {
	cfg := &config.Config{}
	name, model := cloudNameModel(cfg)
	return &App{
		cfg:         cfg,
		cloud:       ai.NewAnthropicProvider("sk-test", model, ai.RetryConfig{}),
		local:       ai.NewLocalProvider("http://127.0.0.1:0", "qwen2.5-coder", ai.RetryConfig{}),
		localModel:  "qwen2.5-coder",
		activeName:  name,
		activeModel: model,
	}
}

func TestSetModelSwitchAnthropicModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	a := newTestApp()
	desc, err := a.SetModel([]string{"anthropic", "claude-haiku-test"})
	if err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if desc != "anthropic / claude-haiku-test" {
		t.Errorf("desc = %q", desc)
	}
	if a.override == nil {
		t.Fatal("expected override to be set")
	}
	if a.activeName != "anthropic" || a.activeModel != "claude-haiku-test" {
		t.Errorf("active = %s / %s", a.activeName, a.activeModel)
	}
	// providers() must surface the override so subsequent turns use it.
	if a.providers().Override == nil {
		t.Error("providers().Override not propagated")
	}
}

func TestSetModelBareModelKeepsProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	a := newTestApp()
	if _, err := a.SetModel([]string{"some-claude-model"}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if a.activeName != "anthropic" {
		t.Errorf("provider = %q, want anthropic", a.activeName)
	}
	if a.activeModel != "some-claude-model" {
		t.Errorf("model = %q", a.activeModel)
	}
}

func TestSetModelUnknownProvider(t *testing.T) {
	a := newTestApp()
	if _, err := a.SetModel([]string{"gemini", "x"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestSetModelNoArgs(t *testing.T) {
	a := newTestApp()
	if _, err := a.SetModel(nil); err == nil {
		t.Fatal("expected usage error with no args")
	}
}

func TestSetModelOpenAINeedsEndpoint(t *testing.T) {
	a := newTestApp()
	if _, err := a.SetModel([]string{"openai:gpt-4o"}); err == nil {
		t.Fatal("expected error when openai endpoint is unconfigured")
	}
}

func TestModelOptionsListsBoth(t *testing.T) {
	a := newTestApp()
	opts := a.ModelOptions()
	if len(opts) != 2 {
		t.Fatalf("got %d options, want 2", len(opts))
	}
	if opts[0].Provider != "anthropic" {
		t.Errorf("first option = %q, want anthropic", opts[0].Provider)
	}
	if !opts[0].Active {
		t.Error("expected anthropic to be the active option by default")
	}
}

// TestNewWiresModelRouter verifies that model_small and model_strong from
// [routing] are carried into the App's router field, which is used to pass
// model overrides into agent.Config.ModelOverride and cheap completion calls.
func TestNewWiresModelRouter(t *testing.T) {
	cfg := &config.Config{}
	cfg.Routing.ModelSmall = "claude-haiku-4-5"
	cfg.Routing.ModelStrong = "claude-opus-4-8"
	name, model := cloudNameModel(cfg)
	a := &App{
		cfg:         cfg,
		cloud:       ai.NewAnthropicProvider("sk-test", model, ai.RetryConfig{}),
		local:       ai.NewLocalProvider("http://127.0.0.1:0", "qwen", ai.RetryConfig{}),
		localModel:  "qwen",
		activeName:  name,
		activeModel: model,
		router: ai.ModelRouter{
			Small:  cfg.Routing.ModelSmall,
			Strong: cfg.Routing.ModelStrong,
		},
	}
	if got := a.router.ModelFor(ai.TaskKindCheap); got != "claude-haiku-4-5" {
		t.Errorf("router.ModelFor(Cheap) = %q, want claude-haiku-4-5", got)
	}
	if got := a.router.ModelFor(ai.TaskKindReasoning); got != "claude-opus-4-8" {
		t.Errorf("router.ModelFor(Reasoning) = %q, want claude-opus-4-8", got)
	}
	if !a.router.Enabled() {
		t.Error("router.Enabled() = false, want true when both models are set")
	}
}

// TestNewRouterZeroValueWhenUnconfigured verifies that an App built from config
// without model_small/model_strong has a zero-value router (routing disabled).
func TestNewRouterZeroValueWhenUnconfigured(t *testing.T) {
	a := newTestApp()
	if a.router.Enabled() {
		t.Error("router.Enabled() = true, want false when routing is unconfigured")
	}
	if got := a.router.ModelFor(ai.TaskKindCheap); got != "" {
		t.Errorf("router.ModelFor(Cheap) = %q, want empty", got)
	}
	if got := a.router.ModelFor(ai.TaskKindReasoning); got != "" {
		t.Errorf("router.ModelFor(Reasoning) = %q, want empty", got)
	}
}
