package agent

import (
	"encoding/json"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/permissions"
)

// TestClassifyRememberFactTier verifies that the remember_fact tool is gated at
// TierRead — no confirmation prompt is needed, mirroring todo_write. Storing a
// fact to .spai/memory.jsonl is agent bookkeeping, not user-visible mutation.
func TestClassifyRememberFactTier(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"key": "build-command", "value": "make gen"})
	tier, display := classify(ai.ToolCall{Name: "remember_fact", Input: json.RawMessage(input)})
	if tier != permissions.TierRead {
		t.Errorf("classify(remember_fact) tier = %v, want TierRead", tier)
	}
	if display == "" {
		t.Error("classify(remember_fact) display should not be empty")
	}
}
