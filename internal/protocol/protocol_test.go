package protocol_test

import (
	"encoding/json"
	"testing"

	"spaios/internal/protocol"
)

func TestRequestSessionFields(t *testing.T) {
	req := protocol.Request{
		Type:      "session",
		SessionID: "12345",
		Stdin:     "hello pipe",
		Session: &protocol.SessionRequest{
			Command: "clear",
			Lines:   10,
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var req2 protocol.Request
	if err := json.Unmarshal(data, &req2); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if req2.SessionID != "12345" {
		t.Errorf("expected SessionID '12345', got %q", req2.SessionID)
	}
	if req2.Stdin != "hello pipe" {
		t.Errorf("expected Stdin 'hello pipe', got %q", req2.Stdin)
	}
	if req2.Session == nil || req2.Session.Command != "clear" || req2.Session.Lines != 10 {
		t.Errorf("unexpected Session: %+v", req2.Session)
	}
}

func TestSessionRequestRebuildContext(t *testing.T) {
	req := protocol.SessionRequest{Command: "rebuild-context"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded protocol.SessionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Command != "rebuild-context" {
		t.Errorf("expected 'rebuild-context', got %q", decoded.Command)
	}
}
