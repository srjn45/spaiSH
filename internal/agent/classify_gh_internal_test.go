package agent

import (
	"encoding/json"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/permissions"
)

// TestClassifyGHRoutesToGHTier guards the wiring between the gh tool and the
// permission gate: classify() must route a gh tool call through tools.GHTier so
// outward-facing subcommands (pr-create/pr-merge/push) require confirmation.
// Without this case classify() would fall through to the TierRead default and
// run those mutations silently — a permission-model regression.
func TestClassifyGHRoutesToGHTier(t *testing.T) {
	cases := []struct {
		sub  string
		want permissions.Tier
	}{
		{"pr-list", permissions.TierRead},
		{"pr-view", permissions.TierRead},
		{"pr-checkout", permissions.TierWrite},
		{"create-branch", permissions.TierWrite},
		{"pr-create", permissions.TierElevated},
		{"pr-merge", permissions.TierElevated},
		{"push", permissions.TierElevated},
	}
	for _, c := range cases {
		input, _ := json.Marshal(map[string]any{"subcommand": c.sub})
		tier, _ := classify(ai.ToolCall{Name: "gh", Input: json.RawMessage(input)})
		if tier != c.want {
			t.Errorf("classify(gh %s) = %v, want %v", c.sub, tier, c.want)
		}
	}
}
