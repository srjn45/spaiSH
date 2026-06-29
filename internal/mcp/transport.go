package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Dial spawns the configured server as a subprocess, wires JSON-RPC over its
// stdin/stdout, and returns a connected (but not yet initialized) Client. The
// subprocess inherits the parent environment plus any cfg.Env entries, and its
// stderr is forwarded to the parent's stderr for diagnostics. The returned
// Client's Close kills the subprocess.
func Dial(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp %q: no command configured", cfg.Name)
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), cfg.Env...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdin pipe: %w", cfg.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdout pipe: %w", cfg.Name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %q: start %s: %w", cfg.Name, cfg.Command, err)
	}

	closeFn := func() error {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		return nil
	}

	c := newConn(cfg.Name, stdout, stdin, closeFn)

	// If the caller's context is cancelled (app shutdown), tear the server down.
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-c.done:
		}
	}()

	return c, nil
}
