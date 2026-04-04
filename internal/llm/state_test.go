package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaios/internal/llm"
)

func TestLoadStateEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")
	s, err := llm.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if s.ActiveRuntime != "ollama" {
		t.Errorf("expected default active_runtime 'ollama', got %q", s.ActiveRuntime)
	}
	if s.ActiveModel == "" {
		t.Error("expected non-empty default active_model")
	}
}

func TestLoadStatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")

	s, _ := llm.LoadState(path)
	s.SetRuntime("ollama", "0.6.1", "http://localhost:11434")
	s.SetActiveModel("llama3.2:3b")
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	s2, err := llm.LoadState(path)
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if s2.ActiveModel != "llama3.2:3b" {
		t.Errorf("got active model %q, want %q", s2.ActiveModel, "llama3.2:3b")
	}
	rt, ok := s2.Runtimes["ollama"]
	if !ok {
		t.Fatal("expected ollama runtime in state")
	}
	if rt.Version != "0.6.1" {
		t.Errorf("got version %q, want %q", rt.Version, "0.6.1")
	}
}

func TestLoadStateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")
	s, _ := llm.LoadState(path)
	s.SetActiveModel("test-model")
	s.Save()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected mode 0600, got %v", info.Mode().Perm())
	}
}

func TestLoadStateCorrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")
	os.WriteFile(path, []byte("not valid json {{{{"), 0600)

	s, err := llm.LoadState(path)
	if err != nil {
		t.Fatalf("expected no error for corrupted file, got: %v", err)
	}
	// Should return a fresh default state
	if s.ActiveRuntime != "ollama" {
		t.Errorf("expected default state after corruption")
	}
}
