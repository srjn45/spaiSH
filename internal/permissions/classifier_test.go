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

func TestTierString(t *testing.T) {
	if permissions.TierDestructive.String() != "destructive" {
		t.Error("unexpected string for TierDestructive")
	}
	if permissions.TierPassthrough.String() != "passthrough" {
		t.Error("unexpected string for TierPassthrough")
	}
}
