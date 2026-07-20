package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// fakeSandbox records Wrap calls and can be toggled enabled/erroring. When
// wrapErr is set, Wrap returns it (to exercise the fail-closed path); otherwise
// it rewrites the command to a harmless no-op so the tool's own cmd.Run does not
// execute the model-supplied command during the test.
type fakeSandbox struct {
	enabled bool
	wrapErr error
	calls   int
}

func (f *fakeSandbox) Enabled() bool { return f.enabled }

func (f *fakeSandbox) Wrap(cmd *exec.Cmd) error {
	f.calls++
	if f.wrapErr != nil {
		return f.wrapErr
	}
	// Neutralize the command so the test never runs the original payload.
	cmd.Path = "/bin/true"
	cmd.Args = []string{"/bin/true"}
	return nil
}

// TestBashWrapsWhenEnabled verifies Bash.Run calls Wrap when the sandbox is
// enabled and no Trusted predicate exempts the command.
func TestBashWrapsWhenEnabled(t *testing.T) {
	sb := &fakeSandbox{enabled: true}
	b := Bash{Sandbox: sb}
	if _, err := b.Run(context.Background(), json.RawMessage(`{"command":"echo secret"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sb.calls != 1 {
		t.Errorf("expected Wrap called once, got %d", sb.calls)
	}
}

// TestBashFailsClosedOnWrapError verifies a Wrap error aborts the run (the
// command is refused rather than executed unsandboxed).
func TestBashFailsClosedOnWrapError(t *testing.T) {
	sb := &fakeSandbox{enabled: true, wrapErr: errors.New("no backend")}
	b := Bash{Sandbox: sb}
	out, err := b.Run(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if err == nil {
		t.Fatalf("expected fail-closed error, got output %q", out)
	}
	if !strings.Contains(err.Error(), "refusing to run unsandboxed") {
		t.Errorf("expected fail-closed message, got %v", err)
	}
	if sb.calls != 1 {
		t.Errorf("expected Wrap attempted once, got %d", sb.calls)
	}
}

// TestBashTrustedBypassesSandbox verifies a command the Trusted predicate matches
// is NOT wrapped (the allow_commands carve-out).
func TestBashTrustedBypassesSandbox(t *testing.T) {
	sb := &fakeSandbox{enabled: true, wrapErr: errors.New("should not be called")}
	b := Bash{
		Sandbox: sb,
		Trusted: func(cmd string) bool { return strings.HasPrefix(cmd, "git ") },
	}
	// A trusted command must skip Wrap entirely and run normally.
	if _, err := b.Run(context.Background(), json.RawMessage(`{"command":"git --version"}`)); err != nil {
		t.Fatalf("trusted command should run, got %v", err)
	}
	if sb.calls != 0 {
		t.Errorf("trusted command must not be wrapped, Wrap called %d times", sb.calls)
	}
}

// TestBashDisabledSandboxNotWrapped verifies a disabled sandbox is never invoked.
func TestBashDisabledSandboxNotWrapped(t *testing.T) {
	sb := &fakeSandbox{enabled: false, wrapErr: errors.New("should not be called")}
	b := Bash{Sandbox: sb}
	if _, err := b.Run(context.Background(), json.RawMessage(`{"command":"echo hi"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sb.calls != 0 {
		t.Errorf("disabled sandbox must not wrap, Wrap called %d times", sb.calls)
	}
}

// TestBashNilSandboxUnchanged verifies a zero-value Bash (nil sandbox) behaves
// exactly as before — no wrapping, command runs.
func TestBashNilSandboxUnchanged(t *testing.T) {
	b := Bash{}
	out, err := b.Run(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("expected command output, got %q", out)
	}
}

// TestCodeExecFailsClosedOnWrapError verifies CodeExec wraps when enabled and
// fails closed on a Wrap error (the interpreter is never executed).
func TestCodeExecFailsClosedOnWrapError(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	sb := &fakeSandbox{enabled: true, wrapErr: errors.New("no backend")}
	c := CodeExec{Sandbox: sb}
	_, err := c.Run(context.Background(), json.RawMessage(`{"language":"go","code":"package main\nfunc main(){}"}`))
	if err == nil {
		t.Fatalf("expected fail-closed error")
	}
	if !strings.Contains(err.Error(), "refusing to run unsandboxed") {
		t.Errorf("expected fail-closed message, got %v", err)
	}
	if sb.calls != 1 {
		t.Errorf("expected Wrap attempted once, got %d", sb.calls)
	}
}

// TestCodeExecWrapsWhenEnabled verifies CodeExec calls Wrap and, once wrapped to
// the neutral command, runs cleanly.
func TestCodeExecWrapsWhenEnabled(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	sb := &fakeSandbox{enabled: true}
	c := CodeExec{Sandbox: sb}
	if _, err := c.Run(context.Background(), json.RawMessage(`{"language":"go","code":"package main\nfunc main(){}"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sb.calls != 1 {
		t.Errorf("expected Wrap called once, got %d", sb.calls)
	}
}
