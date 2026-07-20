package agent

import (
	"context"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/protocol"
	"spaish/internal/tools"
)

func approve(_ protocol.ConfirmRequest) bool { return true }

// stubProvider is a minimal ai.Provider that never streams anything; the
// white-box tests below only inspect construction, not the run loop.
type stubProvider struct{}

func (stubProvider) Available() bool { return true }
func (stubProvider) Name() string    { return "stub" }
func (stubProvider) Complete(_ context.Context, _ []ai.Message) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}
func (stubProvider) Stream(_ context.Context, _ ai.Request) (<-chan ai.Event, error) {
	ch := make(chan ai.Event)
	close(ch)
	return ch, nil
}

// TestTopLevelHasDelegateNestedDoesNot proves the depth limit at the registry
// level: a top-level Agent (built by New) advertises the delegate tool, while
// the registry runDelegate hands the nested Agent — tools.DefaultRegistry() —
// does not. A sub-agent therefore physically cannot delegate again.
func TestTopLevelHasDelegateNestedDoesNot(t *testing.T) {
	a := New(stubProvider{}, Config{}, approve)
	if _, ok := a.registry.Get("delegate"); !ok {
		t.Fatalf("top-level agent registry should contain the delegate tool")
	}

	// runDelegate builds the nested Agent from exactly this registry.
	if _, ok := tools.DefaultRegistry().Get("delegate"); ok {
		t.Fatalf("nested agent registry (DefaultRegistry) must not contain the delegate tool")
	}
}

// TestNewWithRegistryAddsDelegate confirms the wiring adds delegate even to an
// injected (initially empty) registry.
func TestNewWithRegistryAddsDelegate(t *testing.T) {
	reg := tools.NewRegistry()
	a := NewWithRegistry(stubProvider{}, Config{}, approve, reg)
	if _, ok := a.registry.Get("delegate"); !ok {
		t.Fatalf("NewWithRegistry should wire up the delegate tool")
	}
}

// TestChildConfigShrinksBudget verifies the derived child config always gets a
// strictly smaller (and still valid) iteration budget, drops inherited stdin,
// and preserves working dir / branch / mode so the nested loop is gated like
// its parent.
func TestChildConfigShrinksBudget(t *testing.T) {
	cases := []struct {
		parentMax int
		wantChild int
	}{
		{0, 12},  // unset parent → treated as default 25 → 12
		{25, 12}, // 25 / 2
		{10, 5},
		{2, 1},
		{1, 1}, // degenerate floor; never 0 (which would re-default to 25)
	}
	for _, c := range cases {
		child := childConfig(Config{MaxIterations: c.parentMax})
		if child.MaxIterations != c.wantChild {
			t.Errorf("parent max %d → child max %d, want %d", c.parentMax, child.MaxIterations, c.wantChild)
		}
		if child.MaxIterations < 1 {
			t.Errorf("child max %d must be >= 1", child.MaxIterations)
		}
		// Effective parent budget the loop would use.
		effParent := c.parentMax
		if effParent <= 0 {
			effParent = defaultMaxIterations
		}
		if effParent >= 2 && child.MaxIterations >= effParent {
			t.Errorf("child budget %d not strictly smaller than parent %d", child.MaxIterations, effParent)
		}
	}

	child := childConfig(Config{MaxIterations: 8, Stdin: "piped", WorkingDir: "/work", GitBranch: "main", Mode: ModeManual})
	if child.Stdin != "" {
		t.Errorf("child should not inherit parent stdin, got %q", child.Stdin)
	}
	if child.WorkingDir != "/work" || child.GitBranch != "main" || child.Mode != ModeManual {
		t.Errorf("child should inherit workdir/branch/mode: %+v", child)
	}
}

// TestResolveProfileBuiltins verifies the built-in profiles are discoverable by
// name and carry the expected non-empty fields.
func TestResolveProfileBuiltins(t *testing.T) {
	cases := []struct {
		name       string
		wantTools  bool // at least one tool in the allowlist
		wantPrompt bool // non-empty system prompt
	}{
		{"reviewer", true, true},
		{"tester", true, true},
		{"general", false, false},
	}
	for _, c := range cases {
		p, ok := resolveProfile(nil, c.name)
		if !ok {
			t.Errorf("resolveProfile(nil, %q) not found", c.name)
			continue
		}
		if p.Name != c.name {
			t.Errorf("resolveProfile(%q) Name = %q", c.name, p.Name)
		}
		if c.wantTools && len(p.Tools) == 0 {
			t.Errorf("builtin %q should have a non-empty tool allowlist", c.name)
		}
		if !c.wantTools && len(p.Tools) != 0 {
			t.Errorf("builtin %q should have an empty (unrestricted) tool allowlist", c.name)
		}
		if c.wantPrompt && p.SystemPrompt == "" {
			t.Errorf("builtin %q should have a non-empty system prompt", c.name)
		}
		if !c.wantPrompt && p.SystemPrompt != "" {
			t.Errorf("builtin %q should have no system prompt override, got %q", c.name, p.SystemPrompt)
		}
	}
}

// TestResolveProfileUnknown verifies that an unknown name returns false.
func TestResolveProfileUnknown(t *testing.T) {
	if _, ok := resolveProfile(nil, "no-such-profile"); ok {
		t.Errorf("resolveProfile for unknown name should return false")
	}
	if _, ok := resolveProfile(nil, ""); ok {
		t.Errorf("resolveProfile for empty name should return false")
	}
}

// TestResolveProfileUserOverridesBuiltin verifies a user-configured profile with
// the same name takes precedence over the builtin.
func TestResolveProfileUserOverridesBuiltin(t *testing.T) {
	custom := []SubagentProfile{
		{Name: "reviewer", SystemPrompt: "custom reviewer prompt", Tools: []string{"bash"}},
	}
	p, ok := resolveProfile(custom, "reviewer")
	if !ok {
		t.Fatal("resolveProfile should find user-defined reviewer")
	}
	if p.SystemPrompt != "custom reviewer prompt" {
		t.Errorf("user profile should override builtin, got %q", p.SystemPrompt)
	}
	if len(p.Tools) != 1 || p.Tools[0] != "bash" {
		t.Errorf("user profile tools = %v, want [bash]", p.Tools)
	}
}

// TestResolveProfileUserExtends verifies a custom profile name (not colliding
// with a builtin) is also discoverable.
func TestResolveProfileUserExtends(t *testing.T) {
	custom := []SubagentProfile{
		{Name: "deployer", SystemPrompt: "deploy expert", Tools: []string{"bash", "git"}},
	}
	p, ok := resolveProfile(custom, "deployer")
	if !ok {
		t.Fatal("resolveProfile should find user-defined deployer")
	}
	if p.Name != "deployer" {
		t.Errorf("Name = %q, want deployer", p.Name)
	}
	// builtins still reachable when user defines a different name
	if _, ok2 := resolveProfile(custom, "reviewer"); !ok2 {
		t.Errorf("builtin reviewer should still be resolvable alongside user profile")
	}
}
