package socket_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"spaios/internal/protocol"
	"spaios/internal/socket"
)

func TestFuseDispatch(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	fuseHandlerCalled := make(chan *protocol.FuseRequest, 1)

	go func() {
		socket.Serve(
			sockPath,
			func(*protocol.Request, *json.Encoder) {},
			func(*protocol.Request, *json.Encoder) {},
			func(*protocol.Request, *json.Encoder) {},
			func(*protocol.Request, *json.Encoder, *json.Decoder) {},
			func(*protocol.Request, *json.Encoder) {},
			func(req *protocol.Request, enc *json.Encoder) {
				fuseHandlerCalled <- req.Fuse
				enc.Encode(protocol.Response{Type: "text", Content: "ai response"})
				enc.Encode(protocol.Response{Type: "done"})
			},
		)
	}()

	// Wait for socket to appear
	for i := 0; i < 30; i++ {
		time.Sleep(50 * time.Millisecond)
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
	}

	client := socket.NewClient(sockPath)
	req := &protocol.Request{
		Type: "fuse",
		Fuse: &protocol.FuseRequest{
			Op:       "ask",
			FileName: "what is using port 8080",
		},
	}

	var collected string
	if err := client.Send(req, func(resp protocol.Response) error {
		if resp.Type == "text" {
			collected += resp.Content
		}
		return nil
	}); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	select {
	case fuseReq := <-fuseHandlerCalled:
		if fuseReq == nil {
			t.Fatal("FuseRequest was nil")
		}
		if fuseReq.Op != "ask" {
			t.Errorf("op: got %q want %q", fuseReq.Op, "ask")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onFuse handler was not called within 2s")
	}

	if collected != "ai response" {
		t.Errorf("collected: got %q want %q", collected, "ai response")
	}
}
