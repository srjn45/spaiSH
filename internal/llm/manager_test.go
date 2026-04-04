package llm_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"spaios/internal/llm"
	"spaios/internal/protocol"
)

// newTestState returns an in-memory state pointing to a temp file.
func newTestState(t *testing.T) *llm.State {
	t.Helper()
	s, _ := llm.LoadState(filepath.Join(t.TempDir(), "llm-state.json"))
	return s
}

// collectResponses drains the response channel and returns all items.
func collectResponses(ch <-chan protocol.Response) []protocol.Response {
	var out []protocol.Response
	for r := range ch {
		out = append(out, r)
	}
	return out
}

func TestManagerStatusOllamaRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"models":[{"name":"qwen2.5-coder:7b"},{"name":"llama3.2:3b"}]}`)
		}
	}))
	defer srv.Close()

	state := newTestState(t)
	state.SetRuntime("ollama", "0.6.1", srv.URL)
	state.SetActiveModel("qwen2.5-coder:7b")

	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL)
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "status"}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected last response type 'done', got %q", last.Type)
	}

	var fullText string
	for _, r := range resps {
		if r.Type == "text" {
			fullText += r.Content
		}
	}
	if fullText == "" {
		t.Error("expected non-empty text response for status")
	}
}

func TestManagerStatusOllamaNotRunning(t *testing.T) {
	state := newTestState(t)
	// Point at a port nothing is listening on
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "status"}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
}

func TestManagerInstallReturnsPlan(t *testing.T) {
	state := newTestState(t)
	// Point at port nothing is listening on — ollama not running
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "install"}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) == 0 {
		t.Error("expected at least one command in install plan")
	}
	if plan[0].Tier == "" {
		t.Error("expected non-empty tier on install command")
	}
}

func TestManagerInstallAlreadyInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprintln(w, `{"models":[]}`)
		}
	}))
	defer srv.Close()

	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL)
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "install"}))

	var hadPlan bool
	var hadText bool
	for _, r := range resps {
		if r.Type == "plan" {
			hadPlan = true
		}
		if r.Type == "text" {
			hadText = true
		}
	}
	if hadPlan {
		t.Error("expected no plan when Ollama already running")
	}
	if !hadText {
		t.Error("expected text response saying already installed")
	}
}

func TestManagerListShowsRecommended(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprintln(w, `{"models":[{"name":"qwen2.5-coder:7b"}]}`)
		}
	}))
	defer srv.Close()

	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL)
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "list"}))

	var fullText string
	for _, r := range resps {
		if r.Type == "text" {
			fullText += r.Content
		}
	}
	if fullText == "" {
		t.Error("expected text output from list command")
	}
}

func TestManagerPullReturnsPlan(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "pull",
		Args:    []string{"llama3.2:3b"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 command in pull plan, got %d", len(plan))
	}
	if plan[0].Command != "ollama pull llama3.2:3b" {
		t.Errorf("unexpected pull command: %q", plan[0].Command)
	}
}

func TestManagerPullMissingArg(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "pull"}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
	var hasError bool
	for _, r := range resps {
		if r.Type == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error response when pull called without args")
	}
}

func TestManagerUseUpdatesState(t *testing.T) {
	dir := t.TempDir()
	state, _ := llm.LoadState(filepath.Join(dir, "llm-state.json"))
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")

	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "use",
		Args:    []string{"mistral:7b"},
	}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
	if state.ActiveModel != "mistral:7b" {
		t.Errorf("expected active model 'mistral:7b', got %q", state.ActiveModel)
	}
}

func TestManagerRemoveReturnsPlan(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "remove",
		Args:    []string{"llama3.2:3b"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 command in remove plan, got %d", len(plan))
	}
	if plan[0].Command != "ollama rm llama3.2:3b" {
		t.Errorf("unexpected remove command: %q", plan[0].Command)
	}
}

func TestManagerRemoveMissingArg(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "remove"}))

	var hasError bool
	for _, r := range resps {
		if r.Type == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error response when remove called without args")
	}
}

func TestManagerUnknownCommand(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "bogus"}))

	var hasError bool
	for _, r := range resps {
		if r.Type == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error response for unknown command")
	}
}
