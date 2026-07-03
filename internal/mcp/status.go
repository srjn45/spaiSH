package mcp

// ServerStatus is the discovery outcome for a single configured MCP server: its
// name, whether it connected and listed tools successfully, the error message
// when it did not, and the server-local names of the tools it exposed.
type ServerStatus struct {
	Name  string
	OK    bool
	Err   string
	Tools []string
}

// Status returns the discovery outcome recorded during Load for every
// configured server, in configuration order — including servers that failed to
// start, handshake, or list tools (whose failure would otherwise only be
// logged). It is safe to call on a nil Manager and returns nil in that case.
func (m *Manager) Status() []ServerStatus {
	if m == nil {
		return nil
	}
	return m.statuses
}
