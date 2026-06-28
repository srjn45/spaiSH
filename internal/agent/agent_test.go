package agent_test

import (
	"context"
	"strings"
	"testing"

	"spaish/internal/agent"
	"spaish/internal/ai"
	"spaish/internal/protocol"
	"spaish/internal/session"
)

// mockProvider returns a fixed sequence of text responses.
type mockProvider struct {
	responses []string
	idx       int
}

func (m *mockProvider) Available() bool { return true }
func (m *mockProvider) Name() string    { return "mock" }
func (m *mockProvider) Stream(_ context.Context, _ ai.Request) (<-chan ai.Event, error) {
	ch := make(chan ai.Event)
	close(ch)
	return ch, nil
}
func (m *mockProvider) Complete(_ context.Context, _ []ai.Message) (<-chan string, error) {
	ch := make(chan string, 1)
	resp := ""
	if m.idx < len(m.responses) {
		resp = m.responses[m.idx]
		m.idx++
	}
	go func() { ch <- resp; close(ch) }()
	return ch, nil
}

// alwaysApprove is a ConfirmFunc that always approves.
func alwaysApprove(_ protocol.ConfirmRequest) bool { return true }

// alwaysDeny is a ConfirmFunc that always denies.
func alwaysDeny(_ protocol.ConfirmRequest) bool { return false }

// collectResponses drains the channel and returns all items.
func collectResponses(ch <-chan protocol.Response) []protocol.Response {
	var out []protocol.Response
	for r := range ch {
		out = append(out, r)
	}
	return out
}

func newSession(t *testing.T) *session.Session {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s, _ := session.LoadByID("test")
	return s
}

// successExec always returns exit 0.
func successExec(_ context.Context, _ string) (string, int) {
	return "ok\n", 0
}

// failExec always returns exit 1.
func failExec(_ context.Context, _ string) (string, int) {
	return "permission denied\n", 1
}

// capturingMockProvider records the first call's message slice.
type capturingMockProvider struct {
	mockProvider
	lastMessages []ai.Message
}

func (c *capturingMockProvider) Available() bool { return true }
func (c *capturingMockProvider) Complete(ctx context.Context, msgs []ai.Message) (<-chan string, error) {
	if c.lastMessages == nil {
		c.lastMessages = append([]ai.Message(nil), msgs...)
	}
	return c.mockProvider.Complete(ctx, msgs)
}

func TestAgentNoCommandsOnFirstTry(t *testing.T) {
	// AI returns no bash block — goal immediately achieved.
	p := &mockProvider{responses: []string{"Done, no commands needed."}}
	cfg := agent.Config{MaxIterations: 3}
	a := agent.NewWithExec(p, cfg, alwaysApprove, successExec)

	resps := collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "what time is it"}, newSession(t)))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected last type 'done', got %q", last.Type)
	}
	var text string
	for _, r := range resps {
		if r.Type == "text" {
			text += r.Content
		}
	}
	if !strings.Contains(text, "Done") {
		t.Errorf("expected AI text in response, got: %q", text)
	}
}

func TestAgentCommandSucceedsThenDone(t *testing.T) {
	// First AI call returns a command; second AI call returns no commands.
	p := &mockProvider{responses: []string{
		"I will list files.\n```bash\nls\n```",
		"Goal achieved.",
	}}
	cfg := agent.Config{MaxIterations: 5}
	a := agent.NewWithExec(p, cfg, alwaysApprove, successExec)

	resps := collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "list files"}, newSession(t)))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
}

func TestAgentFixesFailureThenDone(t *testing.T) {
	// First command fails; AI proposes fix; fix succeeds; AI says done.
	p := &mockProvider{responses: []string{
		"Running the command.\n```bash\nls /nonexistent\n```",
		"That path doesn't exist. Let me fix it.\n```bash\nls /tmp\n```",
		"All done.",
	}}
	cfg := agent.Config{MaxIterations: 5}

	execFn := func(_ context.Context, cmd string) (string, int) {
		if cmd == "ls /nonexistent" {
			return "No such file or directory\n", 1
		}
		return "tmp contents\n", 0
	}

	a := agent.NewWithExec(p, cfg, alwaysApprove, execFn)
	resps := collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "list nonexistent"}, newSession(t)))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
}

func TestAgentMaxIterationsReached(t *testing.T) {
	// AI always returns a failing command — should stop at max_iterations.
	responses := make([]string, 10)
	for i := range responses {
		responses[i] = "Trying again.\n```bash\nls /nope\n```"
	}
	p := &mockProvider{responses: responses}
	cfg := agent.Config{MaxIterations: 3}
	a := agent.NewWithExec(p, cfg, alwaysApprove, failExec)

	resps := collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "do something"}, newSession(t)))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done' at cap, got %q", last.Type)
	}
	var text string
	for _, r := range resps {
		text += r.Content
	}
	if !strings.Contains(text, "iteration limit") {
		t.Errorf("expected 'iteration limit' message, got: %q", text)
	}
}

func TestAgentConfirmationDeniedCancels(t *testing.T) {
	// Write-tier command, user denies — loop should cancel cleanly.
	p := &mockProvider{responses: []string{
		"Creating a file.\n```bash\ntouch /tmp/test.txt\n```",
	}}
	cfg := agent.Config{MaxIterations: 5}
	a := agent.NewWithExec(p, cfg, alwaysDeny, successExec)

	resps := collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "create file"}, newSession(t)))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done' after cancel, got %q", last.Type)
	}
	var text string
	for _, r := range resps {
		text += r.Content
	}
	if !strings.Contains(strings.ToLower(text), "cancel") {
		t.Errorf("expected cancellation message, got: %q", text)
	}
}

func TestAgentAutonomousSkipsConfirm(t *testing.T) {
	// autonomous=true — confirm function must never be called.
	confirmCalled := false
	confirmFn := func(_ protocol.ConfirmRequest) bool {
		confirmCalled = true
		return true
	}
	p := &mockProvider{responses: []string{
		"Touching a file.\n```bash\ntouch /tmp/auto.txt\n```",
		"Done.",
	}}
	cfg := agent.Config{MaxIterations: 5, Autonomous: true}
	a := agent.NewWithExec(p, cfg, confirmFn, successExec)

	collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "create file"}, newSession(t)))

	if confirmCalled {
		t.Error("confirm function must not be called in autonomous mode")
	}
}

func TestAgentVerboseIncludesIterationHeader(t *testing.T) {
	// After the first iteration (which fails), verbose mode should include a header.
	p := &mockProvider{responses: []string{
		"First try.\n```bash\nls /bad\n```",
		"Fix try.\n```bash\nls /tmp\n```",
		"Done.",
	}}
	cfg := agent.Config{MaxIterations: 5, Verbose: true}
	execFn := func(_ context.Context, cmd string) (string, int) {
		if cmd == "ls /bad" {
			return "error\n", 1
		}
		return "ok\n", 0
	}
	a := agent.NewWithExec(p, cfg, alwaysApprove, execFn)

	resps := collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "q", Verbose: false}, newSession(t)))

	var text string
	for _, r := range resps {
		text += r.Content
	}
	if !strings.Contains(text, "iteration") {
		t.Errorf("expected iteration header in verbose mode, got: %q", text)
	}
}

func TestAgentInjectsStdinBeforeQuery(t *testing.T) {
	p := &capturingMockProvider{
		mockProvider: mockProvider{responses: []string{"Done, no commands needed."}},
	}

	cfg := agent.Config{
		MaxIterations: 3,
		Stdin:         "total 42\nfoo.go  bar.go\n",
	}
	a := agent.NewWithExec(p, cfg, alwaysApprove, successExec)
	collectResponses(a.Run(context.Background(), &protocol.AgentRequest{Query: "explain this"}, newSession(t)))

	capturedMessages := p.lastMessages
	var pipedIdx, queryIdx int = -1, -1
	for i, m := range capturedMessages {
		if m.Role == "user" && strings.Contains(m.Content, "[piped input]") {
			pipedIdx = i
		}
		if m.Role == "user" && m.Content == "explain this" {
			queryIdx = i
		}
	}
	if pipedIdx == -1 {
		t.Error("expected [piped input] message in agent messages")
	}
	if queryIdx == -1 {
		t.Error("expected query message in agent messages")
	}
	if pipedIdx >= queryIdx {
		t.Errorf("[piped input] (idx %d) must precede query (idx %d)", pipedIdx, queryIdx)
	}
	if !strings.Contains(capturedMessages[pipedIdx].Content, "foo.go") {
		t.Errorf("piped input content missing, got: %q", capturedMessages[pipedIdx].Content)
	}
}
