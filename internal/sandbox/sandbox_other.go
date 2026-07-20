//go:build !linux

package sandbox

import (
	"errors"
	"log"
	"strings"
)

// New returns a Noop sandbox on non-Linux platforms. Enforcement (Landlock,
// seccomp, bubblewrap) is Linux-only, so when the sandbox is enabled here we log
// a prominent warning and fall back to no enforcement. The permission gate still
// applies in full, so the security posture equals the pre-sandbox behavior —
// never worse.
func New(cfg Config) (Sandbox, error) {
	if cfg.Enabled && !strings.EqualFold(cfg.Backend, BackendOff) {
		log.Printf("sandbox: [sandbox].enabled is set, but execution sandboxing is only enforced on Linux; " +
			"running WITHOUT a sandbox (the permission gate still applies)")
	}
	return Noop{}, nil
}

// RunTrampoline is the non-Linux stub for the hidden __sandbox-exec subcommand.
// It is never reached in normal operation because the native backend that emits
// the trampoline invocation exists only on Linux.
func RunTrampoline([]string) error {
	return errors.New("sandbox trampoline is only supported on Linux")
}
