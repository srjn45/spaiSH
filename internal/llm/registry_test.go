package llm_test

import (
	"testing"

	"spaios/internal/llm"
)

func TestRegistryGetOllama(t *testing.T) {
	rt, err := llm.Get("ollama")
	if err != nil {
		t.Fatalf("Get(\"ollama\") error: %v", err)
	}
	if rt.Name != "ollama" {
		t.Errorf("got name %q, want %q", rt.Name, "ollama")
	}
	if rt.Endpoint == "" {
		t.Error("expected non-empty endpoint")
	}
	if rt.DetectCmd == "" {
		t.Error("expected non-empty DetectCmd")
	}
	if len(rt.InstallCmds) == 0 {
		t.Error("expected at least one install command")
	}
	for _, cmd := range rt.InstallCmds {
		if cmd == "" {
			t.Error("install command must not be empty")
		}
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	_, err := llm.Get("nonexistent")
	if err == nil {
		t.Error("expected error for unknown runtime")
	}
}

func TestRegistryList(t *testing.T) {
	names := llm.List()
	if len(names) == 0 {
		t.Error("expected at least one runtime in list")
	}
	found := false
	for _, n := range names {
		if n == "ollama" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'ollama' in runtime list")
	}
}

func TestRegistryInstallTierNotEmpty(t *testing.T) {
	rt, _ := llm.Get("ollama")
	if rt.InstallTier == 0 {
		t.Error("expected non-zero InstallTier for ollama")
	}
}
