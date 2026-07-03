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

// ollamaMsg is the wire shape of an Ollama chat message. Images is a list of
// base64-encoded images (no data: prefix) for vision models; it is omitted for
// text-only messages.
type ollamaMsg struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	Images    []string         `json:"images,omitempty"`
}

// base64Images extracts the raw base64 payloads from images, the shape Ollama's
// per-message "images" field expects.
func base64Images(images []ImageContent) []string {
	if len(images) == 0 {
		return nil
	}
	out := make([]string, 0, len(images))
	for _, img := range images {
		out = append(out, img.Data)
	}
	return out
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

	// Always stream for responsive output. Models that support Ollama's native
	// tool-calling return structured tool_calls in the final streamed message;
	// models that instead print tool calls as text are handled by the inline
	// fallback below.
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
		var content strings.Builder
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
				content.WriteString(event.Message.Content)
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

		// Degraded fallback: some local models emit tool calls as JSON text in
		// the content instead of structured tool_calls. Recover them so the
		// agent loop can still execute the requested actions.
		if len(toolCalls) == 0 && len(req.Tools) > 0 {
			toolCalls = extractInlineToolCalls(content.String())
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

// extractInlineToolCalls scans free-form text for JSON objects shaped like
// {"name": "...", "arguments": {...}} and returns them as tool calls. It is a
// best-effort fallback for local models that print tool calls as text (often
// inside ```json fences) rather than emitting structured tool_calls.
func extractInlineToolCalls(s string) []ollamaToolCall {
	var calls []ollamaToolCall
	for i := 0; i < len(s); {
		if s[i] != '{' {
			i++
			continue
		}
		end := matchBrace(s, i)
		if end < 0 {
			break
		}
		var probe struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(s[i:end+1]), &probe); err == nil && probe.Name != "" && len(probe.Arguments) > 0 {
			var oc ollamaToolCall
			oc.Function.Name = probe.Name
			oc.Function.Arguments = probe.Arguments
			calls = append(calls, oc)
			i = end + 1
		} else {
			i++
		}
	}
	return calls
}

// matchBrace returns the index of the '}' that closes the '{' at start, honouring
// string literals and escapes, or -1 if unbalanced.
func matchBrace(s string, start int) int {
	depth := 0
	inStr := false
	esc := false
	for j := start; j < len(s); j++ {
		c := s[j]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
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
				// Ollama attaches images via a per-message "images" field rather
				// than in tool messages, so forward any tool-produced images as a
				// following user message the vision model can see.
				var toolImages []ImageContent
				for _, tr := range m.ToolResults {
					out = append(out, ollamaMsg{Role: "tool", Content: tr.Content})
					toolImages = append(toolImages, tr.Images...)
				}
				if len(toolImages) > 0 {
					out = append(out, ollamaMsg{Role: "user", Images: base64Images(toolImages)})
				}
			}
			if m.Content != "" || len(m.Images) > 0 || len(m.ToolResults) == 0 {
				out = append(out, ollamaMsg{Role: "user", Content: m.Content, Images: base64Images(m.Images)})
			}
		}
	}
	return out
}
