//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestBwrapFilesystemEnforcement exercises the real bwrap backend end-to-end:
// a write inside allow_paths succeeds, one outside is denied. Skipped when
// bwrap is not installed.
func TestBwrapFilesystemEnforcement(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	allow := t.TempDir()
	deny := t.TempDir()
	sb, err := New(Config{Enabled: true, Backend: BackendBwrap, AllowPaths: []string{allow}})
	if err != nil {
		t.Fatalf("New(bwrap): %v", err)
	}

	if code := runShWrite(t, sb, allow, filepath.Join(allow, "ok")); code != 0 {
		t.Errorf("write inside allow_paths should succeed under bwrap, exit=%d", code)
	}
	if code := runShWrite(t, sb, allow, filepath.Join(deny, "nope")); code == 0 {
		t.Errorf("write outside allow_paths should be denied under bwrap, exit=0")
	}
}

// runShWrite wraps `sh -c 'echo x > target'` with sb and returns the exit code.
// dir is the (writable) working directory for the command.
func runShWrite(t *testing.T, sb Sandbox, dir, target string) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "echo x > '"+target+"'")
	cmd.Dir = dir
	if err := sb.Wrap(cmd); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	return exitCode(t, cmd.Run())
}

// TestNativeApplyRestrictions exercises the native Landlock+seccomp primitives
// via a re-exec of the test binary (TestHelperProcess), which applies the
// restrictions on a locked OS thread and performs a filesystem/network probe.
// Skipped when the kernel lacks Landlock or the arch is unsupported.
func TestNativeApplyRestrictions(t *testing.T) {
	if !landlockAvailable() {
		t.Skip("kernel does not support Landlock")
	}
	if _, _, err := seccompArch(); err != nil {
		t.Skipf("native seccomp unsupported: %v", err)
	}
	allow := t.TempDir()
	deny := t.TempDir()

	if code := runHelper(t, allow, "off", "write:"+filepath.Join(allow, "ok")); code != 0 {
		t.Errorf("Landlock: write inside allow should succeed, exit=%d", code)
	}
	if code := runHelper(t, allow, "off", "write:"+filepath.Join(deny, "nope")); code == 0 {
		t.Errorf("Landlock: write outside allow should be denied, exit=0")
	}
	if code := runHelper(t, allow, "off", "socket"); code == 0 {
		t.Errorf("seccomp: AF_INET socket should be denied when net=off, exit=0")
	}
	if code := runHelper(t, allow, "on", "socket"); code != 0 {
		t.Errorf("AF_INET socket should be allowed when net=on, exit=%d", code)
	}
}

func runHelper(t *testing.T, allow, net, op string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_SANDBOX_HELPER="+op,
		"GO_SANDBOX_ALLOW="+allow,
		"GO_SANDBOX_NET="+net,
	)
	out, err := cmd.CombinedOutput()
	code := exitCode(t, err)
	if code == 3 {
		t.Fatalf("helper failed to apply restrictions: %s", strings.TrimSpace(string(out)))
	}
	return code
}

// TestHelperProcess is not a real test: when GO_SANDBOX_HELPER is set it applies
// the sandbox restrictions to a locked OS thread and performs the requested
// probe, exiting 0 on "allowed" and 1 on "denied".
func TestHelperProcess(t *testing.T) {
	op := os.Getenv("GO_SANDBOX_HELPER")
	if op == "" {
		return
	}
	// Lock to the OS thread so the per-thread Landlock/seccomp restrictions we
	// install cover every syscall the probe makes.
	runtime.LockOSThread()

	allow := os.Getenv("GO_SANDBOX_ALLOW")
	denyNetwork := os.Getenv("GO_SANDBOX_NET") != "on"
	if err := ApplyRestrictions([]string{allow}, denyNetwork); err != nil {
		os.Exit(3)
	}

	switch {
	case strings.HasPrefix(op, "write:"):
		if err := os.WriteFile(strings.TrimPrefix(op, "write:"), []byte("x"), 0600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case op == "socket":
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
		if err != nil {
			os.Exit(1)
		}
		unix.Close(fd)
		os.Exit(0)
	}
	os.Exit(9)
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	t.Fatalf("command run error: %v", err)
	return -1
}
