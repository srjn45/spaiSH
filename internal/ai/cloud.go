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

// CloudProvider calls any OpenAI-compatible streaming chat completions endpoint.
type CloudProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

func NewCloudProvider(endpoint, apiKey, model string) *CloudProvider {
	return &CloudProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{},
	}
}

func (p *CloudProvider) Available() bool {
	return p.apiKey != "" && p.endpoint != ""
}

func (p *CloudProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
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
			if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" {
				select {
				case ch <- event.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
