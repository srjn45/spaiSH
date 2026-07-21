package agent_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/agent"
	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/hooks"
	"spaish/internal/protocol"
	"spaish/internal/tools"
)

// mustHooks builds a hooks.Runner from specs or fails the test.
func mustHooks(t *testing.T, workDir string, specs ...config.HookSpec) hooks.Runner {
	t.Helper()
	r, err := hooks.New(specs, workDir)
	if err != nil {
		t.Fatalf("hooks.New error: %v", err)
	}
	return r
}

// TestPreToolBlock_doesNotRunTool proves a pre_tool block refuses the tool
// WITHOUT calling Registry.Run: the spy tool's call counter stays zero, the
// tool result is an IsError carrying the hook reason, and the loop continues.
func TestPreToolBlock_doesNotRunTool(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "write_file", out: "wrote", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "write_file", `{"path":"vendor/x.go"}`), doneEv()},
		{textEv("adapted"), doneEv()},
	}}
	cfg := agent.Config{
		Autonomous: true, // skip confirm so the block is the only thing stopping the tool
		Hooks: mustHooks(t, dir, config.HookSpec{
			Event: "pre_tool", Match: "write_file",
			Command: "echo 'no writes under vendor/' >&2; exit 1",
		}),
	}
	rs := run(t, p, cfg, alwaysApprove, reg, "write a file")
	if calls != 0 {
		t.Errorf("pre_tool block must not run the tool, ran %d times", calls)
	}
	out := joinText(rs)
	if !strings.Contains(out, "blocked by pre_tool hook: no writes under vendor/") {
		t.Errorf("expected block reason surfaced, got %q", out)
	}
	if !strings.Contains(out, "adapted") {
		t.Errorf("expected loop to continue after a block, got %q", out)
	}
	// The tool result sent to the model must be flagged as an error.
	var sawErrResult bool
	for _, m := range p.lastReq.Messages {
		for _, tr := range m.ToolResults {
			if tr.ToolUseID == "1" && tr.IsError && strings.Contains(tr.Content, "blocked by pre_tool hook") {
				sawErrResult = true
			}
		}
	}
	if !sawErrResult {
		t.Errorf("expected an IsError tool result with the block reason, messages=%+v", p.lastReq.Messages)
	}
}

// TestConfirmStillFires_withHooks proves hooks do not touch the tier/confirm
// gate: a Write-tier tool with a (non-blocking) pre_tool hook still invokes
// confirmFn with the same ConfirmRequest it would receive without hooks.
func TestConfirmStillFires_withHooks(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "write_file", out: "wrote", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "write_file", `{"path":"x"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	var recorded []protocol.ConfirmRequest
	confirm := func(req protocol.ConfirmRequest) bool {
		recorded = append(recorded, req)
		return true
	}
	cfg := agent.Config{
		Hooks: mustHooks(t, dir, config.HookSpec{
			Event: "pre_tool", Match: "write_file", Command: "exit 0",
		}),
	}
	run(t, p, cfg, confirm, reg, "write a file")

	if len(recorded) != 1 {
		t.Fatalf("confirmFn called %d times, want 1 (hooks must not touch the gate)", len(recorded))
	}
	if recorded[0].Tier != "write" {
		t.Errorf("confirm tier = %q, want write (hooks must not change tiers)", recorded[0].Tier)
	}
	if calls != 1 {
		t.Errorf("approved tool should run once, ran %d times", calls)
	}
}

// TestCancelledCall_neverRunsHook proves the hook fires strictly AFTER the
// confirm gate: when the user cancels, the loop returns before the hook layer,
// so the pre_tool hook's side effect never happens.
func TestCancelledCall_neverRunsHook(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "hook-ran")
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "write_file", out: "wrote", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "write_file", `{"path":"x"}`), doneEv()},
	}}
	cfg := agent.Config{
		Hooks: mustHooks(t, dir, config.HookSpec{
			Event: "pre_tool", Match: "write_file", Command: "touch " + marker,
		}),
	}
	rs := run(t, p, cfg, alwaysDeny, reg, "write a file")
	if calls != 0 {
		t.Errorf("cancelled tool must not run, ran %d times", calls)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("pre_tool hook ran for a cancelled call (marker exists)")
	}
	if !strings.Contains(joinText(rs), "Cancelled by user") {
		t.Errorf("expected cancellation message, got %q", joinText(rs))
	}
}

// TestPostToolRunsAfterSuccess proves post_tool observes a successful call: the
// hook runs, the tool's result is unchanged, and a post failure is surfaced as
// output without flipping the result to an error.
func TestPostToolRunsAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "post-ran")
	reg := tools.NewRegistry(fakeTool{name: "edit_file", out: "edited"})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "edit_file", `{"path":"a.go"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	cfg := agent.Config{
		Autonomous: true,
		Hooks: mustHooks(t, dir,
			config.HookSpec{Event: "post_tool", Match: "edit_file", Command: "touch " + marker},
			config.HookSpec{Event: "post_tool", Match: "edit_file", Command: "echo boom >&2; exit 1"},
		),
	}
	rs := run(t, p, cfg, alwaysApprove, reg, "edit a file")

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("post_tool hook did not run after success: %v", err)
	}
	if !strings.Contains(joinText(rs), "post_tool hook failed") {
		t.Errorf("expected a post_tool failure surfaced, got %q", joinText(rs))
	}
	// The tool result must remain a success (post_tool never undoes the tool).
	var found, isErr bool
	for _, m := range p.lastReq.Messages {
		for _, tr := range m.ToolResults {
			if tr.ToolUseID == "1" {
				found = true
				isErr = tr.IsError
			}
		}
	}
	if !found {
		t.Fatalf("no tool result for the edit found")
	}
	if isErr {
		t.Errorf("post_tool failure flipped the tool result to IsError; it must stay a success")
	}
}

// TestPostToolSkippedOnToolError proves post_tool observes only successes: a
// failing tool does not trigger the post hook.
func TestPostToolSkippedOnToolError(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "post-ran")
	reg := tools.NewRegistry(errTool{name: "bash", err: errors.New("boom")})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"false"}`), doneEv()},
		{textEv("recovered"), doneEv()},
	}}
	cfg := agent.Config{
		Autonomous: true,
		Hooks: mustHooks(t, dir, config.HookSpec{
			Event: "post_tool", Match: "bash", Command: "touch " + marker,
		}),
	}
	run(t, p, cfg, alwaysApprove, reg, "run a failing command")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("post_tool hook ran for a failed tool call (marker exists)")
	}
}

// TestNoHooks_identical proves zero-config byte-identity: with an empty
// Config.Hooks the recorded response stream matches the same run with no Hooks
// field set at all (the two must be indistinguishable).
func TestNoHooks_identical(t *testing.T) {
	newRun := func(cfg agent.Config) string {
		reg := tools.NewRegistry(fakeTool{name: "read_file", out: "contents"})
		p := &scriptedProvider{turns: [][]ai.Event{
			{toolEv("1", "read_file", `{"path":"x"}`), doneEv()},
			{textEv("done"), doneEv()},
		}}
		return joinText(run(t, p, cfg, alwaysApprove, reg, "read"))
	}
	baseline := newRun(agent.Config{})
	withEmptyHooks := newRun(agent.Config{Hooks: mustHooks(t, t.TempDir())})
	if baseline != withEmptyHooks {
		t.Errorf("empty Hooks changed behaviour:\n baseline=%q\n withEmptyHooks=%q", baseline, withEmptyHooks)
	}
}
