package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// TestManagerDiscoverOK verifies that a server which handshakes and lists tools
// is recorded as OK with its tool names, and that its tools are bridged.
func TestManagerDiscoverOK(t *testing.T) {
	c, _ := newTestClient(t, defaultHandler)
	m := &Manager{}

	// The bridged tool name derives from the client's own name ("test"); the
	// discover name argument labels the status entry.
	st, bridged := m.discover(context.Background(), "test", c, nil)
	if !st.OK {
		t.Fatalf("expected OK status, got %+v", st)
	}
	if st.Err != "" {
		t.Fatalf("expected no error, got %q", st.Err)
	}
	if len(st.Tools) != 1 || st.Tools[0] != "echo" {
		t.Fatalf("unexpected tools: %+v", st.Tools)
	}
	if len(bridged) != 1 {
		t.Fatalf("expected 1 bridged tool, got %d", len(bridged))
	}
	if got := bridged[0].Name(); got != "mcp__test__echo" {
		t.Fatalf("unexpected bridged tool name: %q", got)
	}
	if len(m.clients) != 1 {
		t.Fatalf("expected client retained on success, got %d", len(m.clients))
	}
}

// TestManagerDiscoverListFailure verifies that a tools/list failure is recorded
// as OK=false with the error, yields no tools, and drops the client.
func TestManagerDiscoverListFailure(t *testing.T) {
	handler := func(method string, params json.RawMessage) (any, *rpcError) {
		if method == "tools/list" {
			return nil, &rpcError{Code: -32603, Message: "list boom"}
		}
		return defaultHandler(method, params)
	}
	c, _ := newTestClient(t, handler)
	m := &Manager{}

	st, bridged := m.discover(context.Background(), "broken", c, nil)
	if st.OK {
		t.Fatalf("expected failed status, got %+v", st)
	}
	if st.Err == "" {
		t.Fatal("expected an error message on failure")
	}
	if len(bridged) != 0 {
		t.Fatalf("expected no bridged tools, got %d", len(bridged))
	}
	if len(m.clients) != 0 {
		t.Fatalf("expected client dropped on failure, got %d", len(m.clients))
	}
}

// TestLoadRecordsDialFailure verifies that a server whose command cannot start
// is still surfaced in Status() (rather than silently skipped), and that Load
// stays resilient.
func TestLoadRecordsDialFailure(t *testing.T) {
	m, bridged := Load(context.Background(), []ServerConfig{
		{Name: "nope", Command: "/no/such/spai-mcp-binary-xyz"},
	}, nil)

	if len(bridged) != 0 {
		t.Fatalf("expected no tools from a failed server, got %d", len(bridged))
	}
	statuses := m.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d: %+v", len(statuses), statuses)
	}
	if statuses[0].Name != "nope" || statuses[0].OK {
		t.Fatalf("expected failed status for %q, got %+v", "nope", statuses[0])
	}
	if statuses[0].Err == "" {
		t.Fatal("expected an error message for a server that failed to start")
	}
}

// TestLoadRecordsEmptyConfig verifies invalid entries are recorded and that an
// empty server list yields no statuses.
func TestLoadRecordsEmptyConfig(t *testing.T) {
	if m, _ := Load(context.Background(), nil, nil); len(m.Status()) != 0 {
		t.Fatalf("expected no statuses for empty config, got %+v", m.Status())
	}

	m, _ := Load(context.Background(), []ServerConfig{{Name: "x"}}, nil)
	if len(m.Status()) != 1 || m.Status()[0].OK || m.Status()[0].Err == "" {
		t.Fatalf("expected recorded failure for server with empty command, got %+v", m.Status())
	}
}

// TestNilManagerStatus verifies Status is safe on a nil Manager.
func TestNilManagerStatus(t *testing.T) {
	var m *Manager
	if s := m.Status(); s != nil {
		t.Fatalf("expected nil status for nil manager, got %+v", s)
	}
}
