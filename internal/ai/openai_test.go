package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spaish/internal/ai"
)

// minimalSSE writes two SSE events and a [DONE] sentinel so Stream returns
// without the goroutine blocking forever.
func minimalSSE(w http.ResponseWriter) {
	fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`)
	fmt.Fprintln(w, `data: [DONE]`)
}

func TestOpenAIReasoningEffort_Set(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		minimalSSE(w)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m", ai.RetryConfig{}).
		WithReasoningEffort("medium")
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "think"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// drain the channel so the goroutine finishes before we inspect gotBody
	for range ch {
	}

	got, ok := gotBody["reasoning_effort"]
	if !ok {
		t.Fatal("reasoning_effort field missing from request body")
	}
	if got != "medium" {
		t.Errorf("reasoning_effort = %q, want %q", got, "medium")
	}
}

func TestOpenAIReasoningEffort_Unset(t *testing.T) {
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		minimalSSE(w)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m", ai.RetryConfig{})
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "think"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if strings.Contains(rawBody, "reasoning_effort") {
		t.Errorf("reasoning_effort should be absent from request body, got: %s", rawBody)
	}
}

func TestOpenAIReasoningEffort_Empty(t *testing.T) {
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		minimalSSE(w)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m", ai.RetryConfig{}).
		WithReasoningEffort("")
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "think"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if strings.Contains(rawBody, "reasoning_effort") {
		t.Errorf("reasoning_effort should be absent when effort is empty string, got: %s", rawBody)
	}
}
