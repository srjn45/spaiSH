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
