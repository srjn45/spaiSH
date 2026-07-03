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
	clients  []*Client
	statuses []ServerStatus
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
			m.statuses = append(m.statuses, ServerStatus{Name: cfg.Name, Err: "empty name or command"})
			continue
		}
		c, err := Dial(ctx, cfg)
		if err != nil {
			logf("mcp: server %q failed to start: %v", cfg.Name, err)
			m.statuses = append(m.statuses, ServerStatus{Name: cfg.Name, Err: err.Error()})
			continue
		}

		st, toolz := m.discover(ctx, cfg.Name, c, logf)
		m.statuses = append(m.statuses, st)
		bridged = append(bridged, toolz...)
	}

	return m, bridged
}

// discover performs the handshake and tool listing for one already-dialed
// client, returning its resolved status. On success the client is retained in
// the manager and its tools are bridged; on any failure the status carries the
// error, the client is closed, and no tools are returned. It never aborts the
// caller's loop over the remaining servers.
func (m *Manager) discover(ctx context.Context, name string, c *Client, logf Logf) (ServerStatus, []tools.Tool) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	st := ServerStatus{Name: name}
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	if err := c.Initialize(hctx); err != nil {
		logf("mcp: server %q handshake failed: %v", name, err)
		st.Err = err.Error()
		_ = c.Close()
		return st, nil
	}
	infos, err := c.ListTools(hctx)
	if err != nil {
		logf("mcp: server %q tools/list failed: %v", name, err)
		st.Err = err.Error()
		_ = c.Close()
		return st, nil
	}

	m.clients = append(m.clients, c)
	st.OK = true
	st.Tools = make([]string, len(infos))
	for i, info := range infos {
		st.Tools[i] = info.Name
	}
	logf("mcp: server %q ready (%d tools)", name, len(infos))
	return st, toolsFor(c, infos)
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
