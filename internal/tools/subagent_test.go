package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"spaish/internal/tools"
)

// TestDelegateBasics covers name, schema, and the empty-task guard.
func TestDelegateBasics(t *testing.T) {
	d := tools.NewDelegate(func(_ context.Context, task string) (string, error) {
		return "ran: " + task, nil
	})
	if d.Name() != "delegate" {
		t.Fatalf("Name() = %q, want delegate", d.Name())
	}
	if _, ok := d.Schema()["properties"]; !ok {
		t.Errorf("Schema() missing properties")
	}

	if _, err := d.Run(context.Background(), json.RawMessage(`{"task":"  "}`)); err == nil {
		t.Errorf("expected error for blank task")
	}
	if _, err := d.Run(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Errorf("expected error for invalid JSON")
	}
}

// TestDelegateRunsRunner verifies the tool forwards the task to its runner and
// returns the runner's output (and its error) verbatim.
func TestDelegateRunsRunner(t *testing.T) {
	var got string
	d := tools.NewDelegate(func(_ context.Context, task string) (string, error) {
		got = task
		return "summary text", nil
	})
	out, err := d.Run(context.Background(), json.RawMessage(`{"task":"do a thing"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "do a thing" {
		t.Errorf("runner got task %q, want %q", got, "do a thing")
	}
	if out != "summary text" {
		t.Errorf("Run output = %q, want %q", out, "summary text")
	}

	boom := tools.NewDelegate(func(_ context.Context, _ string) (string, error) {
		return "", errors.New("nested boom")
	})
	if _, err := boom.Run(context.Background(), json.RawMessage(`{"task":"x"}`)); err == nil || !strings.Contains(err.Error(), "nested boom") {
		t.Errorf("expected runner error to propagate, got %v", err)
	}
}

// TestDelegateNilRunner ensures a Delegate with no runner refuses to run rather
// than panicking — a guardrail for any construction path outside agent.go.
func TestDelegateNilRunner(t *testing.T) {
	d := tools.NewDelegate(nil)
	if _, err := d.Run(context.Background(), json.RawMessage(`{"task":"x"}`)); err == nil {
		t.Errorf("expected error from nil-runner Delegate")
	}
}

// TestDefaultRegistryHasNoDelegate is the depth-limit cornerstone: the nested
// agent is built from tools.DefaultRegistry(), so proving that registry does not
// contain "delegate" proves a delegated sub-agent cannot delegate again.
func TestDefaultRegistryHasNoDelegate(t *testing.T) {
	if _, ok := tools.DefaultRegistry().Get("delegate"); ok {
		t.Fatalf("DefaultRegistry() must not contain the delegate tool (would allow unbounded recursion)")
	}
}

func TestTaskArg(t *testing.T) {
	if got := tools.TaskArg(json.RawMessage(`{"task":"hello"}`)); got != "hello" {
		t.Errorf("TaskArg = %q, want hello", got)
	}
	if got := tools.TaskArg(json.RawMessage(`{}`)); got != "" {
		t.Errorf("TaskArg = %q, want empty", got)
	}
}
