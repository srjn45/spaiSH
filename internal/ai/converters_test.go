package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// This file is an internal (package ai) test. It exercises the unexported
// message/tool converters and helpers directly, which is far simpler than
// reconstructing every field-mapping branch from a captured HTTP request body.

func TestSplitSystem(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		sys, rest := splitSystem(nil)
		if sys != "" {
			t.Errorf("system = %q, want empty", sys)
		}
		if len(rest) != 0 {
			t.Errorf("rest = %v, want empty", rest)
		}
	})

	t.Run("no leading system", func(t *testing.T) {
		msgs := []Message{{Role: "user", Content: "hi"}}
		sys, rest := splitSystem(msgs)
		if sys != "" {
			t.Errorf("system = %q, want empty", sys)
		}
		if len(rest) != 1 || rest[0].Content != "hi" {
			t.Errorf("rest = %v, want the original messages unchanged", rest)
		}
	})

	t.Run("leading system split off", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		}
		sys, rest := splitSystem(msgs)
		if sys != "be terse" {
			t.Errorf("system = %q, want %q", sys, "be terse")
		}
		if len(rest) != 2 || rest[0].Role != "user" || rest[1].Role != "assistant" {
			t.Errorf("rest = %v, want the two non-system messages", rest)
		}
	})
}

func TestToAnthropicTools(t *testing.T) {
	tools := []ToolSpec{
		// Description present, required as a concrete []string, plus properties.
		{
			Name:        "with_string_required",
			Description: "has a description",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		},
		// required as []any (the shape produced by json.Unmarshal into map[string]any),
		// including a non-string element that must be skipped.
		{
			Name: "with_any_required",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"a", "b", 42},
			},
		},
		// No description, no properties, no required.
		{
			Name:   "bare",
			Schema: map[string]any{"type": "object"},
		},
	}

	out := toAnthropicTools(tools)
	if len(out) != 3 {
		t.Fatalf("got %d tools, want 3", len(out))
	}

	// First tool: description + properties + []string required.
	first := out[0].OfTool
	if first == nil {
		t.Fatal("out[0].OfTool is nil")
	}
	if first.Name != "with_string_required" {
		t.Errorf("name = %q", first.Name)
	}
	if !first.Description.Valid() || first.Description.Value != "has a description" {
		t.Errorf("description = %+v, want valid %q", first.Description, "has a description")
	}
	if first.InputSchema.Properties == nil {
		t.Error("expected properties to be carried through")
	}
	if !reflect.DeepEqual(first.InputSchema.Required, []string{"path"}) {
		t.Errorf("required = %v, want [path]", first.InputSchema.Required)
	}

	// Second tool: []any required with the non-string dropped.
	second := out[1].OfTool
	if !reflect.DeepEqual(second.InputSchema.Required, []string{"a", "b"}) {
		t.Errorf("required = %v, want [a b] (non-string dropped)", second.InputSchema.Required)
	}
	if second.Description.Valid() {
		t.Errorf("bare-ish tool should have no description, got %+v", second.Description)
	}

	// Third tool: nothing optional set.
	third := out[2].OfTool
	if third.Description.Valid() {
		t.Errorf("bare tool should have no description, got %+v", third.Description)
	}
	if len(third.InputSchema.Required) != 0 {
		t.Errorf("bare tool required = %v, want none", third.InputSchema.Required)
	}
}

// TestAddCacheControlToLastBlock covers every block variant the switch handles.
// CacheControlEphemeralParam marshals with omitzero, so a set breakpoint shows up
// as a "cache_control" key in the block's JSON and an unset one does not.
func TestAddCacheControlToLastBlock(t *testing.T) {
	hasCacheControl := func(t *testing.T, b anthropic.ContentBlockParamUnion) bool {
		t.Helper()
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return bytes.Contains(data, []byte(`"cache_control"`))
	}

	cases := map[string]anthropic.ContentBlockParamUnion{
		"text":        anthropic.NewTextBlock("hi"),
		"tool_result": anthropic.NewToolResultBlock("call_1", "ok", false),
		"tool_use":    anthropic.NewToolUseBlock("call_1", json.RawMessage(`{}`), "bash"),
		"image":       anthropic.NewImageBlockBase64("image/png", "aGk="),
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			b := block
			if hasCacheControl(t, b) {
				t.Fatal("block already had cache_control before the call")
			}
			addCacheControlToLastBlock(&b)
			if !hasCacheControl(t, b) {
				t.Errorf("cache_control not set on %s block", name)
			}
		})
	}
}

func TestToOpenAIMessages(t *testing.T) {
	img := ImageContent{MediaType: "image/png", Data: "aGk="}
	msgs := []Message{
		{Role: "user", Content: "look", Images: []ImageContent{img}},
		{Role: "assistant", Content: "sure", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "bash", Input: json.RawMessage(`{"cmd":"ls"}`)},
		}},
		{Role: "user", ToolResults: []ToolResult{
			{ToolUseID: "call_1", Content: "file.txt", Images: []ImageContent{img}},
		}},
	}

	out := toOpenAIMessages("be terse", msgs)

	// [0] system, [1] user(text+image parts), [2] assistant(+tool_calls),
	// [3] tool result, [4] forwarded user image message.
	if len(out) != 5 {
		t.Fatalf("got %d messages, want 5: %+v", len(out), out)
	}
	if out[0].Role != "system" || out[0].Content != "be terse" {
		t.Errorf("out[0] = %+v, want system message", out[0])
	}

	// User message with an image becomes a []openAIContentPart.
	parts, ok := out[1].Content.([]openAIContentPart)
	if !ok {
		t.Fatalf("out[1].Content is %T, want []openAIContentPart", out[1].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Errorf("out[1] parts = %+v, want text + image_url", parts)
	}

	if out[2].Role != "assistant" || len(out[2].ToolCalls) != 1 {
		t.Fatalf("out[2] = %+v, want assistant with 1 tool call", out[2])
	}
	if out[2].ToolCalls[0].Function.Name != "bash" || out[2].ToolCalls[0].Function.Arguments != `{"cmd":"ls"}` {
		t.Errorf("tool call = %+v", out[2].ToolCalls[0])
	}

	if out[3].Role != "tool" || out[3].ToolCallID != "call_1" || out[3].Content != "file.txt" {
		t.Errorf("out[3] = %+v, want tool result", out[3])
	}

	// Tool-produced images are forwarded as a following user message.
	if out[4].Role != "user" {
		t.Errorf("out[4] = %+v, want forwarded user image message", out[4])
	}
	if _, ok := out[4].Content.([]openAIContentPart); !ok {
		t.Errorf("out[4].Content is %T, want image parts", out[4].Content)
	}
}

func TestToOllamaMessages(t *testing.T) {
	img := ImageContent{MediaType: "image/png", Data: "aGk="}
	msgs := []Message{
		{Role: "user", Content: "look", Images: []ImageContent{img}},
		{Role: "assistant", Content: "sure", ToolCalls: []ToolCall{
			{ID: "call_1", Name: "bash", Input: json.RawMessage(`{"cmd":"ls"}`)},
		}},
		{Role: "user", ToolResults: []ToolResult{
			{ToolUseID: "call_1", Content: "file.txt", Images: []ImageContent{img}},
		}},
	}

	out := toOllamaMessages("be terse", msgs)

	// [0] system, [1] user(text+images), [2] assistant(+tool_calls),
	// [3] tool result, [4] forwarded user image message.
	if len(out) != 5 {
		t.Fatalf("got %d messages, want 5: %+v", len(out), out)
	}
	if out[0].Role != "system" || out[0].Content != "be terse" {
		t.Errorf("out[0] = %+v, want system message", out[0])
	}

	if out[1].Role != "user" || out[1].Content != "look" || len(out[1].Images) != 1 {
		t.Errorf("out[1] = %+v, want user with content + 1 image", out[1])
	}

	if out[2].Role != "assistant" || len(out[2].ToolCalls) != 1 {
		t.Fatalf("out[2] = %+v, want assistant with tool call", out[2])
	}
	if out[2].ToolCalls[0].Function.Name != "bash" {
		t.Errorf("tool call name = %q", out[2].ToolCalls[0].Function.Name)
	}

	if out[3].Role != "tool" || out[3].Content != "file.txt" {
		t.Errorf("out[3] = %+v, want tool result", out[3])
	}

	// Tool-produced images forwarded as a user message via the images field.
	if out[4].Role != "user" || len(out[4].Images) != 1 {
		t.Errorf("out[4] = %+v, want forwarded user image message", out[4])
	}
}

// stubProvider is a minimal Provider whose Stream replays a fixed event script.
// It backs the CompleteText tests without any HTTP plumbing.
type stubProvider struct {
	events    []Event
	streamErr error
}

func (s *stubProvider) Name() string    { return "stub" }
func (s *stubProvider) Available() bool { return true }

func (s *stubProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	return streamToTextCh(ctx, s, messages)
}

func (s *stubProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	ch := make(chan Event, len(s.events))
	for _, ev := range s.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func TestProviderErrorError(t *testing.T) {
	err := &ProviderError{Message: "boom"}
	if err.Error() != "boom" {
		t.Errorf("Error() = %q, want %q", err.Error(), "boom")
	}
}

func TestCompleteText(t *testing.T) {
	t.Run("concatenates text events", func(t *testing.T) {
		p := &stubProvider{events: []Event{
			{Type: "text", Text: "Hello"},
			{Type: "text", Text: " world"},
			{Type: "done", Stop: "end_turn"},
		}}
		got, err := CompleteText(context.Background(), p, "sys", nil)
		if err != nil {
			t.Fatalf("CompleteText: %v", err)
		}
		if got != "Hello world" {
			t.Errorf("got %q, want %q", got, "Hello world")
		}
	})

	t.Run("mid-stream error returns partial text and ProviderError", func(t *testing.T) {
		p := &stubProvider{events: []Event{
			{Type: "text", Text: "partial"},
			{Type: "error", Err: "kaboom"},
			{Type: "text", Text: "unreached"},
		}}
		got, err := CompleteText(context.Background(), p, "", nil)
		if got != "partial" {
			t.Errorf("got %q, want %q", got, "partial")
		}
		var pe *ProviderError
		if err == nil {
			t.Fatal("expected an error")
		}
		if pe2, ok := err.(*ProviderError); !ok {
			t.Fatalf("error type = %T, want *ProviderError", err)
		} else {
			pe = pe2
		}
		if pe.Message != "kaboom" {
			t.Errorf("error message = %q, want %q", pe.Message, "kaboom")
		}
	})

	t.Run("Stream setup error propagates", func(t *testing.T) {
		p := &stubProvider{streamErr: context.Canceled}
		got, err := CompleteText(context.Background(), p, "", nil)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
		if err != context.Canceled {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestStreamToTextCh(t *testing.T) {
	t.Run("drains text and skips non-text events", func(t *testing.T) {
		p := &stubProvider{events: []Event{
			{Type: "text", Text: "a"},
			{Type: "text", Text: ""}, // empty text is dropped
			{Type: "tool_call", ToolCall: &ToolCall{Name: "x"}},
			{Type: "text", Text: "b"},
			{Type: "done"},
		}}
		// Complete delegates to streamToTextCh; a leading system message exercises
		// the splitSystem path inside it.
		ch, err := p.Complete(context.Background(), []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hi"},
		})
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		var got string
		for s := range ch {
			got += s
		}
		if got != "ab" {
			t.Errorf("got %q, want %q", got, "ab")
		}
	})

	t.Run("Stream setup error propagates", func(t *testing.T) {
		p := &stubProvider{streamErr: context.Canceled}
		ch, err := p.Complete(context.Background(), nil)
		if err != context.Canceled {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if ch != nil {
			t.Error("expected nil channel on error")
		}
	})
}
