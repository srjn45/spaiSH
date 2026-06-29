// Package mcp implements a minimal Model Context Protocol (MCP) client over the
// stdio transport: it speaks newline-delimited JSON-RPC 2.0 to a spawned
// subprocess, performs the initialize handshake, lists the server's tools, and
// proxies tools/call invocations. Discovered tools are bridged into the agent's
// tool registry (see bridge.go) so the model can call them like any built-in.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// protocolVersion is the MCP revision this client advertises during initialize.
const protocolVersion = "2024-11-05"

// ServerConfig describes a single MCP server to spawn over stdio.
type ServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     []string // entries of the form "KEY=VALUE"
}

// rpcRequest is an outbound JSON-RPC 2.0 request or notification. A request
// carries a non-nil ID; a notification omits it.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is an inbound JSON-RPC 2.0 message. Responses carry an ID and a
// result or error; notifications from the server carry a method and no ID.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Client is a JSON-RPC 2.0 client over a stdio transport. It is safe for
// concurrent use: requests are correlated by ID and a single reader goroutine
// fans responses back to waiting callers.
type Client struct {
	name    string
	w       io.Writer
	closeFn func() error

	writeMu sync.Mutex // serialises writes to w

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResponse
	closed  bool
	readErr error

	done chan struct{}
}

// newConn builds a Client over an already-connected transport and starts its
// read loop. r is the server's stdout, w the server's stdin, and closeFn tears
// the transport down (e.g. kills the subprocess). It does not perform the
// handshake; call Initialize for that.
func newConn(name string, r io.Reader, w io.Writer, closeFn func() error) *Client {
	c := &Client{
		name:    name,
		w:       w,
		closeFn: closeFn,
		pending: make(map[int64]chan rpcResponse),
		done:    make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// readLoop reads newline-delimited JSON messages and dispatches responses to
// the waiting caller registered under the message ID. It runs until the reader
// hits EOF or an error, at which point all pending callers are unblocked.
func (c *Client) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed lines rather than tearing down the conn
		}
		if resp.ID == nil {
			continue // server notification: nothing waits on it
		}
		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
	c.fail(scanner.Err())
}

// fail marks the client closed and unblocks every pending caller.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if err == nil {
		err = io.EOF
	}
	c.readErr = err
	pending := c.pending
	c.pending = make(map[int64]chan rpcResponse)
	c.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
	close(c.done)
}

// notify sends a JSON-RPC notification (no ID, no response expected).
func (c *Client) notify(method string, params any) error {
	return c.writeMessage(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

// call sends a JSON-RPC request and waits for the matching response or ctx.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		err := c.readErr
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp %q: connection closed: %w", c.name, err)
	}
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.writeMessage(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp %q: write %s: %w", c.name, method, err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("mcp %q: connection closed waiting for %s: %w", c.name, method, c.readErr)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp %q: %s: %w", c.name, method, resp.Error)
		}
		return resp.Result, nil
	}
}

// writeMessage marshals msg to a single line and writes it to the transport.
// MCP stdio framing is newline-delimited JSON; messages must not contain
// embedded newlines, which json.Marshal guarantees.
func (c *Client) writeMessage(msg rpcRequest) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.w.Write(data)
	return err
}

// Close tears down the transport and unblocks any in-flight calls.
func (c *Client) Close() error {
	var err error
	if c.closeFn != nil {
		err = c.closeFn()
	}
	c.fail(io.ErrClosedPipe)
	return err
}

// Name returns the configured server name.
func (c *Client) Name() string { return c.name }
