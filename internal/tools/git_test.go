package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/permissions"
)

// setupRepo creates a temp git repository with one committed file and returns
// its path. Author/committer identity is supplied via env so the commit works
// without touching global git config.
func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "initial")
	return dir
}

func TestGitReadSubcommands(t *testing.T) {
	repo := setupRepo(t)
	t.Chdir(repo)

	cases := []struct {
		name  string
		input string
	}{
		{"status", `{"subcommand":"status"}`},
		{"diff", `{"subcommand":"diff"}`},
		{"log", `{"subcommand":"log","args":["-n","1"]}`},
		{"blame", `{"subcommand":"blame","args":["file.txt"]}`},
		{"show", `{"subcommand":"show"}`},
		{"branch", `{"subcommand":"branch"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, Git{}, tc.input) // run fails the test on a Go error
			if out == "" {
				t.Fatalf("%s: empty output", tc.name)
			}
		})
	}
}

func TestGitStatusReflectsChange(t *testing.T) {
	repo := setupRepo(t)
	t.Chdir(repo)
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := run(t, Git{}, `{"subcommand":"status","args":["--porcelain"]}`)
	if !strings.Contains(out, "new.txt") {
		t.Fatalf("status did not report untracked file: %q", out)
	}
}

func TestGitArgsAsString(t *testing.T) {
	// args may arrive as a whitespace-delimited string instead of an array.
	sub, args := GitCall(json.RawMessage(`{"subcommand":"log","args":"-n 20"}`))
	if sub != "log" {
		t.Fatalf("subcommand = %q", sub)
	}
	if len(args) != 2 || args[0] != "-n" || args[1] != "20" {
		t.Fatalf("args = %#v", args)
	}
}

func TestGitInvalidSubcommand(t *testing.T) {
	if _, err := (Git{}).Run(context.Background(), json.RawMessage(`{"subcommand":"nuke"}`)); err == nil {
		t.Fatal("expected error for unsupported subcommand")
	}
	if _, err := (Git{}).Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing subcommand")
	}
}

func TestGitNotARepo(t *testing.T) {
	t.Chdir(t.TempDir()) // plain dir, not a git repo
	out, err := (Git{}).Run(context.Background(), json.RawMessage(`{"subcommand":"status"}`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(out, "not a git repository") {
		t.Fatalf("expected 'not a git repository' in output, got: %q", out)
	}
	if !strings.Contains(out, "exit code") {
		t.Fatalf("expected exit code report, got: %q", out)
	}
}

func TestGitTier(t *testing.T) {
	cases := []struct {
		sub  string
		args []string
		want permissions.Tier
	}{
		// read-only inspection
		{"status", nil, permissions.TierRead},
		{"diff", []string{"file.txt"}, permissions.TierRead},
		{"log", []string{"-n", "20"}, permissions.TierRead},
		{"blame", []string{"file.txt"}, permissions.TierRead},
		{"show", nil, permissions.TierRead},
		// branch: listing vs mutating
		{"branch", nil, permissions.TierRead},
		{"branch", []string{"-a"}, permissions.TierRead},
		{"branch", []string{"-v"}, permissions.TierRead},
		{"branch", []string{"feature"}, permissions.TierWrite},
		{"branch", []string{"-d", "old"}, permissions.TierWrite},
		{"branch", []string{"-D", "old"}, permissions.TierWrite},
		{"branch", []string{"-m", "a", "b"}, permissions.TierWrite},
		// mutating working-tree/index ops
		{"add", []string{"."}, permissions.TierWrite},
		{"commit", []string{"-m", "x"}, permissions.TierWrite},
		{"checkout", []string{"main"}, permissions.TierWrite},
		{"reset", nil, permissions.TierWrite},
		{"reset", []string{"--hard"}, permissions.TierDestructive},
		// push escalation
		{"push", nil, permissions.TierElevated},
		{"push", []string{"origin", "main"}, permissions.TierElevated},
		{"push", []string{"--force"}, permissions.TierDestructive},
		{"push", []string{"-f"}, permissions.TierDestructive},
		{"push", []string{"--force-with-lease"}, permissions.TierDestructive},
		// unknown subcommand defaults to Write
		{"merge", nil, permissions.TierWrite},
	}
	for _, tc := range cases {
		got := GitTier(tc.sub, tc.args)
		if got != tc.want {
			t.Errorf("GitTier(%q, %v) = %v, want %v", tc.sub, tc.args, got, tc.want)
		}
	}
}
