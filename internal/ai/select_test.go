package ai_test

import (
	"context"
	"testing"

	"spaish/internal/ai"
)

// stubProvider is a minimal Provider for selection tests.
type stubProvider struct {
	name      string
	available bool
}

func (s stubProvider) Name() string    { return s.name }
func (s stubProvider) Available() bool { return s.available }
func (s stubProvider) Complete(context.Context, []ai.Message) (<-chan string, error) {
	return nil, nil
}
func (s stubProvider) Stream(context.Context, ai.Request) (<-chan ai.Event, error) {
	return nil, nil
}

func TestParseModelSelection(t *testing.T) {
	isProvider := func(s string) bool {
		switch s {
		case "anthropic", "openai", "ollama":
			return true
		}
		return false
	}
	cases := []struct {
		name         string
		args         []string
		wantProvider string
		wantModel    string
	}{
		{"empty", nil, "", ""},
		{"provider only", []string{"anthropic"}, "anthropic", ""},
		{"two tokens", []string{"anthropic", "claude-opus-4-8"}, "anthropic", "claude-opus-4-8"},
		{"colon", []string{"openai:gpt-4o"}, "openai", "gpt-4o"},
		{"slash", []string{"ollama/qwen2.5-coder"}, "ollama", "qwen2.5-coder"},
		{"bare model", []string{"claude-opus-4-8"}, "", "claude-opus-4-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, m := ai.ParseModelSelection(tc.args, isProvider)
			if p != tc.wantProvider || m != tc.wantModel {
				t.Errorf("got (%q, %q), want (%q, %q)", p, m, tc.wantProvider, tc.wantModel)
			}
		})
	}
}

func TestSelectOverrideWins(t *testing.T) {
	cloud := stubProvider{name: "anthropic", available: true}
	local := stubProvider{name: "ollama", available: true}
	override := stubProvider{name: "ollama", available: true}

	set := ai.ProviderSet{Override: override, Cloud: cloud, Local: local}
	got, err := set.Select(false)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got != override {
		t.Errorf("expected override provider, got %v", got.Name())
	}

	// forceLocal must bypass the override and take the local provider.
	got, err = set.Select(true)
	if err != nil {
		t.Fatalf("Select(forceLocal): %v", err)
	}
	if got != local {
		t.Errorf("forceLocal: expected local provider, got %v", got.Name())
	}
}

func TestSelectUnavailableOverrideFallsBack(t *testing.T) {
	cloud := stubProvider{name: "anthropic", available: true}
	override := stubProvider{name: "openai", available: false}

	set := ai.ProviderSet{Override: override, Cloud: cloud}
	got, err := set.Select(false)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got != cloud {
		t.Errorf("expected fallback to cloud, got %v", got.Name())
	}
}
