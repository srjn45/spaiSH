package permissions

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Tier represents the permission level required to run a command.
type Tier int

const (
	TierPassthrough Tier = iota // bypass AI, run directly
	TierRead                    // execute silently
	TierWrite                   // show plan, confirm once
	TierElevated                // explicit prompt, exact command shown
	TierDestructive             // hard confirm, cannot be undone
)

func (t Tier) String() string {
	switch t {
	case TierPassthrough:
		return "passthrough"
	case TierRead:
		return "read"
	case TierWrite:
		return "write"
	case TierElevated:
		return "elevated"
	case TierDestructive:
		return "destructive"
	default:
		return "unknown"
	}
}

// Display returns the human-readable label shown to the user.
func (t Tier) Display() string {
	switch t {
	case TierPassthrough:
		return "Passthrough"
	case TierRead:
		return "Read"
	case TierWrite:
		return "Write"
	case TierElevated:
		return "Elevated (requires sudo)"
	case TierDestructive:
		return "Destructive — cannot be undone"
	default:
		return "Unknown"
	}
}

// passthroughCmds only observe trivial shell state.
var passthroughCmds = map[string]bool{
	"ls": true, "cd": true, "pwd": true, "clear": true, "exit": true,
	"history": true, "echo": true, "which": true, "type": true,
	"alias": true, "env": true, "printenv": true, "man": true, "help": true,
	"true": true, "false": true, "test": true,
}

// readCmds observe the system without modifying it.
var readCmds = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true,
	"find": true, "locate": true, "wc": true, "diff": true, "file": true,
	"stat": true, "du": true, "df": true, "ps": true, "htop": true, "top": true,
	"netstat": true, "ss": true, "lsof": true, "ping": true, "traceroute": true,
	"nslookup": true, "dig": true, "host": true, "curl": true, "wget": true,
	"free": true, "uptime": true, "uname": true, "whoami": true, "id": true,
	"date": true, "hostname": true, "tree": true, "realpath": true, "readlink": true,
	"journalctl": true, "dmesg": true, "sort": true, "uniq": true, "cut": true,
	"awk": true, "jq": true, "xxd": true, "od": true, "basename": true, "dirname": true,
}

// writeCmds modify files or state but are non-privileged and reversible-ish.
var writeCmds = map[string]bool{
	"mkdir": true, "touch": true, "cp": true, "mv": true, "ln": true,
	"chmod": true, "chown": true, "sed": true, "tee": true,
	"tar": true, "zip": true, "unzip": true, "gzip": true, "gunzip": true,
	"make": true, "go": true, "npm": true, "yarn": true, "pnpm": true,
	"cargo": true, "python": true, "python3": true, "node": true, "ruby": true,
}

// elevatedCmds require elevated privileges or change system services/packages.
var elevatedCmds = map[string]bool{
	"sudo": true, "doas": true, "su": true,
	"apt": true, "apt-get": true, "aptitude": true, "yum": true, "dnf": true,
	"pacman": true, "zypper": true, "snap": true, "flatpak": true,
	"mount": true, "umount": true, "useradd": true, "userdel": true, "passwd": true,
}

// destructiveCmds are inherently dangerous and hard/impossible to undo.
var destructiveCmds = map[string]bool{
	"mkfs": true, "fdisk": true, "parted": true, "dd": true,
	"shred": true, "wipefs": true, "mkswap": true, "mke2fs": true,
}

// sudoValueFlags are sudo/doas/su options that consume the following argument.
// Without skipping the value, the wrapped program is misidentified as the
// flag's value (e.g. "sudo -u root rm -rf /" would treat "root" as the program
// and never classify the rm). Only flags that unambiguously take a separate
// value are listed; boolean flags like sudo's -s/-i must not appear here or a
// real command token would be skipped.
var sudoValueFlags = map[string]bool{
	"-u": true, "--user": true,
	"-g": true, "--group": true, "-G": true,
	"-h": true, "--host": true,
	"-p": true, "--prompt": true,
	"-C": true, "--close-from": true,
	"-r": true, "--role": true,
	"-t": true, "--type": true,
	"-T": true, "--command-timeout": true,
	"-U": true, "--other-user": true,
	"-R": true, "--chroot": true,
	"-D": true, "--chdir": true,
	"-a": true, // doas auth style
	"-c": true, // su command string
	"-w": true, // su whitelist-environment
}

// gitValueFlags are git global options (before the subcommand) that consume the
// following argument, e.g. "git -C /repo clean -fd". Without skipping the value
// the subcommand is misidentified as the path and dangerous subcommands slip
// through as generic writes.
var gitValueFlags = map[string]bool{
	"-C": true, "-c": true,
	"--git-dir": true, "--work-tree": true, "--namespace": true,
}

// slashTiers gives the permission tier for the built-in slash commands. Mutating
// commands (/undo, /redo) appear at TierWrite; read-only commands that are
// queryable by external callers appear at TierRead. Commands absent from this
// map are ungated (pure REPL display output).
var slashTiers = map[string]Tier{
	"/undo": TierWrite,
	"/redo": TierWrite,
	"/jobs": TierRead,
}

// SlashTier returns the tier for a built-in slash command, and false for
// commands the tier system does not gate. /undo and /redo rewrite files, so they
// classify at TierWrite and prompt once in manual mode. The name may be given
// with or without its leading slash.
func SlashTier(name string) (Tier, bool) {
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	t, ok := slashTiers[name]
	return t, ok
}

// Classify returns the permission tier for the given shell command. It parses
// the command with a real shell parser and classifies every simple command in
// the pipeline/list, returning the most dangerous tier found. Classification is
// static and offline — no AI involvement.
func Classify(command string) Tier {
	command = strings.TrimSpace(command)
	if command == "" {
		return TierPassthrough
	}

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		// Unparseable (or non-POSIX) — fail safe by treating as Write.
		return maxTier(TierWrite, rawEscalations(command))
	}

	tier := TierPassthrough
	syntax.Walk(prog, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok {
			tier = maxTier(tier, classifyCall(call))
		}
		return true
	})
	return maxTier(tier, rawEscalations(command))
}

// classifyCall classifies a single simple command (program + args).
func classifyCall(call *syntax.CallExpr) Tier {
	words := litWords(call.Args)
	if len(words) == 0 {
		return TierPassthrough
	}
	prog := baseName(words[0])
	args := words[1:]

	// sudo/doas/su: elevate, and also inspect the wrapped command.
	if prog == "sudo" || prog == "doas" || prog == "su" {
		inner := TierPassthrough
		// Skip leading flags (and the values of value-taking flags) to find the
		// wrapped program, then classify it with its own arguments.
		for i := 0; i < len(args); i++ {
			if strings.HasPrefix(args[i], "-") {
				if sudoValueFlags[args[i]] {
					i++ // also skip this flag's value
				}
				continue
			}
			inner = classifyProg(baseName(args[i]), args[i+1:])
			break
		}
		return maxTier(TierElevated, inner)
	}
	return classifyProg(prog, args)
}

func classifyProg(prog string, args []string) Tier {
	switch {
	case prog == "rm":
		if hasAnyFlag(args, "r", "R", "f", "recursive", "force") {
			return TierDestructive
		}
		return TierWrite
	case prog == "find":
		return classifyFind(args)
	case prog == "tee":
		for _, a := range args {
			if isRawDiskDevice(a) {
				return TierDestructive
			}
		}
		return TierWrite
	case prog == "git":
		return classifyGit(args)
	case prog == "docker":
		return classifyDocker(args)
	case prog == "systemctl":
		return classifySystemctl(args)
	case strings.HasPrefix(prog, "mkfs"): // mkfs, mkfs.ext4, mkfs.xfs, ...
		return TierDestructive
	case destructiveCmds[prog]:
		return TierDestructive
	case elevatedCmds[prog]:
		return TierElevated
	case prog == "pip" || prog == "pip3":
		if contains(args, "install") || contains(args, "uninstall") {
			return TierElevated
		}
		return TierRead
	case passthroughCmds[prog]:
		return TierPassthrough
	case readCmds[prog]:
		return TierRead
	case writeCmds[prog]:
		return TierWrite
	default:
		// Unknown commands default to Write (safe: requires confirmation).
		return TierWrite
	}
}

// classifyFind classifies a find invocation. find itself only reads, but
// -delete removes matched entries and -exec/-ok run an arbitrary command per
// match, so those escalate to the tier of what they actually do.
func classifyFind(args []string) Tier {
	tier := TierRead
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-delete":
			tier = maxTier(tier, TierDestructive)
		case "-exec", "-execdir", "-ok", "-okdir":
			// The executed command runs up to a ';' or '+' terminator.
			cmd := args[i+1:]
			for j, tok := range cmd {
				if tok == ";" || tok == "+" {
					cmd = cmd[:j]
					break
				}
			}
			if len(cmd) > 0 {
				tier = maxTier(tier, classifyProg(baseName(cmd[0]), cmd[1:]))
			}
		}
	}
	return tier
}

func classifyGit(args []string) Tier {
	sub := gitSubcommand(args)
	switch sub {
	case "log", "status", "diff", "show", "branch", "remote", "fetch", "ls-files", "blame":
		return TierRead
	case "reset":
		if contains(args, "--hard") {
			return TierDestructive
		}
		return TierWrite
	case "clean":
		if hasAnyFlag(args, "f", "force", "d") {
			return TierDestructive
		}
		return TierWrite
	case "push":
		if contains(args, "--force") || contains(args, "-f") {
			return TierElevated
		}
		return TierWrite
	default:
		return TierWrite
	}
}

func classifyDocker(args []string) Tier {
	sub := firstNonFlag(args)
	switch sub {
	case "ps", "images", "logs", "inspect", "version", "info":
		return TierRead
	case "system", "rmi", "rm":
		return TierDestructive // prune / remove
	default:
		return TierElevated
	}
}

func classifySystemctl(args []string) Tier {
	sub := firstNonFlag(args)
	switch sub {
	case "status", "show", "list-units", "list-unit-files", "is-active", "is-enabled", "cat":
		return TierRead
	default:
		return TierElevated
	}
}

// rawEscalations catches dangerous shapes that are easier to spot in the raw
// string than the AST (device redirects, SQL DDL).
func rawEscalations(command string) Tier {
	lower := strings.ToLower(command)
	for _, dev := range []string{"/dev/sd", "/dev/nvme", "/dev/disk", "/dev/hd", "/dev/vd", "/dev/mmcblk"} {
		if strings.Contains(lower, dev) {
			if strings.Contains(command, ">") || strings.Contains(lower, "of=") {
				return TierDestructive
			}
			break
		}
	}
	return TierPassthrough
}

// --- helpers ---

func maxTier(a, b Tier) Tier {
	if a > b {
		return a
	}
	return b
}

// litWords returns the literal text of each word, skipping words that contain
// unresolvable expansions (treated as empty).
func litWords(words []*syntax.Word) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, wordLit(w))
	}
	return out
}

func wordLit(w *syntax.Word) string {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					b.WriteString(lit.Value)
				}
			}
		}
	}
	return b.String()
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// gitSubcommand returns the git subcommand, skipping global options and the
// values of value-taking global options so the subcommand isn't confused with
// an option value (e.g. "git -C /repo clean" -> "clean").
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a
		}
		if gitValueFlags[a] {
			i++ // skip the option's value
		}
	}
	return ""
}

// isRawDiskDevice reports whether path refers to a whole raw block device that
// writing to would corrupt (e.g. /dev/sda, /dev/nvme0n1, /dev/disk0).
func isRawDiskDevice(path string) bool {
	return strings.HasPrefix(path, "/dev/sd") ||
		strings.HasPrefix(path, "/dev/nvme") ||
		strings.HasPrefix(path, "/dev/disk") ||
		strings.HasPrefix(path, "/dev/hd") ||
		strings.HasPrefix(path, "/dev/vd") ||
		strings.HasPrefix(path, "/dev/mmcblk")
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// hasAnyFlag reports whether any short flag char or long flag name appears in
// args. Single-char names are matched inside clustered short flags (e.g. "-rf").
func hasAnyFlag(args []string, names ...string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		if strings.HasPrefix(a, "--") {
			long := strings.TrimPrefix(a, "--")
			for _, n := range names {
				if len(n) > 1 && long == n {
					return true
				}
			}
			continue
		}
		cluster := strings.TrimPrefix(a, "-")
		for _, n := range names {
			if len(n) == 1 && strings.Contains(cluster, n) {
				return true
			}
		}
	}
	return false
}
