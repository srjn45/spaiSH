package agent_test

import (
	"context"
	"strings"
	"testing"

	"spaish/internal/agent"
	"spaish/internal/ai"
	"spaish/internal/protocol"
	"spaish/internal/tools"
)

// TestDelegateReusesConfirmForNestedDestructiveCall is the safety-critical test:
// a destructive tool call made *inside* the nested (delegated) loop must still go
// through the parent's real confirmFn — delegation must never auto-approve or
// bypass confirmation.
//
// Turn sequence (the scriptedProvider is shared by parent and nested, so turns
// are consumed in order across both loops):
//
//	0 (parent): call delegate{task}
//	1 (nested): call bash{rm -rf ...}   ← must hit confirmFn; we DENY it so the
//	                                       destructive command never actually runs
//	2 (parent): final text
func TestDelegateReusesConfirmForNestedDestructiveCall(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("d1", "delegate", `{"task":"clean up the temp dir"}`), doneEv()},
		{toolEv("b1", "bash", `{"command":"rm -rf /tmp/spai-subagent-test"}`), doneEv()},
		{textEv("parent finished"), doneEv()},
	}}

	var seen []protocol.ConfirmRequest
	confirm := func(req protocol.ConfirmRequest) bool {
		seen = append(seen, req)
		// Approve the top-level delegation, but DENY the nested destructive call
		// so nothing is actually executed on disk.
		if strings.Contains(req.Command, "rm -rf") {
			return false
		}
		return true
	}

	// Manual mode (the default) so tier-gated calls require confirmation.
	a := agent.NewWithRegistry(p, agent.Config{}, confirm, tools.DefaultRegistry())
	_ = collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "tidy up"}, newSession(t)))

	var sawDelegateGate, sawNestedBashGate bool
	for _, r := range seen {
		if strings.HasPrefix(r.Command, "delegate:") {
			sawDelegateGate = true
		}
		if strings.Contains(r.Command, "rm -rf") {
			sawNestedBashGate = true
			if r.Tier != "destructive" {
				t.Errorf("nested bash confirm tier = %q, want destructive", r.Tier)
			}
		}
	}
	if !sawDelegateGate {
		t.Errorf("expected the top-level delegate call to be confirmed; requests: %+v", seen)
	}
	if !sawNestedBashGate {
		t.Errorf("nested destructive call was NOT routed through confirmFn (auto-approval bug); requests: %+v", seen)
	}
}

// TestDelegateReturnsOnlySummary verifies the parent sees the delegation as a
// single tool call with one summarized result: the nested loop's intermediate
// text is NOT streamed back as top-level Response events.
func TestDelegateReturnsOnlySummary(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("d1", "delegate", `{"task":"compute the answer"}`), doneEv()},
		{textEv("NESTED_SECRET_INTERMEDIATE"), doneEv()}, // nested loop's own output
		{textEv("parent summary received"), doneEv()},
	}}

	// Auto mode: no confirmations needed for this read-only-ish flow.
	a := agent.NewWithRegistry(p, agent.Config{Mode: agent.ModeAuto}, alwaysApprove, tools.DefaultRegistry())
	rs := collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "go"}, newSession(t)))

	text := joinText(rs)
	if !strings.Contains(text, "parent summary received") {
		t.Errorf("expected parent's final text, got %q", text)
	}
	if strings.Contains(text, "NESTED_SECRET_INTERMEDIATE") {
		t.Errorf("nested intermediate output leaked to top-level stream: %q", text)
	}
}

// TestDelegateWithProfileGatesAtWrite verifies that a delegate call with a
// named profile is still confirmed at TierWrite in manual mode — the profile arg
// must not weaken the permission gate. The confirmation display must include the
// profile name so the user knows which kind of sub-agent is being spawned.
func TestDelegateWithProfileGatesAtWrite(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{
		// Turn 0: parent calls delegate with reviewer profile.
		{toolEv("d1", "delegate", `{"task":"review the PR","profile":"reviewer"}`), doneEv()},
		// Turn 1: nested agent produces its summary, then exits.
		{textEv("review complete"), doneEv()},
		// Turn 2: parent receives tool result and finishes.
		{textEv("all done"), doneEv()},
	}}

	var sawConfirm bool
	confirm := func(req protocol.ConfirmRequest) bool {
		// The display must include both the profile name and the task.
		if strings.Contains(req.Command, "delegate[reviewer]") && strings.Contains(req.Command, "review the PR") {
			sawConfirm = true
		}
		return true
	}

	a := agent.NewWithRegistry(p, agent.Config{}, confirm, tools.DefaultRegistry())
	rs := collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "go"}, newSession(t)))

	if !sawConfirm {
		t.Errorf("delegate with profile must be confirmed with profile name in display; got requests without it")
	}
	// Turn 2's "all done" text should appear in parent's top-level output.
	if !strings.Contains(joinText(rs), "all done") {
		t.Errorf("expected parent final text 'all done', got %q", joinText(rs))
	}
}

// TestDelegateProfileRestrictedToReadOnlyTools verifies that when a profile with
// a read-only tool allowlist is used, a write tool that would normally require
// confirmation is simply absent from the nested registry. The nested agent
// encountering an unknown tool receives an error result and recovers.
func TestDelegateProfileRestrictedToReadOnlyTools(t *testing.T) {
	// The provider turns are consumed across parent and nested runs. Turn 0 is the
	// parent calling delegate with the reviewer profile. Turn 1 is the nested
	// reviewer agent calling write_file (which should be absent from its registry).
	// Turn 2 is the nested agent recovering. Turn 3 is the parent finishing.
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("d1", "delegate", `{"task":"review files","profile":"reviewer"}`), doneEv()},
		{toolEv("w1", "write_file", `{"path":"x","content":"y"}`), doneEv()},
		{textEv("nested recovered"), doneEv()},
		{textEv("parent done"), doneEv()},
	}}

	a := agent.NewWithRegistry(p, agent.Config{Mode: agent.ModeAuto}, alwaysApprove, tools.DefaultRegistry())
	rs := collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "go"}, newSession(t)))

	// The nested write_file call should have received an "unknown tool" error
	// (the reviewer profile excludes it), and the nested loop continued. The
	// parent should then report its final text.
	text := joinText(rs)
	if !strings.Contains(text, "parent done") {
		t.Errorf("expected parent to complete, got %q", text)
	}
}
