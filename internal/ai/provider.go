package ai

import (
	"context"
	"encoding/json"
	"strings"
)

// Message is a single turn in a conversation. Text-only callers use Role +
// Content; tool-calling callers also populate ToolCalls (on assistant turns)
// and ToolResults (on user turns).
type Message struct {
	Role        string       // "system", "user", "assistant"
	Content     string       // text content
	ToolCalls   []ToolCall   // assistant-initiated tool invocations
	ToolResults []ToolResult // results fed back to the model
}

// ToolSpec describes a tool offered to the model.
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any // JSON Schema for the tool input ("object" schema)
}

// ToolCall is a model request to invoke a tool.
type ToolCall struct {
	ID    string          // provider-assigned id, echoed in the matching ToolResult
	Name  string          // tool name
	Input json.RawMessage // raw JSON arguments
}

// ToolResult is the outcome of executing a ToolCall.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// Request is a tool-calling completion request.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
	Model    string // optional override; providers fall back to their configured model
}

// Event is one item in a streamed provider response.
//
//	Type "text"      → Text holds a content delta
//	Type "tool_call" → ToolCall holds a complete tool invocation
//	Type "error"     → Err holds the message
//	Type "done"      → Stop holds the stop reason; terminal event
type Event struct {
	Type     string
	Text     string
	ToolCall *ToolCall
	Err      string
	Stop     string
}

// Provider is the interface for any AI model backend.
type Provider interface {
	// Complete streams plain-text completion chunks (no tools). Used for
	// summarisation and other text-only tasks.
	Complete(ctx context.Context, messages []Message) (<-chan string, error)
	// Stream runs a tool-calling completion, emitting text deltas, tool calls,
	// and a terminal "done"/"error" event.
	Stream(ctx context.Context, req Request) (<-chan Event, error)
	// Available reports whether the provider is configured and reachable.
	Available() bool
	// Name is a short human-readable identifier (e.g. "anthropic", "ollama").
	Name() string
}

// CompleteText drives a provider's Stream with no tools and concatenates the
// streamed text. It is the shared implementation behind text-only Complete.
func CompleteText(ctx context.Context, p Provider, system string, messages []Message) (string, error) {
	ch, err := p.Stream(ctx, Request{System: system, Messages: messages})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ev := range ch {
		switch ev.Type {
		case "text":
			sb.WriteString(ev.Text)
		case "error":
			return sb.String(), &ProviderError{Message: ev.Err}
		}
	}
	return sb.String(), nil
}

// ProviderError wraps an error surfaced through a provider event stream.
type ProviderError struct{ Message string }

func (e *ProviderError) Error() string { return e.Message }

// streamToTextCh adapts a provider's Stream into the legacy text-only channel
// returned by Complete. The leading system message (if any) is passed
// out-of-band. Each provider's Complete delegates here.
func streamToTextCh(ctx context.Context, p Provider, messages []Message) (<-chan string, error) {
	sys, rest := splitSystem(messages)
	evCh, err := p.Stream(ctx, Request{System: sys, Messages: rest})
	if err != nil {
		return nil, err
	}
	out := make(chan string)
	go func() {
		defer close(out)
		for ev := range evCh {
			if ev.Type == "text" && ev.Text != "" {
				select {
				case out <- ev.Text:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// splitSystem extracts a leading system message from msgs (if present) and
// returns the system text plus the remaining messages. Several providers take
// the system prompt out-of-band rather than as a message.
func splitSystem(msgs []Message) (system string, rest []Message) {
	if len(msgs) > 0 && msgs[0].Role == "system" {
		return msgs[0].Content, msgs[1:]
	}
	return "", msgs
}
