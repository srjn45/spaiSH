package router_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spaios/internal/ai"
	"spaios/internal/config"
	"spaios/internal/protocol"
	"spaios/internal/router"
	"spaios/internal/session"
)

// stubProvider is a fake AI provider for testing.
type stubProvider struct {
	response  string
	available bool
}

func (s *stubProvider) Available() bool { return s.available }
func (s *stubProvider) Complete(_ context.Context, _ []ai.Message) (<-chan string, error) {
	ch := make(chan string, 1)
	ch <- s.response
	close(ch)
	return ch, nil
}

func newTestSession(t *testing.T) *session.Session {
	t.Helper()
	s, _ := session.LoadFrom(filepath.Join(t.TempDir(), "session.json"))
	return s
}

func TestRouterTextResponse(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}
	cloud := &stubProvider{response: "No commands needed, everything looks fine.", available: true}
	local := &stubProvider{available: false}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{
		Type:       "query",
		Query:      "is my system healthy?",
		WorkingDir: "/home/user",
	}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}

	var gotTypes []string
	for resp := range ch {
		gotTypes = append(gotTypes, resp.Type)
	}

	if gotTypes[len(gotTypes)-1] != "done" {
		t.Error("last response should be 'done'")
	}
}

func TestRouterExtractsCommands(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}
	responseWithCommands := "The timeout is too low. Fix it:\n```bash\nsed -i 's/timeout 30/timeout 90/' /etc/nginx/nginx.conf\nsystemctl reload nginx\n```"
	cloud := &stubProvider{response: responseWithCommands, available: true}
	local := &stubProvider{available: false}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{
		Type:       "query",
		Query:      "fix nginx timeout",
		WorkingDir: "/home/user",
	}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}

	var plan []protocol.CommandItem
	for resp := range ch {
		if resp.Type == "plan" {
			plan = resp.Plan
		}
	}

	if len(plan) != 2 {
		t.Fatalf("expected 2 commands in plan, got %d", len(plan))
	}
	if !strings.Contains(plan[0].Command, "sed") {
		t.Errorf("expected sed command, got %q", plan[0].Command)
	}
	if plan[1].Tier != "elevated" {
		t.Errorf("expected systemctl to be elevated, got %q", plan[1].Tier)
	}
}

func TestRouterFallsBackToLocal(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}
	cloud := &stubProvider{available: false}
	local := &stubProvider{response: "All good.", available: true}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{Type: "query", Query: "check disk", WorkingDir: "/"}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}
	for range ch {
	}
}

func TestRouterNoProviderError(t *testing.T) {
	cfg := &config.Config{}
	cloud := &stubProvider{available: false}
	local := &stubProvider{available: false}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{Type: "query", Query: "test", WorkingDir: "/"}

	_, err := r.Route(context.Background(), req, newTestSession(t))
	if err == nil {
		t.Error("expected error when no provider available")
	}
}

// capturingProvider records the messages it receives for assertion.
type capturingProvider struct {
	stubProvider
	lastMessages []ai.Message
}

func (c *capturingProvider) Available() bool { return true }
func (c *capturingProvider) Complete(_ context.Context, msgs []ai.Message) (<-chan string, error) {
	c.lastMessages = append([]ai.Message(nil), msgs...)
	ch := make(chan string, 1)
	ch <- "All good."
	close(ch)
	return ch, nil
}

func TestRouterInjectsStdin(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}

	capturing := &capturingProvider{}

	r := router.New(cfg, capturing, &stubProvider{available: false})
	req := &protocol.Request{
		Type:       "query",
		Query:      "what does this output mean?",
		WorkingDir: "/home/user",
		Stdin:      "file1.go\nfile2.go\n",
	}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}
	for range ch {
	}

	capturedMessages := capturing.lastMessages
	if len(capturedMessages) < 3 {
		t.Fatalf("expected at least 3 messages (system + piped + query), got %d", len(capturedMessages))
	}

	var pipedIdx, queryIdx int = -1, -1
	for i, m := range capturedMessages {
		if m.Role == "user" && strings.Contains(m.Content, "[piped input]") {
			pipedIdx = i
		}
		if m.Role == "user" && m.Content == "what does this output mean?" {
			queryIdx = i
		}
	}
	if pipedIdx == -1 {
		t.Error("expected a [piped input] user message")
	}
	if queryIdx == -1 {
		t.Error("expected the query user message")
	}
	if pipedIdx >= queryIdx {
		t.Errorf("[piped input] (idx %d) must come before query (idx %d)", pipedIdx, queryIdx)
	}
	if !strings.Contains(capturedMessages[pipedIdx].Content, "file1.go") {
		t.Errorf("piped input should contain stdin content, got: %q", capturedMessages[pipedIdx].Content)
	}
}

func init() {
	// Ensure test temp dirs work
	os.MkdirAll(os.TempDir(), 0755)
}
