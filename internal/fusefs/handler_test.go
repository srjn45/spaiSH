package fusefs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spaios/internal/fusefs"
)

// ── ParsePath ──────────────────────────────────────────────────────────────

func TestParsePath(t *testing.T) {
	tests := []struct {
		path    string
		wantOp  string
		wantTgt string
		wantAsk bool
		wantErr bool
	}{
		{
			path:    "/explain/etc/nginx/nginx.conf",
			wantOp:  "explain",
			wantTgt: "/etc/nginx/nginx.conf",
			wantAsk: false,
		},
		{
			path:    "/summarise/var/log/syslog",
			wantOp:  "summarise",
			wantTgt: "/var/log/syslog",
			wantAsk: false,
		},
		{
			path:    "/fix/etc/fstab",
			wantOp:  "fix",
			wantTgt: "/etc/fstab",
			wantAsk: false,
		},
		{
			path:    "/security/etc/ssh/sshd_config",
			wantOp:  "security",
			wantTgt: "/etc/ssh/sshd_config",
			wantAsk: false,
		},
		{
			path:    "/ask/what is using port 8080",
			wantOp:  "ask",
			wantTgt: "what is using port 8080",
			wantAsk: true,
		},
		{
			path:    "/badop/foo",
			wantErr: true,
		},
		{
			path:    "/explain",
			wantErr: true,
		},
		{
			path:    "/ask/",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			pp, err := fusefs.ParsePath(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %+v", pp)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pp.Op != tc.wantOp {
				t.Errorf("op: got %q want %q", pp.Op, tc.wantOp)
			}
			if pp.Target != tc.wantTgt {
				t.Errorf("target: got %q want %q", pp.Target, tc.wantTgt)
			}
			if pp.IsAsk != tc.wantAsk {
				t.Errorf("isAsk: got %v want %v", pp.IsAsk, tc.wantAsk)
			}
		})
	}
}

// ── ReadFile ───────────────────────────────────────────────────────────────

func TestReadFileNormal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.conf")
	os.WriteFile(path, []byte("server { listen 80; }"), 0644)

	content, err := fusefs.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "server { listen 80; }" {
		t.Errorf("got %q", content)
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, err := fusefs.ReadFile("/nonexistent/totally/missing/file.conf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("expected 'file not found' in error, got: %v", err)
	}
}

func TestReadFileTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.conf")
	// Write 512KB + 100 bytes
	big := make([]byte, 512*1024+100)
	for i := range big {
		big[i] = 'x'
	}
	os.WriteFile(path, big, 0644)

	content, err := fusefs.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(content, "[truncated at 512KB]") {
		t.Errorf("expected truncation suffix, got last 30 bytes: %q", content[len(content)-30:])
	}
	if len(content) != 512*1024+len("[truncated at 512KB]") {
		t.Errorf("unexpected length: %d", len(content))
	}
}

// ── ResolveTimeout ─────────────────────────────────────────────────────────

func TestResolveTimeoutFromEnv(t *testing.T) {
	got := fusefs.ResolveTimeout(map[string]string{"SPAI_TIMEOUT": "120"}, 60)
	if got != 120*time.Second {
		t.Errorf("got %v want 120s", got)
	}
}

func TestResolveTimeoutFromConfig(t *testing.T) {
	got := fusefs.ResolveTimeout(map[string]string{}, 90)
	if got != 90*time.Second {
		t.Errorf("got %v want 90s", got)
	}
}

func TestResolveTimeoutHardcodedDefault(t *testing.T) {
	got := fusefs.ResolveTimeout(map[string]string{}, 0)
	if got != 60*time.Second {
		t.Errorf("got %v want 60s", got)
	}
}

func TestResolveTimeoutEnvZeroMeansNoTimeout(t *testing.T) {
	got := fusefs.ResolveTimeout(map[string]string{"SPAI_TIMEOUT": "0"}, 60)
	if got != 0 {
		t.Errorf("got %v want 0 (no timeout)", got)
	}
}

func TestResolveTimeoutEnvPriorityOverConfig(t *testing.T) {
	got := fusefs.ResolveTimeout(map[string]string{"SPAI_TIMEOUT": "30"}, 90)
	if got != 30*time.Second {
		t.Errorf("got %v want 30s", got)
	}
}
