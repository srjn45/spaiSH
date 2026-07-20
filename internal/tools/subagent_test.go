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
	d := tools.NewDelegate(func(_ context.Context, task, _ string) (string, error) {
		return "ran: " + task, nil
	})
	if d.Name() != "delegate" {
		t.Fatalf("Name() = %q, want delegate", d.Name())
	}
	schema := d.Schema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Errorf("Schema() missing properties")
	} else {
		if _, has := props["task"]; !has {
			t.Errorf("Schema() missing task property")
		}
		if _, has := props["profile"]; !has {
			t.Errorf("Schema() missing profile property")
		}
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
	var gotTask, gotProfile string
	d := tools.NewDelegate(func(_ context.Context, task, profile string) (string, error) {
		gotTask = task
		gotProfile = profile
		return "summary text", nil
	})
	out, err := d.Run(context.Background(), json.RawMessage(`{"task":"do a thing"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotTask != "do a thing" {
		t.Errorf("runner got task %q, want %q", gotTask, "do a thing")
	}
	if gotProfile != "" {
		t.Errorf("runner got profile %q, want empty (no profile in input)", gotProfile)
	}
	if out != "summary text" {
		t.Errorf("Run output = %q, want %q", out, "summary text")
	}

	boom := tools.NewDelegate(func(_ context.Context, _, _ string) (string, error) {
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

// TestDelegateForwardsProfile verifies the profile arg is trimmed and passed to
// the runner.
func TestDelegateForwardsProfile(t *testing.T) {
	var gotTask, gotProfile string
	d := tools.NewDelegate(func(_ context.Context, task, profile string) (string, error) {
		gotTask = task
		gotProfile = profile
		return "ok", nil
	})
	_, err := d.Run(context.Background(), json.RawMessage(`{"task":"review the code","profile":"reviewer"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotTask != "review the code" {
		t.Errorf("task = %q, want %q", gotTask, "review the code")
	}
	if gotProfile != "reviewer" {
		t.Errorf("profile = %q, want reviewer", gotProfile)
	}
}

// TestDelegateProfileArgWhitespaceTrimmed ensures leading/trailing whitespace in
// the profile value is stripped before being passed to the runner.
func TestDelegateProfileArgWhitespaceTrimmed(t *testing.T) {
	var gotProfile string
	d := tools.NewDelegate(func(_ context.Context, _, profile string) (string, error) {
		gotProfile = profile
		return "ok", nil
	})
	_, err := d.Run(context.Background(), json.RawMessage(`{"task":"test","profile":"  tester  "}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotProfile != "tester" {
		t.Errorf("profile = %q, want %q", gotProfile, "tester")
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

func TestProfileArg(t *testing.T) {
	if got := tools.ProfileArg(json.RawMessage(`{"task":"x","profile":"reviewer"}`)); got != "reviewer" {
		t.Errorf("ProfileArg = %q, want reviewer", got)
	}
	if got := tools.ProfileArg(json.RawMessage(`{"task":"x"}`)); got != "" {
		t.Errorf("ProfileArg = %q, want empty (absent field)", got)
	}
	if got := tools.ProfileArg(json.RawMessage(`{}`)); got != "" {
		t.Errorf("ProfileArg = %q, want empty (no fields)", got)
	}
}

// TestRegistryFilter verifies that Filter() returns only the named subset of tools
// and never grants tools that are absent from the source registry.
func TestRegistryFilter(t *testing.T) {
	reg := tools.DefaultRegistry()

	// Empty allowlist → same registry (all tools kept).
	full := reg.Filter(nil)
	if _, ok := full.Get("read_file"); !ok {
		t.Errorf("Filter(nil) should keep all tools, but read_file is missing")
	}

	// Non-empty allowlist → only listed tools survive.
	filtered := reg.Filter([]string{"read_file", "grep"})
	if _, ok := filtered.Get("read_file"); !ok {
		t.Errorf("Filter([read_file grep]) should contain read_file")
	}
	if _, ok := filtered.Get("grep"); !ok {
		t.Errorf("Filter([read_file grep]) should contain grep")
	}
	if _, ok := filtered.Get("bash"); ok {
		t.Errorf("Filter([read_file grep]) must not contain bash")
	}
	if _, ok := filtered.Get("write_file"); ok {
		t.Errorf("Filter([read_file grep]) must not contain write_file")
	}

	// A name not present in the source is silently ignored (never granted).
	filtered2 := reg.Filter([]string{"read_file", "no_such_tool"})
	if _, ok := filtered2.Get("read_file"); !ok {
		t.Errorf("Filter([read_file no_such_tool]) should contain read_file")
	}
	if _, ok := filtered2.Get("no_such_tool"); ok {
		t.Errorf("Filter must not invent tools that weren't in the registry")
	}

	// Specs count should match.
	specs := filtered.Specs()
	if len(specs) != 2 {
		t.Errorf("Filter([read_file grep]) Specs() len = %d, want 2", len(specs))
	}
}
