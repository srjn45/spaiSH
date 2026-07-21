package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// errProvider fails at Stream() setup time (e.g. auth/network error).
type errProvider struct{ err error }

func (p errProvider) Available() bool { return true }
func (p errProvider) Name() string    { return "err" }
func (p errProvider) Complete(_ context.Context, _ []ai.Message) (<-chan string, error) {
	return nil, p.err
}
func (p errProvider) Stream(_ context.Context, _ ai.Request) (<-chan ai.Event, error) {
	return nil, p.err
}

// errTool always fails when run.
type errTool struct {
	name string
	err  error
}

func (t errTool) Name() string           { return t.name }
func (t errTool) Description() string    { return "err" }
func (t errTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t errTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "", t.err
}

// TestAgentNewUsesDefaultRegistry verifies the public constructor wires up a
// usable agent (it builds the default tool registry rather than a nil one).
func TestAgentNewUsesDefaultRegistry(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{{textEv("ok"), doneEv()}}}
	a := agent.New(p, agent.Config{}, alwaysApprove)
	if a == nil {
		t.Fatal("agent.New returned nil")
	}
	// A no-tool run should complete cleanly through the default registry.
	rs := collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "hi"}, newSession(t)))
	if !strings.Contains(joinText(rs), "ok") {
		t.Errorf("expected agent built by New to run, got %q", joinText(rs))
	}
}

// TestAgentProviderStreamErrorSurfaces verifies a provider setup error is
// reported as an error response and the loop terminates with done.
func TestAgentProviderStreamErrorSurfaces(t *testing.T) {
	p := errProvider{err: errors.New("boom: no api key")}
	rs := run(t, p, agent.Config{}, alwaysApprove, tools.NewRegistry(), "hi")
	var sawErr, sawDone bool
	for _, r := range rs {
		if r.Type == "error" && strings.Contains(r.Content, "boom: no api key") {
			sawErr = true
		}
		if r.Type == "done" {
			sawDone = true
		}
	}
	if !sawErr {
		t.Errorf("expected AI error response, got %+v", rs)
	}
	if !sawDone {
		t.Errorf("expected done after error, got %+v", rs)
	}
}

// TestAgentMidStreamErrorSurfaces verifies an error event emitted mid-stream is
// forwarded and stops the loop.
func TestAgentMidStreamErrorSurfaces(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{
		{textEv("thinking..."), {Type: "error", Err: "provider exploded mid-stream"}},
	}}
	rs := run(t, p, agent.Config{}, alwaysApprove, tools.NewRegistry(), "hi")
	var sawErr bool
	for _, r := range rs {
		if r.Type == "error" && strings.Contains(r.Content, "exploded mid-stream") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Errorf("expected mid-stream error to surface, got %+v", rs)
	}
}

// TestAgentToolErrorReportedAndContinues verifies a tool that returns an error
// yields an error tool-result and lets the model recover on the next turn.
func TestAgentToolErrorReportedAndContinues(t *testing.T) {
	reg := tools.NewRegistry(errTool{name: "bash", err: errors.New("exit status 1: file not found")})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "bash", `{"command":"cat missing"}`), doneEv()},
		{textEv("recovered from failure"), doneEv()},
	}}
	rs := run(t, p, agent.Config{Autonomous: true}, alwaysApprove, reg, "read a file")
	out := joinText(rs)
	if !strings.Contains(out, "file not found") {
		t.Errorf("expected tool error to be reported, got %q", out)
	}
	if !strings.Contains(out, "recovered from failure") {
		t.Errorf("expected loop to continue after tool error, got %q", out)
	}
	// The failing tool result must be marked as an error message to the model.
	last := p.lastReq.Messages
	var foundErrResult bool
	for _, m := range last {
		for _, tr := range m.ToolResults {
			if tr.IsError && strings.Contains(tr.Content, "file not found") {
				foundErrResult = true
			}
		}
	}
	if !foundErrResult {
		t.Errorf("expected an IsError tool result carrying the error, messages=%+v", last)
	}
}

// imageTool is a fake ImageProducer: its Run returns a caption and it attaches
// a fixed image via Images.
type imageTool struct{ img ai.ImageContent }

func (imageTool) Name() string           { return "read_image" }
func (imageTool) Description() string    { return "fake image" }
func (imageTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (imageTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "attached image", nil
}
func (t imageTool) Images(_ json.RawMessage) ([]ai.ImageContent, error) {
	return []ai.ImageContent{t.img}, nil
}

// TestAgentImageToolAttachesImages verifies that a tool implementing
// ImageProducer has its images carried into the tool result sent to the model,
// and that read_image is gated at TierRead (no confirmation, even in manual
// mode with a denying confirm func).
func TestAgentImageToolAttachesImages(t *testing.T) {
	img := ai.ImageContent{MediaType: "image/png", Data: "QUJD"}
	reg := tools.NewRegistry(imageTool{img: img})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "read_image", `{"path":"pic.png"}`), doneEv()},
		{textEv("I can see it"), doneEv()},
	}}
	// Manual mode + alwaysDeny: a read must not be gated, so the loop proceeds.
	rs := run(t, p, agent.Config{Mode: agent.ModeManual}, alwaysDeny, reg, "look at pic.png")
	if !strings.Contains(joinText(rs), "I can see it") {
		t.Errorf("expected loop to continue past the read_image call, got %q", joinText(rs))
	}

	var found bool
	for _, m := range p.lastReq.Messages {
		for _, tr := range m.ToolResults {
			if tr.ToolUseID == "1" {
				if len(tr.Images) != 1 || tr.Images[0] != img {
					t.Errorf("tool result images = %+v, want [%+v]", tr.Images, img)
				}
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no tool result for read_image found in %+v", p.lastReq.Messages)
	}
}

// TestAgentVerboseEmitsTierLineAndOutput verifies verbose mode prints the tier
// banner and the tool output even on success.
func TestAgentVerboseEmitsTierLineAndOutput(t *testing.T) {
	reg := tools.NewRegistry(fakeTool{name: "read_file", out: "file contents here"})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "read_file", `{"path":"x"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	rs := run(t, p, agent.Config{Verbose: true}, alwaysApprove, reg, "read")
	out := joinText(rs)
	if !strings.Contains(out, "[Read]") {
		t.Errorf("expected verbose tier banner, got %q", out)
	}
	if !strings.Contains(out, "file contents here") {
		t.Errorf("expected verbose tool output, got %q", out)
	}
}

// TestAgentGitBranchInSystemPrompt verifies the configured git branch is
// injected into the system prompt sent to the provider.
func TestAgentGitBranchInSystemPrompt(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{{textEv("ok"), doneEv()}}}
	a := agent.NewWithRegistry(p, agent.Config{GitBranch: "feature/xyz"}, alwaysApprove, tools.NewRegistry())
	collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "hi"}, newSession(t)))
	if !strings.Contains(p.lastReq.System, "feature/xyz") {
		t.Errorf("expected git branch in system prompt, got %q", p.lastReq.System)
	}
}

// TestAgentReadToolRunsSilentlyInManualMode verifies a Read-tier tool executes
// without confirmation even in manual mode (confirmFn would deny).
func TestAgentReadToolRunsSilentlyInManualMode(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry(fakeTool{name: "grep", out: "match", calls: &calls})
	p := &scriptedProvider{turns: [][]ai.Event{
		{toolEv("1", "grep", `{"pattern":"x"}`), doneEv()},
		{textEv("done"), doneEv()},
	}}
	run(t, p, agent.Config{}, alwaysDeny, reg, "search")
	if calls != 1 {
		t.Errorf("read-tier tool should run without confirm, ran %d times", calls)
	}
}

// TestAgentCachesProjectContextAcrossTurns verifies the SPAI.md lookup is done
// once per Agent instance: the content injected on the first turn is still
// present on a second turn even after the file is deleted between turns, which
// can only hold if the on-disk read was cached rather than repeated per turn.
func TestAgentCachesProjectContextAcrossTurns(t *testing.T) {
	dir := t.TempDir()
	spaiMD := filepath.Join(dir, "SPAI.md")
	if err := os.WriteFile(spaiMD, []byte("CACHED PROJECT RULES"), 0644); err != nil {
		t.Fatal(err)
	}
	// A .git entry stops loadProjectContext's upward walk at dir.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	p := &scriptedProvider{} // each Stream call returns a bare end_turn
	a := agent.NewWithRegistry(p, agent.Config{WorkingDir: dir}, alwaysApprove, tools.NewRegistry())
	sess := newSession(t)

	// First turn: the file exists and its content must reach the system prompt.
	collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "one"}, sess))
	if !strings.Contains(p.lastReq.System, "CACHED PROJECT RULES") {
		t.Fatalf("first turn: SPAI.md content missing from system prompt: %q", p.lastReq.System)
	}

	// Delete the file, then run a second turn. Were the lookup repeated every
	// turn, the content would now be gone; the per-instance cache keeps it.
	if err := os.Remove(spaiMD); err != nil {
		t.Fatal(err)
	}
	collect(a.Run(context.Background(), &protocol.AgentRequest{Query: "two"}, sess))
	if !strings.Contains(p.lastReq.System, "CACHED PROJECT RULES") {
		t.Errorf("second turn: cached SPAI.md content missing after file deletion: %q", p.lastReq.System)
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

// TestAgentModelOverridePassedToStream verifies that Config.ModelOverride is
// forwarded as Request.Model in every Stream call inside the agent loop. This is
// the wiring that enables task-based model routing for reasoning turns.
func TestAgentModelOverridePassedToStream(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{
		{textEv("done"), doneEv()},
	}}
	cfg := agent.Config{ModelOverride: "claude-opus-4-8-override"}
	run(t, p, cfg, alwaysApprove, tools.NewRegistry(), "hi")

	if p.lastReq.Model != "claude-opus-4-8-override" {
		t.Errorf("Request.Model = %q, want claude-opus-4-8-override", p.lastReq.Model)
	}
}

// TestAgentModelOverrideEmptyMeansProviderDefault verifies that an empty
// ModelOverride passes "" as Request.Model, which every provider interprets as
// "use the configured default" — preserving backward-compatible behaviour.
func TestAgentModelOverrideEmptyMeansProviderDefault(t *testing.T) {
	p := &scriptedProvider{turns: [][]ai.Event{{textEv("ok"), doneEv()}}}
	run(t, p, agent.Config{}, alwaysApprove, tools.NewRegistry(), "hi")

	if p.lastReq.Model != "" {
		t.Errorf("Request.Model = %q, want empty (use provider default)", p.lastReq.Model)
	}
}
