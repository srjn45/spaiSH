package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"spaish/internal/agent"
	"spaish/internal/ai"
	"spaish/internal/permissions"
	"spaish/internal/protocol"
	"spaish/internal/session"
	"spaish/internal/tools"
)

// scriptedProvider returns one pre-baked event sequence per Stream call.
type scriptedProvider struct {
	turns    [][]ai.Event
	idx      int
	lastReq  ai.Request
	loopTool *ai.ToolCall // if set, every turn emits this tool call (ignores turns)
}

func (p *scriptedProvider) Available() bool { return true }
func (p *scriptedProvider) Name() string    { return "scripted" }
func (p *scriptedProvider) Complete(_ context.Context, _ []ai.Message) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}
func (p *scriptedProvider) Stream(_ context.Context, req ai.Request) (<-chan ai.Event, error) {
	p.lastReq = req
	ch := make(chan ai.Event)
	var evs []ai.Event
	if p.loopTool != nil {
		evs = []ai.Event{{Type: "tool_call", ToolCall: p.loopTool}, {Type: "done", Stop: "tool_use"}}
	} else if p.idx < len(p.turns) {
		evs = p.turns[p.idx]
		p.idx++
	} else {
		evs = []ai.Event{{Type: "done", Stop: "end_turn"}}
	}
	go func() {
		defer close(ch)
		for _, e := range evs {
			ch <- e
		}
	}()
	return ch, nil
}

// fakeTool records its invocations and returns a fixed output.
type fakeTool struct {
	name  string
	out   string
	calls *int
}

func (t fakeTool) Name() string           { return t.name }
func (t fakeTool) Description() string    { return "fake" }
func (t fakeTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t fakeTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	if t.calls != nil {
		*t.calls++
	}
	return t.out, nil
}

func textEv(s string) ai.Event { return ai.Event{Type: "text", Text: s} }
func doneEv() ai.Event         { return ai.Event{Type: "done", Stop: "end_turn"} }
func toolEv(id, name, input string) ai.Event {
	return ai.Event{Type: "tool_call", ToolCall: &ai.ToolCall{ID: id, Name: name, Input: json.RawMessage(input)}}
}

func alwaysApprove(_ protocol.ConfirmRequest) bool { return true }
func alwaysDeny(_ protocol.ConfirmRequest) bool    { return false }

func newSession(t *testing.T) *session.Session {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("test")
	return s
}

func collect(ch <-chan protocol.Response) []protocol.Response {
	var out []protocol.Response
	for r := range ch {
		out = append(out, r)
	}
	return out
}

func joinText(rs []protocol.Response) string {
	var b strings.Builder
	for _, r := range rs {
		if r.Type == "text" || r.Type == "output" {
			b.WriteString(r.Content)
		}
	}
	return b.String()
}

func run(t *testing.T, p ai.Provider, cfg agent.Config, confirm agent.ConfirmFunc, reg *tools.Registry, query string) []protocol.Response {
	t.Helper()
	a := agent.NewWithRegistry(p, cfg, confirm, reg)
	return collect(a.Run(context.Background(), &protocol.AgentRequest{Query: query}, newSession(t)))
}

func TestAgentFinishesWithNoTools(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{{textEv("all set"), doneEv()}}}
	rs := run(t, p, agent.Config{}, alwaysApprove, tools.NewRegistry(), "do nothing")
	if !strings.Contains(joinText(rs), "all set") {
		t.Errorf("expected final text, got %q", joinText(rs))
	}
}

func TestAgentRunsToolThenFinishes(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "bash", out: "ok", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"ls"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	run(t, p, agent.Config{}, alwaysApprove, reg, "list files")
	if calls != 1 {
		t.Errorf("expected bash to run once, ran %d times", calls)
	}
}

func TestAgentConfirmationDeniedStops(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "write_file", out: "wrote", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "write_file", `{"path":"x"}`), doneEv()},
	}}
	rs := run(t, p, agent.Config{}, alwaysDeny, reg, "write a file")
	if calls != 0 {
		t.Errorf("denied tool should not run, ran %d times", calls)
	}
	if !strings.Contains(joinText(rs), "Cancelled by user") {
		t.Errorf("expected cancellation message, got %q", joinText(rs))
	}
}

func TestAgentAutonomousSkipsConfirm(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "write_file", out: "wrote", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "write_file", `{"path":"x"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	run(t, p, agent.Config{Autonomous: true}, alwaysDeny, reg, "write a file")
	if calls != 1 {
		t.Errorf("autonomous write_file should run once, ran %d times", calls)
	}
}

// TestAgentMCPToolIsGated verifies that an mcp__* tool is treated as Write tier
// and therefore requires confirmation in manual mode (denying it stops the run).
func TestAgentMCPToolIsGated(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "mcp__fs__read", out: "data", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "mcp__fs__read", `{}`), doneEv()},
	}}
	rs := run(t, p, agent.Config{}, alwaysDeny, reg, "use mcp tool")
	if calls != 0 {
		t.Errorf("denied MCP tool should not run, ran %d times", calls)
	}
	if !strings.Contains(joinText(rs), "Cancelled by user") {
		t.Errorf("expected MCP tool to be gated, got %q", joinText(rs))
	}
}

// TestAgentPolicyAllowBypassesConfirm verifies an "allow" policy runs a
// Write-tier tool in manual mode without prompting (confirmFn always denies).
func TestAgentPolicyAllowBypassesConfirm(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "write_file", out: "wrote", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "write_file", `{"path":"x"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	cfg := agent.Config{Policy: permissions.NewPolicy(map[string]string{"write_file": "allow"}, nil, nil)}
	run(t, p, cfg, alwaysDeny, reg, "write a file")
	if calls != 1 {
		t.Errorf("allow policy should run write_file once despite deny confirm, ran %d times", calls)
	}
}

// TestAgentPolicyDenyBlocksAndContinues verifies a "deny" policy blocks the
// tool, emits a blocked result, and lets the loop continue (does not abort).
func TestAgentPolicyDenyBlocksAndContinues(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "bash", out: "ok", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"rm -rf /tmp/x"}`), doneEv()},
		{textEv("recovered"), doneEv()},
	}}
	cfg := agent.Config{Policy: permissions.NewPolicy(map[string]string{"bash": "deny"}, nil, nil)}
	rs := run(t, p, cfg, alwaysApprove, reg, "delete stuff")
	if calls != 0 {
		t.Errorf("denied tool should not run, ran %d times", calls)
	}
	out := joinText(rs)
	if !strings.Contains(out, "blocked by permission policy") {
		t.Errorf("expected blocked message, got %q", out)
	}
	if !strings.Contains(out, "recovered") {
		t.Errorf("expected loop to continue after deny, got %q", out)
	}
}

// TestAgentPolicyDenyEnforcedInAutoMode verifies deny blocks even in auto mode,
// where confirmation is otherwise skipped entirely.
func TestAgentPolicyDenyEnforcedInAutoMode(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "bash", out: "ok", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"rm -rf /tmp/x"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	cfg := agent.Config{Autonomous: true, Policy: permissions.NewPolicy(map[string]string{"bash": "deny"}, nil, nil)}
	rs := run(t, p, cfg, alwaysApprove, reg, "delete stuff")
	if calls != 0 {
		t.Errorf("deny must be enforced in auto mode, ran %d times", calls)
	}
	if !strings.Contains(joinText(rs), "blocked by permission policy") {
		t.Errorf("expected blocked message in auto mode, got %q", joinText(rs))
	}
}

// TestAgentBashAllowlistBypassesConfirm verifies an allowlisted bash prefix runs
// without prompting in manual mode even though bash is Write-tier or higher.
func TestAgentBashAllowlistBypassesConfirm(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "bash", out: "M file.go", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"git status -s"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	cfg := agent.Config{Policy: permissions.NewPolicy(nil, nil, []string{"git status"})}
	run(t, p, cfg, alwaysDeny, reg, "check status")
	if calls != 1 {
		t.Errorf("allowlisted command should run once despite deny confirm, ran %d times", calls)
	}
}

// TestAgentMCPServerAllowBypassesConfirm verifies a per-server "allow" policy
// runs an mcp__<server>__* tool without prompting in manual mode.
func TestAgentMCPServerAllowBypassesConfirm(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "mcp__fs__read", out: "data", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "mcp__fs__read", `{}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	cfg := agent.Config{Policy: permissions.NewPolicy(nil, map[string]string{"fs": "allow"}, nil)}
	run(t, p, cfg, alwaysDeny, reg, "use mcp tool")
	if calls != 1 {
		t.Errorf("server-allowed MCP tool should run once despite deny confirm, ran %d times", calls)
	}
}

func TestAgentUnknownToolContinues(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "nope", `{}`), doneEv()},
		{textEv("recovered"), doneEv()},
	}}
	rs := run(t, p, agent.Config{}, alwaysApprove, tools.NewRegistry(), "use a bad tool")
	if !strings.Contains(joinText(rs), "recovered") {
		t.Errorf("expected loop to continue after unknown tool, got %q", joinText(rs))
	}
}

func TestAgentMaxIterationsReached(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "bash", out: "ok", calls: &calls})
	p := &scriptedProvider{loopTool: &ai.ToolCall{ID: "1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)}}
	rs := run(t, p, agent.Config{Autonomous: true, MaxIterations: 2}, alwaysApprove, reg, "loop")
	if calls != 2 {
		t.Errorf("expected 2 tool runs at the iteration cap, got %d", calls)
	}
	if !strings.Contains(joinText(rs), "iteration limit") {
		t.Errorf("expected iteration-limit message, got %q", joinText(rs))
	}
}

func TestAgentPlanModeDoesNotExecute(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "bash", out: "ok", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"rm -rf /tmp/x"}`), doneEv()},
	}}
	rs := run(t, p, agent.Config{Mode: agent.ModePlan}, alwaysApprove, reg, "clean up")
	if calls != 0 {
		t.Errorf("plan mode must not execute tools, ran %d times", calls)
	}
	if !strings.Contains(joinText(rs), "(plan)") {
		t.Errorf("expected a plan line, got %q", joinText(rs))
	}
}

func TestAgentInjectsStdinAndQuery(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{{textEv("ok"), doneEv()}}}
	a := agent.NewWithRegistry(p, agent.Config{Stdin: "log line"}, alwaysApprove, tools.NewRegistry())
	collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "explain"}, newSession(t)))

	msgs := p.lastReq.Messages
	if len(msgs) < 2 {
		t.Fatalf("expected stdin + query messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[len(msgs)-2].Content, "log line") {
		t.Errorf("stdin not injected: %+v", msgs)
	}
	if msgs[len(msgs)-1].Content != "explain" {
		t.Errorf("query message = %q, want explain", msgs[len(msgs)-1].Content)
	}
}
