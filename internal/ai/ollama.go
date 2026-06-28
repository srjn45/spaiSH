package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// OllamaProvider talks to a local Ollama instance via /api/chat, with streaming
// and tool-calling (for models that support it).
type OllamaProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

// NewLocalProvider creates a provider backed by a local Ollama server.
func NewLocalProvider(endpoint, model string) *OllamaProvider {
	return &OllamaProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{},
	}
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) Available() bool {
	if p.endpoint == "" || p.model == "" {
		return false
	}
	resp, err := p.client.Get(p.endpoint + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (p *OllamaProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	return streamToTextCh(ctx, p, messages)
}

// ollamaMsg is the wire shape of an Ollama chat message.
type ollamaMsg struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func (p *OllamaProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
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
		Messages []ollamaMsg `json:"messages"`
		Tools    []toolDef   `json:"tools,omitempty"`
		Stream   bool        `json:"stream"`
	}

	body := reqBody{Model: model, Messages: toOllamaMessages(req.System, req.Messages), Stream: true}
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan Event)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		var toolCalls []ollamaToolCall
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			var event struct {
				Message ollamaMsg `json:"message"`
				Done    bool      `json:"done"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			if event.Message.Content != "" {
				select {
				case ch <- Event{Type: "text", Text: event.Message.Content}:
				case <-ctx.Done():
					return
				}
			}
			toolCalls = append(toolCalls, event.Message.ToolCalls...)
			if event.Done {
				break
			}
		}

		stop := "stop"
		for i, tc := range toolCalls {
			stop = "tool_use"
			ch <- Event{Type: "tool_call", ToolCall: &ToolCall{
				ID:    "call_" + strconv.Itoa(i),
				Name:  tc.Function.Name,
				Input: tc.Function.Arguments,
			}}
		}
		ch <- Event{Type: "done", Stop: stop}
	}()

	return ch, nil
}

// toOllamaMessages converts neutral messages into Ollama chat messages.
func toOllamaMessages(system string, messages []Message) []ollamaMsg {
	var out []ollamaMsg
	if system != "" {
		out = append(out, ollamaMsg{Role: "system", Content: system})
	}
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			am := ollamaMsg{Role: "assistant", Content: m.Content}
			for _, tc := range m.ToolCalls {
				var oc ollamaToolCall
				oc.Function.Name = tc.Name
				oc.Function.Arguments = json.RawMessage(tc.Input)
				am.ToolCalls = append(am.ToolCalls, oc)
			}
			out = append(out, am)
		default:
			if len(m.ToolResults) > 0 {
				for _, tr := range m.ToolResults {
					out = append(out, ollamaMsg{Role: "tool", Content: tr.Content})
				}
			}
			if m.Content != "" || len(m.ToolResults) == 0 {
				out = append(out, ollamaMsg{Role: "user", Content: m.Content})
			}
		}
	}
	return out
}
