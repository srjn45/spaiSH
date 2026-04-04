package permissions_test

import (
	"testing"

	"spaios/internal/permissions"
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

func TestTierString(t *testing.T) {
	if permissions.TierDestructive.String() != "destructive" {
		t.Error("unexpected string for TierDestructive")
	}
	if permissions.TierPassthrough.String() != "passthrough" {
		t.Error("unexpected string for TierPassthrough")
	}
}
