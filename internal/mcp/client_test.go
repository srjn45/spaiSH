package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// fakeServer is an in-process MCP server speaking newline-delimited JSON-RPC
// over pipes, used to test framing, the handshake, and tool calls without a real
// subprocess. handler receives each decoded request and returns the value to
// place in the response's result (or an *rpcError to return an error).
type fakeServer struct {
	clientToServer *io.PipeReader // server reads requests here
	serverToClient *io.PipeWriter // server writes responses here
	handler        func(method string, params json.RawMessage) (any, *rpcError)
	initialized    chan struct{}
}

// newTestClient wires a Client to a fakeServer via two pipes and starts the
// server loop. The returned closeFn shuts both ends down.
func newTestClient(t *testing.T, handler func(method string, params json.RawMessage) (any, *rpcError)) (*Client, *fakeServer) {
	t.Helper()
	// c2sR/c2sW: client writes -> server reads.
	c2sR, c2sW := io.Pipe()
	// s2cR/s2cW: server writes -> client reads.
	s2cR, s2cW := io.Pipe()

	fs := &fakeServer{
		clientToServer: c2sR,
		serverToClient: s2cW,
		handler:        handler,
		initialized:    make(chan struct{}, 1),
	}
	go fs.run()

	c := newConn("test", s2cR, c2sW, func() error {
		_ = c2sW.Close()
		_ = s2cR.Close()
		return nil
	})
	t.Cleanup(func() { _ = c.Close(); _ = c2sR.Close(); _ = s2cW.Close() })
	return c, fs
}

func (fs *fakeServer) run() {
	scanner := bufio.NewScanner(fs.clientToServer)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		params, _ := json.Marshal(req.Params)
		// Notifications carry no ID and expect no response.
		if req.ID == nil {
			if req.Method == "notifications/initialized" {
				select {
				case fs.initialized <- struct{}{}:
				default:
				}
			}
			continue
		}
		result, rerr := fs.handler(req.Method, params)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}
		if rerr == nil {
			raw, _ := json.Marshal(result)
			resp.Result = raw
		}
		out, _ := json.Marshal(resp)
		out = append(out, '\n')
		if _, err := fs.serverToClient.Write(out); err != nil {
			return
		}
	}
}

// defaultHandler implements initialize, tools/list, and tools/call for a server
// exposing a single "echo" tool.
func defaultHandler(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fake", "version": "1.0"},
		}, nil
	case "tools/list":
		return map[string]any{
			"tools": []map[string]any{
				{
					"name":        "echo",
					"description": "echo back the message",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"message": map[string]any{"type": "string"}},
					},
				},
			},
		}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		if p.Name != "echo" {
			return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
		}
		msg, _ := p.Arguments["message"].(string)
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "echo: " + msg}},
		}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

func TestInitializeHandshake(t *testing.T) {
	c, fs := newTestClient(t, defaultHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	select {
	case <-fs.initialized:
	case <-time.After(time.Second):
		t.Fatal("server never received notifications/initialized")
	}
}

func TestListTools(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	infos, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", infos)
	}
	if infos[0].Description != "echo back the message" {
		t.Fatalf("unexpected description: %q", infos[0].Description)
	}
	if infos[0].InputSchema["type"] != "object" {
		t.Fatalf("unexpected schema: %+v", infos[0].InputSchema)
	}
}

func TestCallTool(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{"message":"hi"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCallToolServerError(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.CallTool(ctx, "missing", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// TestToolErrorResult verifies that an isError:true result becomes a Go error
// carrying the textual content.
func TestToolErrorResult(t *testing.T) {
	handler := func(method string, params json.RawMessage) (any, *rpcError) {
		if method == "tools/call" {
			return map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": "boom"}},
			}, nil
		}
		return defaultHandler(method, params)
	}
	c, _ := newTestClient(t, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.CallTool(ctx, "echo", nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

// TestConcurrentCalls checks that responses are correlated by ID under
// concurrent in-flight requests.
func TestConcurrentCalls(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := c.CallTool(ctx, "echo", json.RawMessage(`{"message":"x"}`))
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent CallTool: %v", err)
		}
	}
}

// TestClosedConnection verifies calls fail cleanly after the transport closes.
func TestClosedConnection(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	_ = c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.CallTool(ctx, "echo", nil); err == nil {
		t.Fatal("expected error calling a closed client")
	}
}
