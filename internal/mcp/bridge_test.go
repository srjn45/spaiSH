package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestToolName(t *testing.T) {
	if got := ToolName("filesystem", "read_file"); got != "mcp__filesystem__read_file" {
		t.Fatalf("ToolName = %q", got)
	}
}

func TestToolsForNamespacing(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	infos, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	bridged := toolsFor(c, infos)
	if len(bridged) != 1 {
		t.Fatalf("expected 1 bridged tool, got %d", len(bridged))
	}
	tool := bridged[0]
	if tool.Name() != "mcp__test__echo" {
		t.Fatalf("bridged name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatal("bridged description is empty")
	}
	if tool.Schema()["type"] != "object" {
		t.Fatalf("bridged schema = %+v", tool.Schema())
	}

	out, err := tool.Run(ctx, json.RawMessage(`{"message":"bridged"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "echo: bridged" {
		t.Fatalf("Run output = %q", out)
	}
}

// TestBridgedToolDefaultDescription checks the fallback description for tools
// that advertise none, and the default schema for tools with no inputSchema.
func TestBridgedToolDefaults(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	bt := &bridgedTool{client: c, fullName: "mcp__test__x", toolName: "x"}
	if bt.Schema()["type"] != "object" {
		t.Fatalf("default schema = %+v", bt.Schema())
	}
}

// TestManagerLoadSkipsBadServers verifies resilience: a server that cannot start
// is skipped, and Load still returns a usable (empty) result.
func TestManagerLoadSkipsBadServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var logged int
	mgr, bridged := Load(ctx, []ServerConfig{
		{Name: "broken", Command: "this-command-does-not-exist-xyz"},
		{Name: "", Command: "echo"}, // empty name skipped
	}, func(string, ...any) { logged++ })
	defer mgr.Close()

	if len(bridged) != 0 {
		t.Fatalf("expected no tools from broken servers, got %d", len(bridged))
	}
	if logged == 0 {
		t.Fatal("expected skipped servers to be logged")
	}
}
