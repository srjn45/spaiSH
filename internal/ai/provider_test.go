package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spaish/internal/ai"
)

// drain collects all events from a provider stream.
func drain(t *testing.T, ch <-chan ai.Event) []ai.Event {
	t.Helper()
	var out []ai.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestOpenAIStreamText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing/incorrect auth header")
		}
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hello"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" world"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m")
	ch, err := p.Stream(context.Background(), ai.Request{Messages: []ai.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	for _, ev := range drain(t, ch) {
		if ev.Type == "text" {
			text.WriteString(ev.Text)
		}
	}
	if text.String() != "Hello world" {
		t.Errorf("got %q, want %q", text.String(), "Hello world")
	}
}

func TestOpenAIStreamToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":"}}]}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m")
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "list files"}},
		Tools:    []ai.ToolSpec{{Name: "bash", Description: "run a command", Schema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got *ai.ToolCall
	for _, ev := range drain(t, ch) {
		if ev.Type == "tool_call" {
			got = ev.ToolCall
		}
	}
	if got == nil {
		t.Fatal("expected a tool_call event")
	}
	if got.Name != "bash" {
		t.Errorf("tool name = %q, want bash", got.Name)
	}
	var args struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal(got.Input, &args); err != nil {
		t.Fatalf("tool input not valid JSON: %v (%s)", err, got.Input)
	}
	if args.Cmd != "ls" {
		t.Errorf("cmd = %q, want ls", args.Cmd)
	}
}

func TestOllamaStreamToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"on it"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"bash","arguments":{"cmd":"ls"}}}]},"done":true}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	if !p.Available() {
		t.Fatal("Available() = false, want true")
	}
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "list files"}},
		Tools:    []ai.ToolSpec{{Name: "bash", Schema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	var got *ai.ToolCall
	for _, ev := range drain(t, ch) {
		switch ev.Type {
		case "text":
			text.WriteString(ev.Text)
		case "tool_call":
			got = ev.ToolCall
		}
	}
	if text.String() != "on it" {
		t.Errorf("text = %q, want %q", text.String(), "on it")
	}
	if got == nil || got.Name != "bash" {
		t.Fatalf("expected bash tool_call, got %+v", got)
	}
	if !json.Valid(got.Input) {
		t.Errorf("tool input not valid JSON: %s", got.Input)
	}
}

func TestAnthropicAvailableAndName(t *testing.T) {
	p := ai.NewAnthropicProvider("sk-test", "")
	if !p.Available() {
		t.Error("expected Available() = true with an API key")
	}
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want anthropic", p.Name())
	}
	if ai.NewAnthropicProvider("", "").Available() {
		t.Error("expected Available() = false without an API key")
	}
}
