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
// model may be empty, in which case DefaultAnthropicModel is used. retry
// configures the shared backoff policy; a zero RetryConfig resolves to the
// package defaults.
func NewAnthropicProvider(apiKey, model string, retry RetryConfig) *AnthropicProvider {
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: anthropic.NewClient(anthropicOptions(apiKey, retry)...),
	}
}

// NewAnthropicProviderWithBase creates a provider that sends requests to baseURL
// instead of the default Anthropic endpoint. Useful for directing traffic to a
// test HTTP server. retry configures the shared backoff policy; a zero
// RetryConfig resolves to the package defaults.
func NewAnthropicProviderWithBase(apiKey, model, baseURL string, retry RetryConfig) *AnthropicProvider {
	if model == "" {
		model = DefaultAnthropicModel
	}
	opts := append(anthropicOptions(apiKey, retry), option.WithBaseURL(baseURL))
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: anthropic.NewClient(opts...),
	}
}

// anthropicOptions returns the base SDK options shared by both constructors. It
// routes the SDK through the shared retry transport and disables the SDK's own
// retry (WithMaxRetries(0)) so backoff is applied exactly once, giving one
// uniform, config-driven policy across all providers.
func anthropicOptions(apiKey string, retry RetryConfig) []option.RequestOption {
	return []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(NewRetryClient(nil, retry)),
		option.WithMaxRetries(0),
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

	// Cache the system prompt — it rarely changes within a session.
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}

	// Cache the tool list via a breakpoint on the last tool definition.
	if len(req.Tools) > 0 {
		tools := toAnthropicTools(req.Tools)
		if tools[len(tools)-1].OfTool != nil {
			tools[len(tools)-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		params.Tools = tools
	}

	// Cache the conversation history by marking the penultimate message's last
	// content block. Anthropic caches everything up to and including a
	// breakpoint, so this lets each new turn reuse all prior context.
	if len(params.Messages) >= 2 {
		m := &params.Messages[len(params.Messages)-2]
		if len(m.Content) > 0 {
			addCacheControlToLastBlock(&m.Content[len(m.Content)-1])
		}
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
		ch <- Event{
			Type: "done",
			Stop: string(acc.StopReason),
			Usage: &Usage{
				InputTokens:         int(acc.Usage.InputTokens),
				OutputTokens:        int(acc.Usage.OutputTokens),
				CacheCreationTokens: int(acc.Usage.CacheCreationInputTokens),
				CacheReadTokens:     int(acc.Usage.CacheReadInputTokens),
			},
		}
	}()

	return ch, nil
}

// addCacheControlToLastBlock sets an ephemeral cache breakpoint on a content
// block union. Only the variants produced by toAnthropicMessages are handled;
// the others cannot appear in practice.
func addCacheControlToLastBlock(b *anthropic.ContentBlockParamUnion) {
	cc := anthropic.NewCacheControlEphemeralParam()
	switch {
	case b.OfText != nil:
		b.OfText.CacheControl = cc
	case b.OfToolResult != nil:
		b.OfToolResult.CacheControl = cc
	case b.OfToolUse != nil:
		b.OfToolUse.CacheControl = cc
	case b.OfImage != nil:
		b.OfImage.CacheControl = cc
	}
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
				// Text-only results keep the exact legacy shape; only results that
				// carry images build the block by hand (to embed image content).
				if len(tr.Images) == 0 {
					blocks = append(blocks, anthropic.NewToolResultBlock(tr.ToolUseID, tr.Content, tr.IsError))
					continue
				}
				blocks = append(blocks, toolResultBlockWithImages(tr))
			}
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			// Images attached directly to a user turn (vision input).
			for _, img := range m.Images {
				blocks = append(blocks, anthropic.NewImageBlockBase64(img.MediaType, img.Data))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewUserMessage(blocks...))
			}
		}
	}
	return out
}

// toolResultBlockWithImages builds a tool_result content block whose content is
// the result text (when present) followed by any images the tool produced.
// Anthropic tool_result blocks accept image content blocks natively.
func toolResultBlockWithImages(tr ToolResult) anthropic.ContentBlockParamUnion {
	block := anthropic.ToolResultBlockParam{
		ToolUseID: tr.ToolUseID,
		IsError:   anthropic.Bool(tr.IsError),
	}
	if tr.Content != "" {
		block.Content = append(block.Content, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: tr.Content},
		})
	}
	for _, img := range tr.Images {
		ib := anthropic.NewImageBlockBase64(img.MediaType, img.Data)
		block.Content = append(block.Content, anthropic.ToolResultBlockParamContentUnion{OfImage: ib.OfImage})
	}
	return anthropic.ContentBlockParamUnion{OfToolResult: &block}
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
