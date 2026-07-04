package pricing

import (
	"math"
	"testing"
)

func TestLookupExact(t *testing.T) {
	r, ok := Lookup("claude-opus-4-8")
	if !ok {
		t.Fatal("expected opus to be found")
	}
	if r.Input != 5 || r.Output != 25 {
		t.Fatalf("opus rate = %v/%v, want 5/25", r.Input, r.Output)
	}
	if r.Local {
		t.Fatal("opus should not be local")
	}
}

func TestLookupPrefixMatch(t *testing.T) {
	// Versioned and vendor-prefixed ids should still resolve.
	cases := []string{
		"claude-opus-4-8-20260101",
		"anthropic/claude-opus-4-8",
		"CLAUDE-OPUS-4-8", // case-insensitive
	}
	for _, c := range cases {
		r, ok := Lookup(c)
		if !ok {
			t.Errorf("Lookup(%q): not found", c)
			continue
		}
		if r.Input != 5 || r.Output != 25 {
			t.Errorf("Lookup(%q) = %v/%v, want 5/25", c, r.Input, r.Output)
		}
	}
}

func TestLookupLongestMatchWins(t *testing.T) {
	// sonnet and haiku entries must not collide with opus.
	r, ok := Lookup("claude-sonnet-4-6")
	if !ok || r.Input != 3 || r.Output != 15 {
		t.Fatalf("sonnet lookup = %v (ok=%v), want 3/15", r, ok)
	}
	r, ok = Lookup("claude-haiku-4-5")
	if !ok || r.Input != 1 || r.Output != 5 {
		t.Fatalf("haiku lookup = %v (ok=%v), want 1/5", r, ok)
	}
}

func TestLookupLocal(t *testing.T) {
	for _, m := range []string{"ollama:llama3", "qwen2.5-coder", "local-model"} {
		r, ok := Lookup(m)
		if !ok {
			t.Errorf("Lookup(%q): expected local match", m)
			continue
		}
		if !r.Local {
			t.Errorf("Lookup(%q): expected Local=true", m)
		}
		if got := r.Cost(1000, 1000); got != 0 {
			t.Errorf("local Cost = %v, want 0", got)
		}
	}
}

func TestLookupUnknown(t *testing.T) {
	if r, ok := Lookup("gpt-4o"); ok {
		t.Fatalf("gpt-4o should be unknown, got %v", r)
	}
	if _, ok := Lookup(""); ok {
		t.Fatal("empty model should be unknown")
	}
}

func TestCost(t *testing.T) {
	r, _ := Lookup("claude-opus-4-8")
	// 1M input @ $5 + 1M output @ $25 = $30
	got := r.Cost(1_000_000, 1_000_000)
	if math.Abs(got-30) > 1e-9 {
		t.Fatalf("Cost = %v, want 30", got)
	}
	// Fractional: 500k input @ $5 = $2.50
	got = r.Cost(500_000, 0)
	if math.Abs(got-2.5) > 1e-9 {
		t.Fatalf("Cost = %v, want 2.5", got)
	}
}

func TestEstimateCost(t *testing.T) {
	got, ok := EstimateCost("claude-haiku-4-5", 1_000_000, 1_000_000)
	if !ok {
		t.Fatal("expected haiku to be found")
	}
	if math.Abs(got-6) > 1e-9 { // 1 + 5
		t.Fatalf("EstimateCost = %v, want 6", got)
	}
	if _, ok := EstimateCost("gpt-4o", 100, 100); ok {
		t.Fatal("unknown model should return ok=false")
	}
}

func TestCostWithCache(t *testing.T) {
	opus, _ := Lookup("claude-opus-4-8") // Input=$5, Output=$25 per 1M

	// 1M input + 1M output + 1M cache write + 1M cache read
	// = 5 + 25 + 5*1.25 + 5*0.1 = 5 + 25 + 6.25 + 0.5 = 36.75
	got := opus.CostWithCache(1_000_000, 1_000_000, 1_000_000, 1_000_000)
	if math.Abs(got-36.75) > 1e-9 {
		t.Errorf("CostWithCache = %v, want 36.75", got)
	}

	// No cache tokens → same as Cost()
	withoutCache := opus.CostWithCache(500_000, 0, 0, 0)
	baseline := opus.Cost(500_000, 0)
	if math.Abs(withoutCache-baseline) > 1e-9 {
		t.Errorf("CostWithCache with no cache tokens = %v, want %v (same as Cost)", withoutCache, baseline)
	}

	// Local model always costs $0.
	local := Rate{Model: "llama", Local: true}
	if got := local.CostWithCache(1_000_000, 1_000_000, 1_000_000, 1_000_000); got != 0 {
		t.Errorf("local CostWithCache = %v, want 0", got)
	}

	// Only cache read, no other tokens.
	// 1M cache read @ 0.1× input ($5) = $0.50
	readOnly := opus.CostWithCache(0, 0, 0, 1_000_000)
	if math.Abs(readOnly-0.5) > 1e-9 {
		t.Errorf("cache-read-only CostWithCache = %v, want 0.5", readOnly)
	}
}
