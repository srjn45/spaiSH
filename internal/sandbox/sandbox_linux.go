//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// New builds the Linux sandbox from cfg. A disabled config (or Backend "off")
// yields a Noop. Otherwise it validates the configured allow_paths and returns a
// sandbox that selects its backend lazily at Wrap time. New does not fail merely
// because a backend binary/kernel-feature is missing — that is surfaced by Wrap
// so a disabled sandbox on a machine without bwrap still constructs cleanly.
func New(cfg Config) (Sandbox, error) {
	if !cfg.Enabled || strings.EqualFold(cfg.Backend, BackendOff) {
		return Noop{}, nil
	}
	for _, p := range cfg.AllowPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("sandbox allow_path %q: %w", p, err)
		}
	}
	return &linuxSandbox{cfg: cfg}, nil
}

// linuxSandbox enforces restrictions via bubblewrap or the native
// Landlock/seccomp trampoline.
type linuxSandbox struct {
	cfg Config
}

// Enabled reports true: a linuxSandbox is only constructed when enforcement is on.
func (s *linuxSandbox) Enabled() bool { return true }

// Wrap rewrites cmd to run under the selected backend, failing closed if no
// backend can be established.
func (s *linuxSandbox) Wrap(cmd *exec.Cmd) error {
	backend, err := s.resolveBackend()
	if err != nil {
		return err
	}
	writable := writablePaths(s.cfg, cmd)
	switch backend {
	case BackendBwrap:
		return s.wrapBwrap(cmd, writable)
	case BackendLandlock:
		return s.wrapTrampoline(cmd, writable)
	default:
		return fmt.Errorf("sandbox: no enforcement backend available")
	}
}

// resolveBackend picks the concrete backend, honoring an explicit Config.Backend
// and falling back to availability detection for "auto". It returns an error
// (fail-closed) when the requested or any backend is unavailable.
func (s *linuxSandbox) resolveBackend() (string, error) {
	switch strings.ToLower(strings.TrimSpace(s.cfg.Backend)) {
	case BackendBwrap:
		if _, err := exec.LookPath("bwrap"); err != nil {
			return "", fmt.Errorf("sandbox backend %q requested but bwrap is not on PATH: %w", BackendBwrap, err)
		}
		return BackendBwrap, nil
	case BackendLandlock:
		if !landlockAvailable() {
			return "", fmt.Errorf("sandbox backend %q requested but the kernel does not support Landlock", BackendLandlock)
		}
		return BackendLandlock, nil
	case "", BackendAuto:
		if _, err := exec.LookPath("bwrap"); err == nil {
			return BackendBwrap, nil
		}
		if landlockAvailable() {
			return BackendLandlock, nil
		}
		return "", fmt.Errorf("sandbox enabled but no backend available " +
			"(need bwrap on PATH or Landlock kernel support); refusing to run unsandboxed")
	default:
		return "", fmt.Errorf("unknown sandbox backend %q", s.cfg.Backend)
	}
}

// wrapBwrap rewrites cmd to run under bubblewrap.
func (s *linuxSandbox) wrapBwrap(cmd *exec.Cmd, writable []string) error {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("sandbox: bwrap not found: %w", err)
	}
	var cmdArgs []string
	if len(cmd.Args) > 1 {
		cmdArgs = cmd.Args[1:]
	}
	argv := buildBwrapArgv(s.cfg, writable, cmd.Path, cmdArgs)
	argv[0] = bwrap
	cmd.Path = bwrap
	cmd.Args = argv
	return nil
}

// wrapTrampoline rewrites cmd to re-exec this binary's hidden __sandbox-exec
// handler, which applies the native Landlock/seccomp restrictions and then execs
// the original command.
func (s *linuxSandbox) wrapTrampoline(cmd *exec.Cmd, writable []string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("sandbox: resolve self for trampoline: %w", err)
	}
	argv := []string{self, TrampolineSubcommand}
	argv = append(argv, buildTrampolineArgs(s.cfg, writable)...)
	argv = append(argv, "--", cmd.Path)
	if len(cmd.Args) > 1 {
		argv = append(argv, cmd.Args[1:]...)
	}
	cmd.Path = self
	cmd.Args = argv
	return nil
}
