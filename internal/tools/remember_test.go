package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/tools"
)

func TestRememberFactRun(t *testing.T) {
	dir := t.TempDir()
	// Create a .git dir so projectRoot stops here.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	tool := &tools.RememberFact{WorkingDir: dir}
	input, _ := json.Marshal(map[string]string{"key": "build", "value": "make gen"})
	out, err := tool.Run(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "build") || !strings.Contains(out, "make gen") {
		t.Errorf("unexpected output: %q", out)
	}

	// Verify the file was actually written.
	memPath := filepath.Join(dir, ".spai", "memory.jsonl")
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("memory.jsonl not created: %v", err)
	}
	if !strings.Contains(string(data), "make gen") {
		t.Errorf("memory.jsonl missing fact: %s", data)
	}
}

func TestRememberFactMissingKey(t *testing.T) {
	tool := &tools.RememberFact{WorkingDir: t.TempDir()}
	input, _ := json.Marshal(map[string]string{"key": "", "value": "v"})
	_, err := tool.Run(context.Background(), json.RawMessage(input))
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestRememberFactMissingValue(t *testing.T) {
	tool := &tools.RememberFact{WorkingDir: t.TempDir()}
	input, _ := json.Marshal(map[string]string{"key": "k", "value": ""})
	_, err := tool.Run(context.Background(), json.RawMessage(input))
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestRememberFactSchema(t *testing.T) {
	tool := &tools.RememberFact{}
	schema := tool.Schema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}
	if _, hasKey := props["key"]; !hasKey {
		t.Error("schema missing 'key' property")
	}
	if _, hasValue := props["value"]; !hasValue {
		t.Error("schema missing 'value' property")
	}
}
