package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/session"
)

// chdir switches the working directory to dir and returns a function that
// restores the original. gitBranch runs `git` in the process cwd, so tests that
// exercise it must not run in parallel.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("restore chdir %s: %v", orig, err)
		}
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns whatever
// was written. cmd/spai is package main and cannot import an internal test
// helper, so this mirrors the ~10-line pattern used elsewhere.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	r.Close()
	return string(data)
}

func TestDataDirWithXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/some/xdg/data")
	got := dataDir()
	want := filepath.Join("/some/xdg/data", "spaish")
	if got != want {
		t.Errorf("dataDir() = %q, want %q", got, want)
	}
}

func TestDataDirWithoutXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "spaish")
	if got := dataDir(); got != want {
		t.Errorf("dataDir() = %q, want %q", got, want)
	}
}

func TestStampPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	want := filepath.Join(dir, "spaish", ".first_run_done")
	if got := stampPath(); got != want {
		t.Errorf("stampPath() = %q, want %q", got, want)
	}
}

func TestShowDisclaimerFirstRunThenNoop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	// First call: no stamp file exists → prints the disclaimer and creates it.
	out := captureStdout(t, showDisclaimer)
	if !strings.Contains(out, "spaiSH") {
		t.Errorf("first showDisclaimer() output %q missing disclaimer text", out)
	}
	if _, err := os.Stat(stampPath()); err != nil {
		t.Errorf("stamp file not created: %v", err)
	}

	// Second call: stamp file exists → no output.
	out2 := captureStdout(t, showDisclaimer)
	if out2 != "" {
		t.Errorf("second showDisclaimer() should be a no-op, got %q", out2)
	}
}

func TestGitBranchNotARepo(t *testing.T) {
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	if got := gitBranch(); got != "" {
		t.Errorf("gitBranch() in non-repo = %q, want empty string", got)
	}
}

func TestGitBranchInRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	restore := chdir(t, dir)
	defer restore()

	// A commit is required before rev-parse --abbrev-ref HEAD reports the branch.
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	for _, args := range [][]string{
		{"init", "-q"},
		{"checkout", "-q", "-b", "feature-xyz"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	if got := gitBranch(); got != "feature-xyz" {
		t.Errorf("gitBranch() = %q, want %q", got, "feature-xyz")
	}
}

func TestIsShellSession(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		shellID string
		want    bool
	}{
		{"matches shellID exactly", "abc", "abc", true},
		{"all-digit id, empty shellID", "12345", "", true},
		{"non-digit id, empty shellID", "infra", "", false},
		{"empty id", "", "", false},
		{"mixed id, empty shellID", "12a45", "", false},
		{"non-matching shellID but all-digit id", "999", "abc", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isShellSession(c.id, c.shellID); got != c.want {
				t.Errorf("isShellSession(%q, %q) = %v, want %v", c.id, c.shellID, got, c.want)
			}
		})
	}
}

// writeSession creates a session dir under SessionsDir with a cache.json holding
// msgCount fake messages, matching the shape ListSessions reads.
func writeSession(t *testing.T, id string, msgCount int) {
	t.Helper()
	sessDir := filepath.Join(session.SessionsDir(), id)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", sessDir, err)
	}
	msgs := make([]string, msgCount)
	for i := range msgs {
		msgs[i] = `{"role":"user","content":"hi"}`
	}
	cache := `{"messages":[` + strings.Join(msgs, ",") + `]}`
	if err := os.WriteFile(filepath.Join(sessDir, "cache.json"), []byte(cache), 0600); err != nil {
		t.Fatalf("write cache.json: %v", err)
	}
}

func TestHandleSessionsListCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("SPAI_SESSION_ID", "")

	writeSession(t, "alpha", 4)
	writeSession(t, "beta", 0)
	writeSession(t, "gamma", 2)

	out := captureStdout(t, handleSessionsListCommand)

	for _, id := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, id) {
			t.Errorf("listing missing session %q:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "4 msgs") {
		t.Errorf("listing missing '4 msgs' for alpha:\n%s", out)
	}
	if !strings.Contains(out, "0 msgs") {
		t.Errorf("listing missing '0 msgs' for beta:\n%s", out)
	}
	if !strings.Contains(out, "2 msgs") {
		t.Errorf("listing missing '2 msgs' for gamma:\n%s", out)
	}
}

func TestHandleHistoryCommandNoHistory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("SPAI_SESSION_ID", "empty-sess")

	// Session with no history.md → empty content early-return branch.
	out := captureStdout(t, func() { handleHistoryCommand(nil) })
	want := "No history for session 'empty-sess'.\n"
	if out != want {
		t.Errorf("handleHistoryCommand output = %q, want %q", out, want)
	}
}

func TestHandleHistoryCommandFallbackPrint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("SPAI_SESSION_ID", "with-hist")

	// Point PATH at an empty dir so neither `less` nor `more` is found; the
	// pager command fails to start and the fmt.Print(content) fallback runs.
	emptyBin := t.TempDir()
	t.Setenv("PATH", emptyBin)

	sessDir := filepath.Join(session.SessionsDir(), "with-hist")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "## exchange\nuser: hello\nassistant: hi there\n"
	if err := os.WriteFile(filepath.Join(sessDir, "history.md"), []byte(content), 0600); err != nil {
		t.Fatalf("write history.md: %v", err)
	}

	out := captureStdout(t, func() { handleHistoryCommand(nil) })
	if !strings.Contains(out, "hello") || !strings.Contains(out, "hi there") {
		t.Errorf("fallback print missing history content:\n%s", out)
	}
}

func TestHandleSessionMaintenanceClear(t *testing.T) {
	dataDirEnv := t.TempDir()
	cfgDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDirEnv)
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("SPAI_SESSION_ID", "clearme")

	// Seed a session with some messages so clear has something to remove.
	writeSession(t, "clearme", 3)

	out := captureStdout(t, func() {
		handleSessionMaintenance("clear", nil)
	})

	if !strings.Contains(out, "Session cleared.") {
		t.Errorf("clear output missing confirmation:\n%s", out)
	}
}
