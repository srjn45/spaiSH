package mcp

import (
	"context"
	"time"

	"spaish/internal/tools"
)

// handshakeTimeout bounds how long a single server may take to initialize and
// list its tools before it is skipped.
const handshakeTimeout = 30 * time.Second

// Manager owns the lifecycle of the connected MCP servers for a session.
type Manager struct {
	clients []*Client
}

// Logf is a minimal logging hook so the manager can report skipped servers
// without depending on a concrete logger.
type Logf func(format string, args ...any)

// Load spawns each configured server, performs the handshake, and discovers its
// tools. It is resilient by design: a server that fails to start, handshake, or
// list tools is logged via logf and skipped, never aborting the others. It
// returns a Manager owning the live clients and the flattened list of bridged
// tools. The clients live until Manager.Close (or ctx cancellation).
func Load(ctx context.Context, servers []ServerConfig, logf Logf) (*Manager, []tools.Tool) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	m := &Manager{}
	var bridged []tools.Tool

	for _, cfg := range servers {
		if cfg.Name == "" || cfg.Command == "" {
			logf("mcp: skipping server with empty name or command")
			continue
		}
		c, err := Dial(ctx, cfg)
		if err != nil {
			logf("mcp: server %q failed to start: %v", cfg.Name, err)
			continue
		}

		hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
		if err := c.Initialize(hctx); err != nil {
			cancel()
			logf("mcp: server %q handshake failed: %v", cfg.Name, err)
			_ = c.Close()
			continue
		}
		infos, err := c.ListTools(hctx)
		cancel()
		if err != nil {
			logf("mcp: server %q tools/list failed: %v", cfg.Name, err)
			_ = c.Close()
			continue
		}

		m.clients = append(m.clients, c)
		bridged = append(bridged, toolsFor(c, infos)...)
		logf("mcp: server %q ready (%d tools)", cfg.Name, len(infos))
	}

	return m, bridged
}

// Close shuts down every connected server. It is safe to call on a nil Manager.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	for _, c := range m.clients {
		_ = c.Close()
	}
	m.clients = nil
	return nil
}
