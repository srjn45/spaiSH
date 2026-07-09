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

// TestOllamaInlineToolCallFallback covers local models that emit tool calls as
// JSON text (often fenced) instead of structured tool_calls.
func TestOllamaInlineToolCallFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			// Non-streaming response whose content embeds tool calls as text.
			body := map[string]any{
				"message": map[string]any{
					"role": "assistant",
					"content": "I'll do it.\n```json\n" +
						`{"name": "write_file", "arguments": {"path": "/tmp/x", "content": "hi"}}` +
						"\n```\nThen read it:\n```json\n" +
						`{"name": "read_file", "arguments": {"path": "/tmp/x"}}` +
						"\n```",
				},
				"done": true,
			}
			json.NewEncoder(w).Encode(body)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "write then read"}},
		Tools: []ai.ToolSpec{
			{Name: "write_file", Schema: map[string]any{"type": "object"}},
			{Name: "read_file", Schema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var calls []*ai.ToolCall
	for _, ev := range drain(t, ch) {
		if ev.Type == "tool_call" {
			calls = append(calls, ev.ToolCall)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 recovered tool calls, got %d", len(calls))
	}
	if calls[0].Name != "write_file" || calls[1].Name != "read_file" {
		t.Errorf("unexpected recovered calls: %s, %s", calls[0].Name, calls[1].Name)
	}
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(calls[0].Input, &args); err != nil || args.Path != "/tmp/x" || args.Content != "hi" {
		t.Errorf("bad recovered args: %s (err=%v)", calls[0].Input, err)
	}
}

// TestOpenAIMalformedSSE confirms a malformed data line is skipped rather than
// aborting the stream: the following well-formed delta still yields its text.
func TestOpenAIMalformedSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `data: {not valid json`)
		fmt.Fprintln(w, `garbage line without data prefix`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`)
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
	if text.String() != "ok" {
		t.Errorf("got %q, want %q (malformed line should be skipped)", text.String(), "ok")
	}
}

// TestOpenAIEmptyToolArgs pins the current behavior for a tool call whose
// arguments never arrive (empty) or arrive as malformed JSON: the provider does
// not panic and surfaces the raw (possibly empty/invalid) Input verbatim,
// leaving JSON validation to the caller.
func TestOpenAIEmptyToolArgs(t *testing.T) {
	t.Run("missing arguments", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		}))
		defer srv.Close()

		p := ai.NewOpenAIProvider(srv.URL, "k", "m")
		ch, err := p.Stream(context.Background(), ai.Request{
			Messages: []ai.Message{{Role: "user", Content: "go"}},
			Tools:    []ai.ToolSpec{{Name: "bash", Schema: map[string]any{"type": "object"}}},
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
			t.Errorf("name = %q, want bash", got.Name)
		}
		if len(got.Input) != 0 {
			t.Errorf("Input = %q, want empty for missing arguments", got.Input)
		}
	})

	t.Run("malformed arguments preserved verbatim", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{bad"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		}))
		defer srv.Close()

		p := ai.NewOpenAIProvider(srv.URL, "k", "m")
		ch, err := p.Stream(context.Background(), ai.Request{
			Messages: []ai.Message{{Role: "user", Content: "go"}},
			Tools:    []ai.ToolSpec{{Name: "bash", Schema: map[string]any{"type": "object"}}},
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
		if string(got.Input) != "{bad" {
			t.Errorf("Input = %q, want raw %q preserved", got.Input, "{bad")
		}
		if json.Valid(got.Input) {
			t.Error("expected malformed Input to be invalid JSON (pinning current behavior)")
		}
	})
}

// TestOpenAINon200 confirms a non-200 status is surfaced as a Stream error
// before any events flow.
func TestOpenAINon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m")
	_, err := p.Stream(context.Background(), ai.Request{Messages: []ai.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected an error for non-200 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention HTTP 500", err)
	}
}

// TestOpenAICompleteDelegation exercises Complete → streamToTextCh → splitSystem
// for the OpenAI provider. The leading system message must be split out-of-band.
func TestOpenAICompleteDelegation(t *testing.T) {
	var sawSystem bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			if m.Role == "system" {
				sawSystem = true
			}
		}
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hi "}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"there"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	p := ai.NewOpenAIProvider(srv.URL, "k", "m")
	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var got strings.Builder
	for s := range ch {
		got.WriteString(s)
	}
	if got.String() != "hi there" {
		t.Errorf("got %q, want %q", got.String(), "hi there")
	}
	if !sawSystem {
		t.Error("system message was not forwarded to the provider")
	}
}

// TestOllamaMalformedLine confirms an unparseable JSON line is skipped and later
// good lines still stream through.
func TestOllamaMalformedLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			fmt.Fprintln(w, `{not json}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"done"},"done":true}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
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
	if text.String() != "done" {
		t.Errorf("got %q, want %q (malformed line should be skipped)", text.String(), "done")
	}
}

// TestOllamaNon200 confirms a non-200 status from /api/chat is surfaced as a
// Stream error.
func TestOllamaNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	_, err := p.Stream(context.Background(), ai.Request{Messages: []ai.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected an error for non-200 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention HTTP 500", err)
	}
}

// TestOllamaEmptyToolArgs pins behavior for a native tool call carrying an empty
// arguments object.
func TestOllamaEmptyToolArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"now","arguments":{}}}]},"done":true}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "time?"}},
		Tools:    []ai.ToolSpec{{Name: "now", Schema: map[string]any{"type": "object"}}},
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
	if got == nil || got.Name != "now" {
		t.Fatalf("expected 'now' tool_call, got %+v", got)
	}
	if string(got.Input) != "{}" {
		t.Errorf("Input = %q, want %q for empty arguments object", got.Input, "{}")
	}
}

// TestOllamaCompleteDelegation exercises Complete → streamToTextCh → splitSystem
// for the Ollama provider.
func TestOllamaCompleteDelegation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"hi "},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"there"},"done":true}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var got strings.Builder
	for s := range ch {
		got.WriteString(s)
	}
	if got.String() != "hi there" {
		t.Errorf("got %q, want %q", got.String(), "hi there")
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

// minimalSSEResponse returns a complete text/event-stream body for the Anthropic
// streaming API. inputTokens/outputTokens/cacheCreation/cacheRead populate the
// usage fields so tests can verify they are correctly extracted.
func minimalSSEResponse(text string, inputTokens, outputTokens, cacheCreation, cacheRead int) string {
	return fmt.Sprintf(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"stop_reason\":null,\"usage\":{\"input_tokens\":%d,\"output_tokens\":0,\"cache_creation_input_tokens\":%d,\"cache_read_input_tokens\":%d}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		inputTokens, cacheCreation, cacheRead, text, outputTokens,
	)
}

// TestAnthropicCacheControlBreakpoints verifies that Stream sets cache_control
// on the system block, the last tool, and the penultimate message's last block.
func TestAnthropicCacheControlBreakpoints(t *testing.T) {
	type reqBody struct {
		System []struct {
			Text         string `json:"text"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"system"`
		Tools []struct {
			Name         string `json:"name"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"tools"`
		Messages []struct {
			Content []struct {
				Type         string `json:"type"`
				Text         string `json:"text,omitempty"`
				CacheControl *struct {
					Type string `json:"type"`
				} `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}

	var captured reqBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, minimalSSEResponse("ok", 10, 5, 0, 0))
	}))
	defer srv.Close()

	p := ai.NewAnthropicProviderWithBase("sk-test", "claude-opus-4-8", srv.URL)
	ch, err := p.Stream(context.Background(), ai.Request{
		System: "You are a helpful assistant.",
		Messages: []ai.Message{
			{Role: "user", Content: "first turn"},
			{Role: "assistant", Content: "first response"},
			{Role: "user", Content: "second turn"},
		},
		Tools: []ai.ToolSpec{
			{Name: "alpha", Description: "tool alpha", Schema: map[string]any{"type": "object"}},
			{Name: "beta", Description: "tool beta", Schema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	drain(t, ch) // consume events

	// System prompt cache breakpoint.
	if len(captured.System) == 0 {
		t.Fatal("no system blocks in request")
	}
	if captured.System[0].CacheControl == nil || captured.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("system[0] cache_control = %v, want {type:ephemeral}", captured.System[0].CacheControl)
	}

	// Last tool cache breakpoint.
	if len(captured.Tools) < 2 {
		t.Fatalf("expected 2 tools, got %d", len(captured.Tools))
	}
	if captured.Tools[0].CacheControl != nil {
		t.Errorf("tools[0] should not have cache_control, got %v", captured.Tools[0].CacheControl)
	}
	if captured.Tools[1].CacheControl == nil || captured.Tools[1].CacheControl.Type != "ephemeral" {
		t.Errorf("tools[1] (last) cache_control = %v, want {type:ephemeral}", captured.Tools[1].CacheControl)
	}

	// Penultimate message last content block cache breakpoint.
	if len(captured.Messages) < 2 {
		t.Fatalf("expected ≥2 messages, got %d", len(captured.Messages))
	}
	penultimate := captured.Messages[len(captured.Messages)-2]
	if len(penultimate.Content) == 0 {
		t.Fatal("penultimate message has no content blocks")
	}
	lastBlock := penultimate.Content[len(penultimate.Content)-1]
	if lastBlock.CacheControl == nil || lastBlock.CacheControl.Type != "ephemeral" {
		t.Errorf("penultimate message last block cache_control = %v, want {type:ephemeral}", lastBlock.CacheControl)
	}
}

// TestAnthropicCompleteDelegation verifies Complete drives Stream via
// streamToTextCh, splitting the leading system message out-of-band and
// concatenating the streamed text deltas.
func TestAnthropicCompleteDelegation(t *testing.T) {
	var gotSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			System []struct {
				Text string `json:"text"`
			} `json:"system"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.System) > 0 {
			gotSystem = body.System[0].Text
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, minimalSSEResponse("hello world", 10, 5, 0, 0))
	}))
	defer srv.Close()

	p := ai.NewAnthropicProviderWithBase("sk-test", "claude-opus-4-8", srv.URL)
	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var got strings.Builder
	for s := range ch {
		got.WriteString(s)
	}
	if got.String() != "hello world" {
		t.Errorf("got %q, want %q", got.String(), "hello world")
	}
	if gotSystem != "be terse" {
		t.Errorf("system forwarded = %q, want %q (splitSystem should hoist it)", gotSystem, "be terse")
	}
}

// TestAnthropicStreamError confirms a non-200 API response is surfaced as an
// "error" event rather than a panic or a silent empty stream.
func TestAnthropicStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 400 is non-retryable, so the SDK surfaces it promptly.
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)
	}))
	defer srv.Close()

	p := ai.NewAnthropicProviderWithBase("sk-test", "claude-opus-4-8", srv.URL)
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var sawError bool
	for _, ev := range drain(t, ch) {
		if ev.Type == "error" {
			sawError = true
			if ev.Err == "" {
				t.Error("error event has an empty message")
			}
		}
	}
	if !sawError {
		t.Error("expected an 'error' event for a 400 response")
	}
}

// TestAnthropicUsageExtraction verifies that the "done" event carries real
// token counts from the accumulator's Usage field.
func TestAnthropicUsageExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, minimalSSEResponse("hello", 100, 42, 50, 20))
	}))
	defer srv.Close()

	p := ai.NewAnthropicProviderWithBase("sk-test", "claude-opus-4-8", srv.URL)
	ch, err := p.Stream(context.Background(), ai.Request{
		Messages: []ai.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var doneEv *ai.Event
	for ev := range ch {
		if ev.Type == "done" {
			cp := ev
			doneEv = &cp
		}
	}
	if doneEv == nil {
		t.Fatal("no 'done' event received")
	}
	u := doneEv.Usage
	if u == nil {
		t.Fatal("done event Usage is nil")
	}
	if u.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", u.InputTokens)
	}
	if u.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want 42", u.OutputTokens)
	}
	if u.CacheCreationTokens != 50 {
		t.Errorf("CacheCreationTokens = %d, want 50", u.CacheCreationTokens)
	}
	if u.CacheReadTokens != 20 {
		t.Errorf("CacheReadTokens = %d, want 20", u.CacheReadTokens)
	}
}
