package permissions

import "strings"

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

// destructivePatterns are checked first — any match is TierDestructive.
var destructivePatterns = []string{
	"rm -rf", "rm -r ", "rm -f ", "rm -fr",
	"git reset --hard", "git clean -f", "git clean -fd",
	"docker system prune",
	"mkfs", "dd if=", "fdisk", "parted",
	"DROP TABLE", "DROP DATABASE", "TRUNCATE TABLE",
	"> /dev/", "truncate --size 0",
}

// elevatedPrefixes: any command starting with these is TierElevated.
var elevatedPrefixes = []string{
	"sudo ",
	"systemctl start ", "systemctl stop ", "systemctl restart ",
	"systemctl reload ", "systemctl enable ", "systemctl disable ",
	"apt ", "apt-get ", "aptitude ",
	"yum ", "dnf ", "pacman ", "zypper ",
	"snap install", "flatpak install",
	"pip install", "pip3 install",
	"npm install -g", "yarn global add",
}

// passthroughCmds: exact command or command + space prefix is TierPassthrough.
var passthroughCmds = []string{
	"ls", "cd", "pwd", "clear", "exit", "history",
	"echo", "which", "type", "alias", "env", "printenv",
	"man", "help",
}

// readPrefixes: commands that only observe the system.
var readPrefixes = []string{
	"cat ", "head ", "tail ", "less ", "more ",
	"grep ", "find ", "locate ", "wc ", "diff ",
	"file ", "stat ", "du ", "df ",
	"ps ", "htop", "top",
	"netstat", "ss ", "lsof ",
	"ping ", "traceroute ", "nslookup ", "dig ",
	"curl ", "wget ",
	"git log", "git status", "git diff", "git show", "git branch",
	"docker ps", "docker images", "docker logs",
	"systemctl status", "journalctl",
	"free ", "uptime", "uname",
}

// writePrefixes: commands that modify files or state (non-privileged).
var writePrefixes = []string{
	"mkdir ", "touch ", "cp ", "mv ", "ln ",
	"chmod ", "chown ",
	"git add", "git commit", "git stash", "git tag",
	"docker build", "docker-compose",
	"sed ", "awk ", "tee ",
	"tar ", "zip ", "unzip ", "gzip ", "gunzip ",
}

// Classify returns the permission tier for the given shell command.
// Classification is static and offline — no AI involvement.
func Classify(command string) Tier {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	// Destructive check first (highest risk)
	for _, p := range destructivePatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return TierDestructive
		}
	}

	// Elevated check
	for _, p := range elevatedPrefixes {
		if strings.HasPrefix(lower, p) {
			return TierElevated
		}
	}

	// Passthrough check
	for _, p := range passthroughCmds {
		if lower == p || strings.HasPrefix(lower, p+" ") {
			return TierPassthrough
		}
	}

	// Read check
	for _, p := range readPrefixes {
		if strings.HasPrefix(lower, p) || lower == strings.TrimRight(p, " ") {
			return TierRead
		}
	}

	// Write check
	for _, p := range writePrefixes {
		if strings.HasPrefix(lower, p) {
			return TierWrite
		}
	}

	// Unknown commands default to Write (safe default, requires confirmation)
	return TierWrite
}
