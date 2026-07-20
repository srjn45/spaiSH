package sandbox

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNewDisabledIsNoop verifies that a zero-value (disabled) config yields a
// sandbox that reports Enabled() == false and leaves commands untouched — on
// every platform, so the disabled path is byte-for-byte the pre-sandbox behavior.
func TestNewDisabledIsNoop(t *testing.T) {
	sb, err := New(Config{})
	if err != nil {
		t.Fatalf("New(disabled) error: %v", err)
	}
	if sb.Enabled() {
		t.Errorf("disabled sandbox reports Enabled() == true")
	}
	cmd := exec.Command("echo", "hi")
	origPath, origArgs := cmd.Path, append([]string{}, cmd.Args...)
	if err := sb.Wrap(cmd); err != nil {
		t.Fatalf("Noop Wrap error: %v", err)
	}
	if cmd.Path != origPath {
		t.Errorf("Wrap changed cmd.Path: %q -> %q", origPath, cmd.Path)
	}
	if strings.Join(cmd.Args, " ") != strings.Join(origArgs, " ") {
		t.Errorf("Wrap changed cmd.Args: %v -> %v", origArgs, cmd.Args)
	}
}

// TestNewBackendOffIsNoop verifies backend "off" disables enforcement even when
// enabled is true.
func TestNewBackendOffIsNoop(t *testing.T) {
	sb, err := New(Config{Enabled: true, Backend: BackendOff})
	if err != nil {
		t.Fatalf("New(off) error: %v", err)
	}
	if sb.Enabled() {
		t.Errorf("backend=off sandbox reports Enabled() == true")
	}
}

func TestBuildBwrapArgv(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		writable    []string
		wantHas     []string
		wantNet     bool // expect --unshare-net present
		wantTrailer []string
	}{
		{
			name:        "deny network binds writable paths",
			cfg:         Config{},
			writable:    []string{"/work", "/tmp/x"},
			wantHas:     []string{"--ro-bind", "--dev", "--proc", "--die-with-parent"},
			wantNet:     true,
			wantTrailer: []string{"--", "/bin/sh", "-c", "echo hi"},
		},
		{
			name:        "allow network omits unshare-net",
			cfg:         Config{AllowNetwork: true},
			writable:    []string{"/work"},
			wantNet:     false,
			wantTrailer: []string{"--", "/bin/sh", "-c", "echo hi"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := buildBwrapArgv(tt.cfg, tt.writable, "/bin/sh", []string{"-c", "echo hi"})
			if argv[0] != BackendBwrap {
				t.Errorf("argv[0] = %q, want %q", argv[0], BackendBwrap)
			}
			joined := strings.Join(argv, " ")
			for _, want := range tt.wantHas {
				if !contains(argv, want) {
					t.Errorf("argv missing %q: %v", want, argv)
				}
			}
			if got := contains(argv, "--unshare-net"); got != tt.wantNet {
				t.Errorf("--unshare-net present = %v, want %v (%q)", got, tt.wantNet, joined)
			}
			// Each writable path must be bound read-write.
			for _, p := range tt.writable {
				if !hasPair(argv, "--bind", p) {
					t.Errorf("writable path %q not bound in %v", p, argv)
				}
			}
			// The real command must be the trailer after "--".
			if !strings.HasSuffix(joined, strings.Join(tt.wantTrailer, " ")) {
				t.Errorf("argv trailer = %q, want suffix %q", joined, strings.Join(tt.wantTrailer, " "))
			}
		})
	}
}

func TestBuildTrampolineArgs(t *testing.T) {
	got := buildTrampolineArgs(Config{}, []string{"/work", "/tmp/x"})
	want := []string{"--allow-path=/work", "--allow-path=/tmp/x", "--net=off"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("buildTrampolineArgs = %v, want %v", got, want)
	}

	gotNet := buildTrampolineArgs(Config{AllowNetwork: true}, []string{"/work"})
	if gotNet[len(gotNet)-1] != "--net=on" {
		t.Errorf("with AllowNetwork, last arg = %q, want --net=on", gotNet[len(gotNet)-1])
	}
}

// TestWritablePaths verifies AllowPaths and the command's Dir are both included,
// absolute, and deduped, and that cmd.Dir (the code_exec temp dir) is picked up
// automatically.
func TestWritablePaths(t *testing.T) {
	cmd := exec.Command("echo")
	cmd.Dir = "/tmp/spai-code-exec-abc"
	got := writablePaths(Config{AllowPaths: []string{"/opt/data", "/opt/data"}}, cmd)

	if !contains(got, "/opt/data") {
		t.Errorf("AllowPaths not included: %v", got)
	}
	if !contains(got, "/tmp/spai-code-exec-abc") {
		t.Errorf("cmd.Dir (code_exec temp) not included: %v", got)
	}
	// Deduped: /opt/data appears once.
	n := 0
	for _, p := range got {
		if p == "/opt/data" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected /opt/data once, got %d in %v", n, got)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// hasPair reports whether flag is immediately followed by both val and val (a
// bwrap "--bind SRC DEST" pair with SRC==DEST==val).
func hasPair(argv []string, flag, val string) bool {
	for i := 0; i+2 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == val && argv[i+2] == val {
			return true
		}
	}
	return false
}
