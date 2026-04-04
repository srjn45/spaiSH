package ai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spaios/internal/ai"
)

func TestCloudProviderComplete(t *testing.T) {
	// Mock OpenAI-compatible SSE response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hello"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" world"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	p := ai.NewCloudProvider(srv.URL, "test-key", "test-model")
	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "user", Content: "say hello"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		result.WriteString(chunk)
	}
	if result.String() != "Hello world" {
		t.Errorf("got %q, want %q", result.String(), "Hello world")
	}
}

func TestCloudProviderAvailable(t *testing.T) {
	p := ai.NewCloudProvider("https://api.example.com", "key", "model")
	if !p.Available() {
		t.Error("expected Available() = true when key and endpoint are set")
	}
	p2 := ai.NewCloudProvider("", "", "")
	if p2.Available() {
		t.Error("expected Available() = false when key and endpoint are empty")
	}
}
