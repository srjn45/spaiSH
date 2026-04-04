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

// LocalProvider calls a locally running Ollama instance.
type LocalProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewLocalProvider(endpoint, model string) *LocalProvider {
	return &LocalProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{},
	}
}

// Available checks whether Ollama is running and reachable.
func (p *LocalProvider) Available() bool {
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

func (p *LocalProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(body))
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
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var event struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			if event.Message.Content != "" {
				select {
				case ch <- event.Message.Content:
				case <-ctx.Done():
					return
				}
			}
			if event.Done {
				return
			}
		}
	}()

	return ch, nil
}
