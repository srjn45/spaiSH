package socket

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"spaios/internal/protocol"
)

// Client connects to the spaid Unix socket.
type Client struct {
	sockPath string
}

// NewClient creates a client pointing at sockPath.
func NewClient(sockPath string) *Client {
	return &Client{sockPath: sockPath}
}

// Send sends req and calls fn for each Response chunk until "done" or "error".
func (c *Client) Send(req *protocol.Request, fn func(protocol.Response) error) error {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return fmt.Errorf("cannot reach spaid: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}

	dec := json.NewDecoder(conn)
	for {
		var resp protocol.Response
		if err := dec.Decode(&resp); err != nil {
			return err
		}
		if err := fn(resp); err != nil {
			return err
		}
		if resp.Type == "done" || resp.Type == "error" {
			return nil
		}
	}
}

// SendInteractive sends req and calls fn for each Response chunk. fn receives
// the encoder so it can write back to spaid mid-stream (used for the agent
// confirm round-trip). Stops on "done" or "error".
func (c *Client) SendInteractive(req *protocol.Request, fn func(protocol.Response, *json.Encoder) error) error {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return fmt.Errorf("cannot reach spaid: %w", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return err
	}

	dec := json.NewDecoder(conn)
	for {
		var resp protocol.Response
		if err := dec.Decode(&resp); err != nil {
			return err
		}
		if err := fn(resp, enc); err != nil {
			return err
		}
		if resp.Type == "done" || resp.Type == "error" {
			return nil
		}
	}
}

// EnsureRunning starts spaid if it is not already running, then waits up to 3s.
func EnsureRunning(sockPath, daemonBin string) error {
	if _, err := os.Stat(sockPath); err == nil {
		// Socket exists — assume running
		return nil
	}
	cmd := exec.Command(daemonBin)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start spaid: %w", err)
	}
	// Wait up to 3 seconds for the socket to appear
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(sockPath); err == nil {
			return nil
		}
	}
	return fmt.Errorf("spaid did not start within 3 seconds")
}
