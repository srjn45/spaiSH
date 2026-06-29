package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"spaish/internal/tools"
)

// ToolName builds the namespaced tool name for an MCP tool, mirroring how Claude
// Code names them: mcp__<server>__<tool>. The namespace avoids collisions
// between servers and with built-in tools.
func ToolName(server, tool string) string {
	return fmt.Sprintf("mcp__%s__%s", server, tool)
}

// bridgedTool adapts a single discovered MCP tool to the tools.Tool interface,
// proxying Run to tools/call on the owning server.
type bridgedTool struct {
	client   *Client
	fullName string // mcp__server__tool, advertised to the model
	toolName string // server-local name passed to tools/call
	desc     string
	schema   map[string]any
}

func (t *bridgedTool) Name() string        { return t.fullName }
func (t *bridgedTool) Description() string { return t.desc }

func (t *bridgedTool) Schema() map[string]any {
	if t.schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return t.schema
}

func (t *bridgedTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	return t.client.CallTool(ctx, t.toolName, input)
}

// toolsFor turns a server's advertised tools into bridged tools.Tool values.
func toolsFor(c *Client, infos []ToolInfo) []tools.Tool {
	out := make([]tools.Tool, 0, len(infos))
	for _, info := range infos {
		desc := info.Description
		if desc == "" {
			desc = "MCP tool " + info.Name + " (server: " + c.Name() + ")"
		}
		out = append(out, &bridgedTool{
			client:   c,
			fullName: ToolName(c.Name(), info.Name),
			toolName: info.Name,
			desc:     desc,
			schema:   info.InputSchema,
		})
	}
	return out
}
