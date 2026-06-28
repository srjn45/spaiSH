package ai

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultAnthropicModel is used when no model is configured.
const DefaultAnthropicModel = "claude-opus-4-8"

// AnthropicProvider talks to the Anthropic Messages API natively via the
// official SDK, with streaming and tool use.
type AnthropicProvider struct {
	apiKey string
	model  string
	client anthropic.Client
}

// NewAnthropicProvider creates a provider for the Anthropic Messages API.
// model may be empty, in which case DefaultAnthropicModel is used.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
	}
}

func (p *AnthropicProvider) Name() string    { return "anthropic" }
func (p *AnthropicProvider) Available() bool { return p.apiKey != "" }

func (p *AnthropicProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	return streamToTextCh(ctx, p, messages)
}

func (p *AnthropicProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 16000,
		Messages:  toAnthropicMessages(req.Messages),
		// Adaptive thinking: the model decides when and how much to reason.
		Thinking: anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		},
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if len(req.Tools) > 0 {
		params.Tools = toAnthropicTools(req.Tools)
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan Event)
	go func() {
		defer close(ch)

		acc := anthropic.Message{}
		for stream.Next() {
			ev := stream.Current()
			if err := acc.Accumulate(ev); err != nil {
				ch <- Event{Type: "error", Err: err.Error()}
				return
			}
			if delta, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if td, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && td.Text != "" {
					select {
					case ch <- Event{Type: "text", Text: td.Text}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: "error", Err: err.Error()}
			return
		}

		// Emit any tool calls accumulated in the final message.
		for _, block := range acc.Content {
			if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
				ch <- Event{Type: "tool_call", ToolCall: &ToolCall{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: json.RawMessage(tu.Input),
				}}
			}
		}
		ch <- Event{Type: "done", Stop: string(acc.StopReason)}
	}()

	return ch, nil
}

// toAnthropicMessages converts neutral messages into SDK MessageParams.
func toAnthropicMessages(messages []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, json.RawMessage(tc.Input), tc.Name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}
		default: // user (and any tool-result turns)
			var blocks []anthropic.ContentBlockParamUnion
			for _, tr := range m.ToolResults {
				blocks = append(blocks, anthropic.NewToolResultBlock(tr.ToolUseID, tr.Content, tr.IsError))
			}
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewUserMessage(blocks...))
			}
		}
	}
	return out
}

// toAnthropicTools converts neutral tool specs into SDK tool params.
func toAnthropicTools(tools []ToolSpec) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := anthropic.ToolInputSchemaParam{}
		if props, ok := t.Schema["properties"]; ok {
			schema.Properties = props
		}
		if req, ok := t.Schema["required"].([]string); ok {
			schema.Required = req
		} else if reqAny, ok := t.Schema["required"].([]any); ok {
			for _, r := range reqAny {
				if s, ok := r.(string); ok {
					schema.Required = append(schema.Required, s)
				}
			}
		}
		tp := anthropic.ToolParam{
			Name:        t.Name,
			InputSchema: schema,
		}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out
}
