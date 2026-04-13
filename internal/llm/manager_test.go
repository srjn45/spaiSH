package llm_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	// Force all step checks to report "not done" so we always get the full plan.
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999").
		WithStepChecker(func(string) bool { return false })
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
	// Force all step checks to pass so install sees everything as already done.
	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL).
		WithStepChecker(func(string) bool { return true })
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
		t.Error("expected no plan when Ollama already installed")
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

func TestManagerUninstallOllamaReturnsPlan(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "uninstall",
		Args:    []string{"ollama"},
	}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) == 0 {
		t.Error("expected at least one command in uninstall plan")
	}
}

func TestManagerUninstallBitnetReturnsPlan(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "uninstall",
		Args:    []string{"bitnet"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) == 0 {
		t.Fatal("expected at least one command in bitnet uninstall plan")
	}
	// The bitnet uninstall should remove the install directory.
	found := false
	for _, item := range plan {
		if strings.Contains(item.Command, "rm -rf") && strings.Contains(item.Command, "bitnet") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected rm -rf of bitnet dir, got plan: %v", plan)
	}
}

func TestManagerUninstallUnknownRuntimeReturnsError(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "uninstall",
		Args:    []string{"vllm"},
	}))

	var hasError bool
	for _, r := range resps {
		if r.Type == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error for unknown runtime uninstall")
	}
}

func TestManagerPullBitnetUsesSetupEnv(t *testing.T) {
	state := newTestState(t)
	state.SetRuntime("bitnet", "", "http://localhost:8080")
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "pull",
		Args:    []string{"BitNet-b1.58-3B"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 command in bitnet pull plan, got %d", len(plan))
	}
	if !strings.Contains(plan[0].Command, "setup_env.py") {
		t.Errorf("expected setup_env.py command for bitnet pull, got: %q", plan[0].Command)
	}
	if !strings.Contains(plan[0].Command, "BitNet-b1.58-3B") {
		t.Errorf("expected model name in pull command, got: %q", plan[0].Command)
	}
}

func TestManagerRemoveBitnetUsesRmRf(t *testing.T) {
	state := newTestState(t)
	state.SetRuntime("bitnet", "", "http://localhost:8080")
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "remove",
		Args:    []string{"BitNet-b1.58-3B"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 command in bitnet remove plan, got %d", len(plan))
	}
	if !strings.Contains(plan[0].Command, "rm -rf") {
		t.Errorf("expected rm -rf for bitnet remove, got: %q", plan[0].Command)
	}
	if !strings.Contains(plan[0].Command, "BitNet-b1.58-3B") {
		t.Errorf("expected model name in remove command, got: %q", plan[0].Command)
	}
}

func TestManagerInstallBitnetFullPlanWhenNotInstalled(t *testing.T) {
	state := newTestState(t)
	// Force all step checks to fail — simulate a fresh system.
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999").
		WithStepChecker(func(string) bool { return false })
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "install",
		Args:    []string{"bitnet"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) == 0 {
		t.Fatal("expected install plan for bitnet")
	}
	// First command should be git clone.
	if !strings.Contains(plan[0].Command, "git clone") {
		t.Errorf("expected git clone as first bitnet install step, got: %q", plan[0].Command)
	}
}

func TestManagerInstallBitnetResumeSkipsCompletedSteps(t *testing.T) {
	state := newTestState(t)
	// Simulate: git clone done (step 0 passes), pip install and setup_env not done.
	stepCount := 0
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999").
		WithStepChecker(func(string) bool {
			done := stepCount == 0
			stepCount++
			return done
		})
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "install",
		Args:    []string{"bitnet"},
	}))

	var plan []protocol.CommandItem
	var fullText string
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
		if r.Type == "text" {
			fullText += r.Content
		}
	}
	// Should have 3 remaining steps (apt install + pip install + setup_env), not 4.
	if len(plan) != 3 {
		t.Errorf("expected 3 pending steps after resuming, got %d: %v", len(plan), plan)
	}
	// Should mention "Resuming".
	if !strings.Contains(fullText, "Resuming") {
		t.Errorf("expected 'Resuming' in response text, got: %q", fullText)
	}
}

func TestManagerInstallBitnetAlreadyDone(t *testing.T) {
	state := newTestState(t)
	// All step checks pass — fully installed.
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999").
		WithStepChecker(func(string) bool { return true })
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "install",
		Args:    []string{"bitnet"},
	}))

	var hadPlan bool
	for _, r := range resps {
		if r.Type == "plan" {
			hadPlan = true
		}
	}
	if hadPlan {
		t.Error("expected no install plan when bitnet is fully installed")
	}
}
