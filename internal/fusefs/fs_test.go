package fusefs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spaios/internal/fusefs"
	"spaios/internal/protocol"
	"spaios/internal/socket"
)

func TestFuseMount(t *testing.T) {
	if _, err := os.Stat("/dev/fuse"); err != nil {
		t.Skip("FUSE not available on this system (/dev/fuse missing)")
	}

	dir := t.TempDir()
	sockFile := filepath.Join(dir, "spaid.sock")
	mountDir := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mountDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Start a fake spaid that responds to fuse requests.
	go socket.Serve(
		sockFile,
		func(*protocol.Request, *json.Encoder) {},
		func(*protocol.Request, *json.Encoder) {},
		func(*protocol.Request, *json.Encoder) {},
		func(*protocol.Request, *json.Encoder, *json.Decoder) {},
		func(*protocol.Request, *json.Encoder) {},
		func(req *protocol.Request, enc *json.Encoder) {
			reply := "explained: " + req.Fuse.FileName
			enc.Encode(protocol.Response{Type: "text", Content: reply})
			enc.Encode(protocol.Response{Type: "done"})
		},
	)

	// Wait for socket.
	for i := 0; i < 30; i++ {
		time.Sleep(50 * time.Millisecond)
		if _, err := os.Stat(sockFile); err == nil {
			break
		}
	}

	h := &fusefs.Handler{SockPath: sockFile, DefaultTimeout: 5 * time.Second}
	srv, err := fusefs.Mount(mountDir, h)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { srv.Unmount() })

	// Create a real file to read via FUSE.
	testConf := filepath.Join(dir, "test.conf")
	os.WriteFile(testConf, []byte("server { listen 80; }"), 0644)

	// Read /mnt/explain/<absolute-path-to-testConf>
	virtPath := filepath.Join(mountDir, "explain", strings.TrimPrefix(testConf, "/"))
	content, err := os.ReadFile(virtPath)
	if err != nil {
		t.Fatalf("read virtual file: %v", err)
	}
	if !strings.Contains(string(content), "explained:") {
		t.Errorf("unexpected content: %q", string(content))
	}
}

func TestFuseMountReaddir(t *testing.T) {
	if _, err := os.Stat("/dev/fuse"); err != nil {
		t.Skip("FUSE not available")
	}

	dir := t.TempDir()
	sockFile := filepath.Join(dir, "spaid.sock")
	mountDir := filepath.Join(dir, "mnt")
	os.Mkdir(mountDir, 0755)

	go socket.Serve(
		sockFile,
		func(*protocol.Request, *json.Encoder) {},
		func(*protocol.Request, *json.Encoder) {},
		func(*protocol.Request, *json.Encoder) {},
		func(*protocol.Request, *json.Encoder, *json.Decoder) {},
		func(*protocol.Request, *json.Encoder) {},
		func(req *protocol.Request, enc *json.Encoder) {
			enc.Encode(protocol.Response{Type: "done"})
		},
	)

	for i := 0; i < 30; i++ {
		time.Sleep(50 * time.Millisecond)
		if _, err := os.Stat(sockFile); err == nil {
			break
		}
	}

	h := &fusefs.Handler{SockPath: sockFile, DefaultTimeout: 5 * time.Second}
	srv, err := fusefs.Mount(mountDir, h)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { srv.Unmount() })

	entries, err := os.ReadDir(mountDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, op := range []string{"explain", "summarise", "fix", "security", "ask"} {
		if !names[op] {
			t.Errorf("expected %q in root readdir, got: %v", op, names)
		}
	}
}
