package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolInfo describes a tool advertised by an MCP server via tools/list.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Initialize performs the MCP handshake: an initialize request followed by the
// notifications/initialized notification. It must be called once before any
// other request.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "spai",
			"version": "dev",
		},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("mcp %q: initialized notification: %w", c.name, err)
	}
	return nil
}

// ListTools returns every tool the server exposes, following cursor pagination.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	var all []ToolInfo
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("mcp %q: decode tools/list: %w", c.name, err)
		}
		all = append(all, page.Tools...)
		if page.NextCursor == "" {
			return all, nil
		}
		cursor = page.NextCursor
	}
}

// contentBlock is one element of a tools/call result's content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallTool invokes a tool by its server-local name and returns the textual
// result. A server-reported tool error (isError) is surfaced as a Go error so
// the agent feeds it back to the model as an error tool result.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	} else {
		params["arguments"] = map[string]any{}
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}
	var result struct {
		Content []contentBlock `json:"content"`
		IsError bool           `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp %q: decode tools/call: %w", c.name, err)
	}
	text := joinText(result.Content)
	if result.IsError {
		if text == "" {
			text = "tool reported an error"
		}
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}

// joinText concatenates the text blocks of a content array, summarising any
// non-text blocks so the model still sees that something was returned.
func joinText(blocks []contentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		if b.Type == "text" || b.Text != "" {
			sb.WriteString(b.Text)
		} else {
			sb.WriteString("[" + b.Type + " content]")
		}
	}
	return sb.String()
}
