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

// OpenAICompatProvider calls any server exposing the OpenAI-compatible chat completions API.
// Used for BitNet (bitnet.cpp / llama-server) and other llama.cpp-based runtimes.
type OpenAICompatProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewOpenAICompatProvider(endpoint, model string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{},
	}
}

// Available checks whether the server is running and reachable.
func (p *OpenAICompatProvider) Available() bool {
	if p.endpoint == "" || p.model == "" {
		return false
	}
	resp, err := p.client.Get(p.endpoint + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Complete sends messages to the server and streams response text chunks via SSE.
func (p *OpenAICompatProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type reqBody struct {
		Model    string `json:"model"`
		Messages []msg  `json:"messages"`
		Stream   bool   `json:"stream"`
	}

	reqMsgs := make([]msg, len(messages))
	for i, m := range messages {
		reqMsgs[i] = msg{Role: m.Role, Content: m.Content}
	}

	body, err := json.Marshal(reqBody{Model: p.model, Messages: reqMsgs, Stream: true})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("local server returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			for _, choice := range event.Choices {
				if choice.Delta.Content != "" {
					select {
					case ch <- choice.Delta.Content:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}
