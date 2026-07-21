package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"spaish/internal/session"
)

// RememberFact persists a learned key/value fact to the project's
// .spai/memory.jsonl so it is available in future sessions.
//
// It is classified at TierRead (no confirmation prompt) because it writes only
// to an agent-managed metadata file, not to user source code — analogous to
// todo_write.
type RememberFact struct {
	// WorkingDir is the agent's working directory, used to locate the
	// project root (and therefore .spai/memory.jsonl).
	WorkingDir string
	// MaxFacts caps the number of stored facts. 0 resolves to
	// session.DefaultMaxFacts inside the memory store.
	MaxFacts int
}

func (t *RememberFact) Name() string { return "remember_fact" }

func (t *RememberFact) Description() string {
	return "Persist a learned fact (key/value pair) to the project memory store so it is available in future sessions."
}

func (t *RememberFact) Schema() map[string]any {
	return objectSchema(
		map[string]any{
			"key":   strProp(`Short unique identifier for this fact (e.g. "build-command", "test-runner"). Used for deduplication: storing to an existing key updates the value.`),
			"value": strProp(`The fact to remember (e.g. "make gen", "prefer table-driven tests").`),
		},
		"key", "value",
	)
}

func (t *RememberFact) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("remember_fact: %w", err)
	}
	if args.Key == "" {
		return "", fmt.Errorf("remember_fact: key is required")
	}
	if args.Value == "" {
		return "", fmt.Errorf("remember_fact: value is required")
	}
	store := session.NewMemoryStore(t.WorkingDir, t.MaxFacts)
	if err := store.Append(args.Key, args.Value); err != nil {
		return "", fmt.Errorf("remember_fact: %w", err)
	}
	return fmt.Sprintf("remembered: %s = %s", args.Key, args.Value), nil
}
