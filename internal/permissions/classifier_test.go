package permissions_test

import (
	"testing"

	"spaish/internal/permissions"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		command string
		want    permissions.Tier
	}{
		// Passthrough
		{"ls", permissions.TierPassthrough},
		{"ls -la", permissions.TierPassthrough},
		{"cd /tmp", permissions.TierPassthrough},
		{"pwd", permissions.TierPassthrough},
		{"clear", permissions.TierPassthrough},
		// Read
		{"cat /etc/hosts", permissions.TierRead},
		{"grep error /var/log/syslog", permissions.TierRead},
		{"ps aux", permissions.TierRead},
		{"git status", permissions.TierRead},
		{"git log --oneline", permissions.TierRead},
		{"systemctl status nginx", permissions.TierRead},
		{"journalctl -u nginx -n 50", permissions.TierRead},
		// Write
		{"mkdir /tmp/test", permissions.TierWrite},
		{"touch file.txt", permissions.TierWrite},
		{"cp src dst", permissions.TierWrite},
		{"mv old new", permissions.TierWrite},
		{"git commit -m msg", permissions.TierWrite},
		// Elevated
		{"sudo systemctl restart nginx", permissions.TierElevated},
		{"sudo apt install curl", permissions.TierElevated},
		{"systemctl restart nginx", permissions.TierElevated},
		{"apt-get update", permissions.TierElevated},
		// Destructive
		{"rm -rf /tmp/test", permissions.TierDestructive},
		{"rm -r /home/user/old", permissions.TierDestructive},
		{"git reset --hard HEAD~1", permissions.TierDestructive},
		{"git clean -f", permissions.TierDestructive},
		{"docker system prune", permissions.TierDestructive},
	}

	for _, tc := range cases {
		got := permissions.Classify(tc.command)
		if got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// TestSlashTier covers the built-in slash commands the tier system gates.
func TestSlashTier(t *testing.T) {
	cases := []struct {
		name   string
		want   permissions.Tier
		wantOK bool
	}{
		{"/undo", permissions.TierWrite, true},
		{"/redo", permissions.TierWrite, true},
		{"undo", permissions.TierWrite, true}, // leading slash optional
		{"/help", 0, false},                   // read-only command: ungated
		{"/deploy", 0, false},                 // custom command: not tier-gated here
	}
	for _, tc := range cases {
		got, ok := permissions.SlashTier(tc.name)
		if ok != tc.wantOK {
			t.Errorf("SlashTier(%q) ok = %v, want %v", tc.name, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Errorf("SlashTier(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestClassifyParsing covers cases the old substring matcher got wrong.
func TestClassifyParsing(t *testing.T) {
	cases := []struct {
		command string
		want    permissions.Tier
	}{
		// Flag variants the substring matcher missed.
		{"rm  -rf x", permissions.TierDestructive}, // double space
		{"rm --recursive x", permissions.TierDestructive},
		{"rm -r -f x", permissions.TierDestructive}, // separate flags
		{"rm file.txt", permissions.TierWrite},      // plain rm is not destructive
		// Over-matching: a dangerous string inside an argument is not a command.
		{`echo "rm -rf /"`, permissions.TierPassthrough},
		{`grep "drop table" log.sql`, permissions.TierRead},
		// Compound commands take the most dangerous tier.
		{"ls && rm -rf b", permissions.TierDestructive},
		{"cat a.txt | grep x", permissions.TierRead},
		{"mkdir d; cp a b", permissions.TierWrite},
		{"sudo rm -rf /", permissions.TierDestructive},
		// Command substitution runs the inner command.
		{"echo $(rm -rf x)", permissions.TierDestructive},
		// Device writes.
		{"dd if=/dev/zero of=/dev/sda", permissions.TierDestructive},
		// git push --force is elevated, plain push is write.
		{"git push --force origin main", permissions.TierElevated},
		{"git push origin main", permissions.TierWrite},
	}
	for _, tc := range cases {
		if got := permissions.Classify(tc.command); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// TestClassifyBypassRegressions locks in fixes for commands that previously
// under-classified (each line documents the tier they wrongly returned before).
func TestClassifyBypassRegressions(t *testing.T) {
	cases := []struct {
		command string
		want    permissions.Tier
	}{
		// sudo/doas value-taking flags used to hide the wrapped program: the
		// flag's value ("root") was mistaken for the program, so the real rm
		// was never seen (was elevated).
		{"sudo -u root rm -rf /", permissions.TierDestructive},
		{"sudo --user root rm -rf /", permissions.TierDestructive},
		{"doas -u root rm -rf /home", permissions.TierDestructive},
		{"sudo -g wheel -u root rm -rf /", permissions.TierDestructive},
		// Attached-value form always worked; keep it covered.
		{"sudo -uroot rm -rf /", permissions.TierDestructive},
		// A benign sudo command stays elevated (no over-classification).
		{"sudo -u root ls", permissions.TierElevated},
		// find that deletes or executes a command (was read).
		{"find . -delete", permissions.TierDestructive},
		{"find /tmp -name '*.log' -exec rm -rf {} +", permissions.TierDestructive},
		{"find . -type f -exec rm {} \\;", permissions.TierWrite}, // rm w/o -rf is write
		{"find . -execdir shred {} +", permissions.TierDestructive},
		{"find . -name '*.go' -exec cat {} \\;", permissions.TierRead}, // read-only exec stays read
		{"find . -type f -name '*.tmp'", permissions.TierRead},         // plain find is read
		// mkfs variants (was write): the base name has a filesystem suffix.
		{"mkfs.ext4 /dev/sda1", permissions.TierDestructive},
		{"mkfs.xfs /dev/nvme0n1", permissions.TierDestructive},
		{"mke2fs /dev/sda1", permissions.TierDestructive},
		// Writing a raw disk device via tee (was write).
		{"echo x | tee /dev/sda", permissions.TierDestructive},
		{"cat img | sudo tee /dev/nvme0n1", permissions.TierDestructive},
		{"echo hi | tee ./notes.txt", permissions.TierWrite}, // ordinary tee stays write
		// git global value-options hiding a dangerous subcommand (was write).
		{"git -C /repo clean -fd", permissions.TierDestructive},
		{"git -C /repo reset --hard", permissions.TierDestructive},
		{"git -c core.pager=cat -C /r clean -f", permissions.TierDestructive},
		{"git -C /repo status", permissions.TierRead}, // read subcommand still read
		// Raw-disk redirect coverage broadened to more device families.
		{"echo boom > /dev/vda", permissions.TierDestructive},
		{"dd if=/dev/zero of=/dev/mmcblk0", permissions.TierDestructive},
	}
	for _, tc := range cases {
		if got := permissions.Classify(tc.command); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// TestClassifyMoreCommands covers docker/systemctl subcommands, the
// unparseable-input fail-safe, and empty/whitespace input.
func TestClassifyMoreCommands(t *testing.T) {
	cases := []struct {
		command string
		want    permissions.Tier
	}{
		// docker
		{"docker ps", permissions.TierRead},
		{"docker images", permissions.TierRead},
		{"docker rmi img", permissions.TierDestructive},
		{"docker system prune -af", permissions.TierDestructive},
		{"docker run -it ubuntu", permissions.TierElevated},
		// systemctl
		{"systemctl is-active nginx", permissions.TierRead},
		{"systemctl daemon-reload", permissions.TierElevated},
		// pip
		{"pip install requests", permissions.TierElevated},
		{"pip3 uninstall requests", permissions.TierElevated},
		{"pip list", permissions.TierRead},
		// empty / whitespace input
		{"", permissions.TierPassthrough},
		{"   ", permissions.TierPassthrough},
		// Unparseable input fails safe to at least Write, and still catches a
		// device write via the raw-string escalation path.
		{`echo "unterminated`, permissions.TierWrite},
		{`dd of=/dev/sda if="unterminated`, permissions.TierDestructive},
	}
	for _, tc := range cases {
		if got := permissions.Classify(tc.command); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// TestClassifyGH covers the gh CLI subcommand tier mapping wired into Classify.
func TestClassifyGH(t *testing.T) {
	cases := []struct {
		command string
		want    permissions.Tier
	}{
		// PR read-only
		{"gh pr view", permissions.TierRead},
		{"gh pr view 123", permissions.TierRead},
		{"gh pr list", permissions.TierRead},
		{"gh pr list --limit 10", permissions.TierRead},
		{"gh pr status", permissions.TierRead},
		{"gh pr checks", permissions.TierRead},
		{"gh pr diff", permissions.TierRead},
		// PR local mutation
		{"gh pr checkout 42", permissions.TierWrite},
		// PR outward-facing (remote mutations)
		{"gh pr create --title x --body y", permissions.TierElevated},
		{"gh pr merge 42", permissions.TierElevated},
		{"gh pr close 42", permissions.TierElevated},
		{"gh pr comment --body hi", permissions.TierElevated},
		{"gh pr reopen 42", permissions.TierElevated},
		{"gh pr review --approve", permissions.TierElevated},
		// Run subcommand
		{"gh run view", permissions.TierRead},
		{"gh run list", permissions.TierRead},
		{"gh run watch 123", permissions.TierRead},
		{"gh run rerun 123", permissions.TierElevated},
		{"gh run cancel 123", permissions.TierElevated},
		// Repo subcommand
		{"gh repo view", permissions.TierRead},
		{"gh repo clone owner/repo", permissions.TierRead},
		{"gh repo create myrepo", permissions.TierElevated},
		{"gh repo fork upstream/repo", permissions.TierElevated},
		// Issue subcommand
		{"gh issue view 1", permissions.TierRead},
		{"gh issue list", permissions.TierRead},
		{"gh issue status", permissions.TierRead},
		{"gh issue create --title x", permissions.TierElevated},
		{"gh issue close 1", permissions.TierElevated},
		// Release subcommand
		{"gh release view v1.0", permissions.TierRead},
		{"gh release list", permissions.TierRead},
		{"gh release create v1.1", permissions.TierElevated},
		// Unknown gh subcommand defaults to Write
		{"gh auth login", permissions.TierWrite},
		{"gh extension list", permissions.TierWrite},
	}
	for _, tc := range cases {
		got := permissions.Classify(tc.command)
		if got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestTierStringAndDisplay(t *testing.T) {
	tiers := []struct {
		tier    permissions.Tier
		str     string
		display string
	}{
		{permissions.TierPassthrough, "passthrough", "Passthrough"},
		{permissions.TierRead, "read", "Read"},
		{permissions.TierWrite, "write", "Write"},
		{permissions.TierElevated, "elevated", "Elevated (requires sudo)"},
		{permissions.TierDestructive, "destructive", "Destructive — cannot be undone"},
	}
	for _, tc := range tiers {
		if got := tc.tier.String(); got != tc.str {
			t.Errorf("%d.String() = %q, want %q", tc.tier, got, tc.str)
		}
		if got := tc.tier.Display(); got != tc.display {
			t.Errorf("%d.Display() = %q, want %q", tc.tier, got, tc.display)
		}
	}
	// Out-of-range tier falls through to the default arms.
	bogus := permissions.Tier(99)
	if bogus.String() != "unknown" || bogus.Display() != "Unknown" {
		t.Errorf("bogus tier = (%q, %q), want (unknown, Unknown)", bogus.String(), bogus.Display())
	}
}
