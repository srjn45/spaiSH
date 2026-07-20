package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OpenAIProvider calls any OpenAI-compatible /chat/completions endpoint with
// streaming and tool-calling. It serves both hosted OpenAI-style APIs (with an
// API key) and local keyless servers such as llama.cpp / bitnet.cpp.
type OpenAIProvider struct {
	endpoint string // base URL; "/chat/completions" is appended
	apiKey   string
	model    string
	client   *http.Client
}

// NewOpenAIProvider creates an OpenAI-compatible provider. apiKey may be empty
// for local keyless servers; endpoint should include any version prefix (e.g.
// ".../v1"). retry configures the shared backoff policy applied to every
// request; a zero RetryConfig resolves to the package defaults.
func NewOpenAIProvider(endpoint, apiKey, model string, retry RetryConfig) *OpenAIProvider {
	return &OpenAIProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		client:   NewRetryClient(nil, retry),
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Available() bool {
	if p.endpoint == "" {
		return false
	}
	if p.apiKey != "" {
		return true
	}
	// Keyless local server: probe reachability.
	resp, err := p.client.Get(p.endpoint + "/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func (p *OpenAIProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	return streamToTextCh(ctx, p, messages)
}

// openAIMsg is the wire shape of a chat message. Content is either a plain
// string (text-only, the common case) or a []openAIContentPart array when the
// message carries images — both are valid JSON for the OpenAI content field.
type openAIMsg struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// openAIContentPart is one item of a multi-part message content array, used to
// mix text and images. Type is "text" or "image_url".
type openAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"` // base64 data URI: data:image/png;base64,...
}

// userContent returns the OpenAI content value for a user message: a plain
// string when there are no images, or a text+image parts array otherwise.
func userContent(text string, images []ImageContent) any {
	if len(images) == 0 {
		return text
	}
	var parts []openAIContentPart
	if text != "" {
		parts = append(parts, openAIContentPart{Type: "text", Text: text})
	}
	for _, img := range images {
		parts = append(parts, openAIContentPart{Type: "image_url", ImageURL: &openAIImageURL{URL: img.DataURI()}})
	}
	return parts
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Index    int    `json:"index,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (p *OpenAIProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}

	type toolDef struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description,omitempty"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	}
	type reqBody struct {
		Model    string      `json:"model"`
		Messages []openAIMsg `json:"messages"`
		Tools    []toolDef   `json:"tools,omitempty"`
		Stream   bool        `json:"stream"`
	}

	body := reqBody{Model: model, Messages: toOpenAIMessages(req.System, req.Messages), Stream: true}
	for _, t := range req.Tools {
		var td toolDef
		td.Type = "function"
		td.Function.Name = t.Name
		td.Function.Description = t.Description
		td.Function.Parameters = t.Schema
		body.Tools = append(body.Tools, td)
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan Event)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		// Tool calls arrive incrementally, keyed by index; accumulate them.
		pending := map[int]*ToolCall{}
		var order []int
		argBuf := map[int]*strings.Builder{}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		stop := "stop"
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var event struct {
				Choices []struct {
					Delta struct {
						Content   string           `json:"content"`
						ToolCalls []openAIToolCall `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			for _, c := range event.Choices {
				if c.Delta.Content != "" {
					select {
					case ch <- Event{Type: "text", Text: c.Delta.Content}:
					case <-ctx.Done():
						return
					}
				}
				for _, tc := range c.Delta.ToolCalls {
					cur, ok := pending[tc.Index]
					if !ok {
						cur = &ToolCall{}
						pending[tc.Index] = cur
						argBuf[tc.Index] = &strings.Builder{}
						order = append(order, tc.Index)
					}
					if tc.ID != "" {
						cur.ID = tc.ID
					}
					if tc.Function.Name != "" {
						cur.Name = tc.Function.Name
					}
					argBuf[tc.Index].WriteString(tc.Function.Arguments)
				}
				if c.FinishReason != "" {
					stop = c.FinishReason
				}
			}
		}

		for _, idx := range order {
			tc := pending[idx]
			tc.Input = json.RawMessage(argBuf[idx].String())
			ch <- Event{Type: "tool_call", ToolCall: tc}
		}
		ch <- Event{Type: "done", Stop: stop}
	}()

	return ch, nil
}

// toOpenAIMessages converts neutral messages into OpenAI chat messages.
func toOpenAIMessages(system string, messages []Message) []openAIMsg {
	var out []openAIMsg
	if system != "" {
		out = append(out, openAIMsg{Role: "system", Content: system})
	}
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			am := openAIMsg{Role: "assistant", Content: m.Content}
			for _, tc := range m.ToolCalls {
				oc := openAIToolCall{ID: tc.ID, Type: "function"}
				oc.Function.Name = tc.Name
				oc.Function.Arguments = string(tc.Input)
				am.ToolCalls = append(am.ToolCalls, oc)
			}
			out = append(out, am)
		default:
			if len(m.ToolResults) > 0 {
				// OpenAI tool messages must be plain strings, so any images a tool
				// produced can't ride along on the tool message; collect them and
				// forward them as a following user message the model can see.
				var toolImages []ImageContent
				for _, tr := range m.ToolResults {
					out = append(out, openAIMsg{Role: "tool", ToolCallID: tr.ToolUseID, Content: tr.Content})
					toolImages = append(toolImages, tr.Images...)
				}
				if len(toolImages) > 0 {
					out = append(out, openAIMsg{Role: "user", Content: userContent("", toolImages)})
				}
			}
			if m.Content != "" || len(m.Images) > 0 || len(m.ToolResults) == 0 {
				out = append(out, openAIMsg{Role: "user", Content: userContent(m.Content, m.Images)})
			}
		}
	}
	return out
}
