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
	"shred": true, "wipefs": true, "mkswap": true,
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
		if len(args) > 0 {
			// Skip sudo flags to find the wrapped program.
			for i := 0; i < len(args); i++ {
				if strings.HasPrefix(args[i], "-") {
					continue
				}
				inner = classifyProg(baseName(args[i]), args[i+1:])
				break
			}
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
	case prog == "git":
		return classifyGit(args)
	case prog == "docker":
		return classifyDocker(args)
	case prog == "systemctl":
		return classifySystemctl(args)
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

func classifyGit(args []string) Tier {
	sub := firstNonFlag(args)
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
	if strings.Contains(lower, "/dev/sd") || strings.Contains(lower, "/dev/nvme") || strings.Contains(lower, "/dev/disk") {
		if strings.Contains(command, ">") || strings.Contains(lower, "of=") {
			return TierDestructive
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
