// Package sandbox provides an opt-in, default-OFF containment layer that
// restricts the filesystem and network reach of the subprocesses spawned by the
// bash and code_exec tools. It is defense-in-depth layered UNDER the permission
// gate: it is applied only after a command has already been allowed to run, and
// it never alters a tool's classified tier or suppresses a confirmation prompt.
//
// The real enforcement lives behind //go:build linux (bubblewrap, or a native
// Landlock + seccomp trampoline). On every other platform, and whenever the
// sandbox is disabled, New returns a Noop that leaves commands untouched — so
// the interface is identical across platforms and callers never use build tags.
package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Backend selectors accepted in Config.Backend.
const (
	BackendAuto     = "auto"     // prefer bwrap, else native Landlock/seccomp
	BackendBwrap    = "bwrap"    // require bubblewrap
	BackendLandlock = "landlock" // require the native Landlock/seccomp trampoline
	BackendOff      = "off"      // explicitly disable enforcement (like disabled)

	// TrampolineSubcommand is the hidden argv[1] the native backend re-enters the
	// binary with. Its handler applies the restrictions and execs the real
	// command; it performs no gating and is reachable only via Wrap.
	TrampolineSubcommand = "__sandbox-exec"
)

// Config is the platform-independent sandbox configuration, derived from the
// [sandbox] section of spaid.toml. The zero value is disabled.
type Config struct {
	// Enabled is the master opt-in. When false, New returns a Noop and behavior
	// is byte-for-byte identical to a build without the sandbox.
	Enabled bool
	// AllowNetwork keeps network access open when true. When false (the default
	// once enabled), the child process is denied network reach.
	AllowNetwork bool
	// AllowPaths lists extra writable directories. The command's working
	// directory (and thus the code_exec throwaway temp dir) is always writable in
	// addition to these.
	AllowPaths []string
	// Backend selects the enforcement mechanism; see the Backend* constants. An
	// empty value is treated as BackendAuto.
	Backend string
}

// Sandbox restricts the filesystem and network reach of a child process.
type Sandbox interface {
	// Wrap rewrites cmd in place so the process it launches runs under the
	// sandbox's restrictions. It MUST be called after cmd is otherwise fully
	// configured (Dir, Env, Stdout/Stderr). It returns an error only when the
	// sandbox is Enabled but cannot be established; callers MUST treat that as
	// fail-closed and abort the run rather than executing unsandboxed.
	Wrap(cmd *exec.Cmd) error
	// Enabled reports whether restrictions are actually enforced. Noop (and the
	// non-Linux fallback) report false.
	Enabled() bool
}

// Noop is the do-nothing sandbox used when the feature is disabled or the
// platform cannot enforce it. Wrap leaves the command untouched.
type Noop struct{}

// Wrap returns nil without modifying cmd.
func (Noop) Wrap(*exec.Cmd) error { return nil }

// Enabled always reports false for the Noop sandbox.
func (Noop) Enabled() bool { return false }

// writablePaths resolves the set of directories the sandboxed command may write
// to: the configured AllowPaths plus the command's effective working directory.
// For code_exec, cmd.Dir is the throwaway temp dir, so it is included
// automatically; for bash, cmd.Dir is empty and the process cwd is used, so the
// agent's working directory stays writable. Paths are made absolute and deduped.
func writablePaths(cfg Config, cmd *exec.Cmd) []string {
	dir := cmd.Dir
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range cfg.AllowPaths {
		add(p)
	}
	add(dir)
	return out
}

// buildBwrapArgv builds the bubblewrap argv that wraps cmdPath+cmdArgs. The whole
// filesystem is bound read-only, /dev and /proc are provided, each writable path
// is bound read-write, and (unless AllowNetwork) the network namespace is
// unshared. It is a pure function so it can be unit-tested on any platform;
// callers replace argv[0] with the resolved bwrap path. cmdArgs are the
// arguments after argv[0] (i.e. exec.Cmd.Args[1:]).
func buildBwrapArgv(cfg Config, writable []string, cmdPath string, cmdArgs []string) []string {
	argv := []string{
		BackendBwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--die-with-parent",
	}
	for _, p := range writable {
		argv = append(argv, "--bind", p, p)
	}
	if !cfg.AllowNetwork {
		argv = append(argv, "--unshare-net")
	}
	argv = append(argv, "--", cmdPath)
	argv = append(argv, cmdArgs...)
	return argv
}

// buildTrampolineArgs builds the restriction flags passed to the hidden
// __sandbox-exec subcommand (before the "--" that precedes the real command):
// one --allow-path per writable directory and a --net=on/off toggle. Pure and
// platform-independent so it is unit-tested everywhere.
func buildTrampolineArgs(cfg Config, writable []string) []string {
	args := make([]string, 0, len(writable)+1)
	for _, p := range writable {
		args = append(args, "--allow-path="+p)
	}
	if cfg.AllowNetwork {
		args = append(args, "--net=on")
	} else {
		args = append(args, "--net=off")
	}
	return args
}
