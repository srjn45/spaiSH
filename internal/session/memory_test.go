package session

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestMemoryStore(t *testing.T, maxFacts int) *MemoryStore {
	t.Helper()
	dir := t.TempDir()
	// Create a fake .git so projectRoot stops there.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore(dir, maxFacts)
	return store
}

func TestMemoryStoreLoadEmpty(t *testing.T) {
	s := newTestMemoryStore(t, 0)
	facts, err := s.Load()
	if err != nil {
		t.Fatalf("Load on absent file: %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("expected 0 facts, got %d", len(facts))
	}
}

func TestMemoryStoreAppendAndLoad(t *testing.T) {
	s := newTestMemoryStore(t, 0)

	if err := s.Append("build", "make gen"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append("test", "go test ./..."); err != nil {
		t.Fatalf("Append: %v", err)
	}

	facts, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	if facts[0].Key != "build" || facts[0].Value != "make gen" {
		t.Errorf("fact[0] = {%q,%q}, want {%q,%q}", facts[0].Key, facts[0].Value, "build", "make gen")
	}
	if facts[1].Key != "test" || facts[1].Value != "go test ./..." {
		t.Errorf("fact[1] = {%q,%q}, want {%q,%q}", facts[1].Key, facts[1].Value, "test", "go test ./...")
	}
	if facts[0].LearnedAt.IsZero() {
		t.Error("LearnedAt should not be zero")
	}
}

func TestMemoryStoreDedup(t *testing.T) {
	s := newTestMemoryStore(t, 0)

	if err := s.Append("build", "make"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("style", "tabs"); err != nil {
		t.Fatal(err)
	}
	// Update existing key — value changes, position preserved, count stays 2.
	if err := s.Append("build", "make gen"); err != nil {
		t.Fatal(err)
	}

	facts, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts after dedup, got %d", len(facts))
	}
	if facts[0].Key != "build" || facts[0].Value != "make gen" {
		t.Errorf("dedup: fact[0] = {%q,%q}, want {%q,%q}", facts[0].Key, facts[0].Value, "build", "make gen")
	}
	if facts[1].Key != "style" {
		t.Errorf("dedup: second fact key = %q, want %q", facts[1].Key, "style")
	}
}

func TestMemoryStorePrune(t *testing.T) {
	const max = 3
	s := newTestMemoryStore(t, max)

	keys := []string{"a", "b", "c", "d", "e"}
	for _, k := range keys {
		if err := s.Append(k, "v-"+k); err != nil {
			t.Fatalf("Append %s: %v", k, err)
		}
	}

	facts, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != max {
		t.Fatalf("expected %d facts after prune, got %d", max, len(facts))
	}
	// Oldest (a, b) should be gone; newest (c, d, e) survive.
	if facts[0].Key != "c" || facts[1].Key != "d" || facts[2].Key != "e" {
		t.Errorf("prune kept wrong facts: %v", facts)
	}
}

func TestMemoryStoreCorruptEntry(t *testing.T) {
	s := newTestMemoryStore(t, 0)

	// Seed with a valid fact.
	if err := s.Append("ok", "value"); err != nil {
		t.Fatal(err)
	}
	// Inject a corrupt line directly.
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not-json\n")
	_ = f.Close()

	// Load should return only the valid entry and not error.
	facts, err := s.Load()
	if err != nil {
		t.Fatalf("Load with corrupt entry: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 valid fact, got %d", len(facts))
	}
	if facts[0].Key != "ok" {
		t.Errorf("expected key %q, got %q", "ok", facts[0].Key)
	}
}

func TestMemoryStoreDefaultMaxFacts(t *testing.T) {
	s := newTestMemoryStore(t, 0) // 0 → DefaultMaxFacts
	if s.maxFacts != DefaultMaxFacts {
		t.Errorf("maxFacts = %d, want %d", s.maxFacts, DefaultMaxFacts)
	}
}
