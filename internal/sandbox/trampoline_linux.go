//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// RunTrampoline is the handler for the hidden __sandbox-exec subcommand. It
// parses the restriction flags produced by buildTrampolineArgs (up to a "--"
// separator), applies Landlock + seccomp to the current process, then execs the
// real command that follows. It never returns on success; a returned error means
// the restrictions could not be applied (the caller must abort, not run
// unsandboxed). It performs no permission gating and is reachable only via Wrap.
func RunTrampoline(args []string) error {
	var allowPaths []string
	denyNetwork := true
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
		switch {
		case strings.HasPrefix(a, "--allow-path="):
			allowPaths = append(allowPaths, strings.TrimPrefix(a, "--allow-path="))
		case a == "--net=on":
			denyNetwork = false
		case a == "--net=off":
			denyNetwork = true
		default:
			return fmt.Errorf("sandbox trampoline: unknown argument %q", a)
		}
	}
	if sep < 0 || sep+1 >= len(args) {
		return fmt.Errorf("sandbox trampoline: no command to execute")
	}
	cmd := args[sep+1:]

	if err := ApplyRestrictions(allowPaths, denyNetwork); err != nil {
		return fmt.Errorf("sandbox trampoline: %w", err)
	}
	if err := syscall.Exec(cmd[0], cmd, os.Environ()); err != nil {
		return fmt.Errorf("sandbox trampoline: exec %q: %w", cmd[0], err)
	}
	return nil // unreachable: a successful Exec replaces the process image.
}

// ApplyRestrictions confines the calling process: it makes the whole filesystem
// read-only except the given writable subtrees (via Landlock), and, when
// denyNetwork is set, blocks the creation of IP sockets (via seccomp). Both are
// inherited across exec, so the command the trampoline runs stays confined.
func ApplyRestrictions(writablePaths []string, denyNetwork bool) error {
	// NO_NEW_PRIVS is mandatory for unprivileged Landlock and seccomp: it
	// guarantees the restrictions cannot be dropped by a later execve of a
	// setuid binary.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	if err := applyLandlock(writablePaths); err != nil {
		return fmt.Errorf("landlock: %w", err)
	}
	if denyNetwork {
		if err := applyNetworkSeccomp(); err != nil {
			return fmt.Errorf("seccomp: %w", err)
		}
	}
	return nil
}

// landlockABI queries the kernel's supported Landlock ABI version. A value >= 1
// means Landlock is available; err is non-nil (ENOSYS/EOPNOTSUPP) when it is not.
func landlockABI() (int, error) {
	r1, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, uintptr(unix.LANDLOCK_CREATE_RULESET_VERSION))
	if errno != 0 {
		return 0, errno
	}
	return int(r1), nil
}

// landlockAvailable reports whether the running kernel enforces Landlock.
func landlockAvailable() bool {
	abi, err := landlockABI()
	return err == nil && abi >= 1
}

// readOnlyAccessFS is the access mask granted to the root ("/") rule: read and
// execute, but no write/create/remove. It is masked against the kernel-supported
// set before use.
const readOnlyAccessFS = uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE |
	unix.LANDLOCK_ACCESS_FS_READ_FILE |
	unix.LANDLOCK_ACCESS_FS_READ_DIR)

// handledAccessFS returns the full set of filesystem access rights to place
// under Landlock control for the given ABI. Rights introduced in later ABIs are
// only included when the running kernel supports them, or create_ruleset would
// reject the mask.
func handledAccessFS(abi int) uint64 {
	h := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM)
	if abi >= 2 {
		h |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		h |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		h |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return h
}

// applyLandlock installs a Landlock ruleset that makes the entire filesystem
// read-only (a root "/" rule granting only read/execute), then grants full
// access to each writable subtree. Landlock resolves the most specific matching
// rule, so writes under an allowed path succeed while writes elsewhere are
// denied. Reads remain broadly permitted (as with bwrap's read-only bind of /);
// containment targets writes and, separately, the network.
func applyLandlock(writablePaths []string) error {
	abi, err := landlockABI()
	if err != nil {
		return err
	}
	if abi < 1 {
		return fmt.Errorf("kernel does not support Landlock")
	}
	handled := handledAccessFS(abi)

	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("create_ruleset: %w", errno)
	}
	rulesetFD := int(fd)
	defer unix.Close(rulesetFD)

	// Everything readable/executable, nothing writable, unless a more specific
	// writable rule below covers the path.
	if err := addPathRule(rulesetFD, "/", readOnlyAccessFS&handled); err != nil {
		return err
	}
	for _, p := range writablePaths {
		if err := addPathRule(rulesetFD, p, handled); err != nil {
			// A writable path that does not exist is skipped rather than fatal:
			// the allow-list may legitimately name a not-yet-created directory.
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
	}

	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(rulesetFD), 0, 0); errno != 0 {
		return fmt.Errorf("restrict_self: %w", errno)
	}
	return nil
}

// addPathRule adds a PATH_BENEATH rule granting access under path to the ruleset.
func addPathRule(rulesetFD int, path string, access uint64) error {
	pathFD, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer unix.Close(pathFD)

	beneath := unix.LandlockPathBeneathAttr{
		Allowed_access: access,
		Parent_fd:      int32(pathFD),
	}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFD), unix.LANDLOCK_RULE_PATH_BENEATH,
		uintptr(unsafe.Pointer(&beneath)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("add_rule %q: %w", path, errno)
	}
	return nil
}

// applyNetworkSeccomp installs a seccomp-bpf filter that denies socket(2) for the
// AF_INET and AF_INET6 families (returning EACCES), blocking TCP/UDP/raw IP
// networking while leaving AF_UNIX and all non-socket syscalls untouched.
func applyNetworkSeccomp() error {
	prog, err := networkSeccompProgram()
	if err != nil {
		return err
	}
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	_, _, errno := unix.Syscall(unix.SYS_PRCTL,
		unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&fprog)))
	runtime.KeepAlive(prog)
	if errno != 0 {
		return fmt.Errorf("prctl(PR_SET_SECCOMP): %w", errno)
	}
	return nil
}

// seccompArch returns the AUDIT_ARCH value and socket(2) syscall number for the
// current architecture, so the filter validates the arch and inspects the right
// syscall. Only 64-bit x86/arm are supported natively; other arches should use
// the bwrap backend.
func seccompArch() (auditArch uint32, socketNr uint32, err error) {
	switch runtime.GOARCH {
	case "amd64":
		return unix.AUDIT_ARCH_X86_64, uint32(unix.SYS_SOCKET), nil
	case "arm64":
		return unix.AUDIT_ARCH_AARCH64, uint32(unix.SYS_SOCKET), nil
	default:
		return 0, 0, fmt.Errorf("native network seccomp unsupported on GOARCH %q (use the bwrap backend)", runtime.GOARCH)
	}
}

// networkSeccompProgram builds the BPF filter denying IP socket creation. Offsets
// index the kernel's seccomp_data: nr at 0, arch at 4, args[0] (low word) at 16.
func networkSeccompProgram() ([]unix.SockFilter, error) {
	auditArch, socketNr, err := seccompArch()
	if err != nil {
		return nil, err
	}
	const (
		offNr   = 0
		offArch = 4
		offArg0 = 16
	)
	ld := uint16(unix.BPF_LD | unix.BPF_W | unix.BPF_ABS)
	jeq := uint16(unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K)
	ret := uint16(unix.BPF_RET | unix.BPF_K)

	retAllow := uint32(unix.SECCOMP_RET_ALLOW)
	retDeny := uint32(unix.SECCOMP_RET_ERRNO | (uint32(unix.EACCES) & unix.SECCOMP_RET_DATA))
	retKill := uint32(unix.SECCOMP_RET_KILL_PROCESS)

	return []unix.SockFilter{
		/* 0 */ {Code: ld, K: offArch},
		/* 1 */ {Code: jeq, K: auditArch, Jt: 0, Jf: 7}, // wrong arch -> KILL (9)
		/* 2 */ {Code: ld, K: offNr},
		/* 3 */ {Code: jeq, K: socketNr, Jt: 0, Jf: 3}, // not socket() -> ALLOW (7)
		/* 4 */ {Code: ld, K: offArg0},
		/* 5 */ {Code: jeq, K: uint32(unix.AF_INET), Jt: 2, Jf: 0}, // AF_INET -> DENY (8)
		/* 6 */ {Code: jeq, K: uint32(unix.AF_INET6), Jt: 1, Jf: 0}, // AF_INET6 -> DENY (8)
		/* 7 */ {Code: ret, K: retAllow},
		/* 8 */ {Code: ret, K: retDeny},
		/* 9 */ {Code: ret, K: retKill},
	}, nil
}
