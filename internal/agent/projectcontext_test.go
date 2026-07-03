package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectContextFoundInCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SPAI.md"), []byte("# My project"), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadProjectContext(dir)
	if got != "# My project" {
		t.Errorf("got %q, want %q", got, "# My project")
	}
}

func TestLoadProjectContextFoundInAncestor(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SPAI.md"), []byte("ancestor instructions"), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadProjectContext(child)
	if got != "ancestor instructions" {
		t.Errorf("got %q, want %q", got, "ancestor instructions")
	}
}

func TestLoadProjectContextNotFound(t *testing.T) {
	// Place a .git at the root so the walk terminates before hitting the real FS root.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "src")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	got := loadProjectContext(child)
	if got != "" {
		t.Errorf("expected empty result when SPAI.md absent, got %q", got)
	}
}

func TestLoadProjectContextStopsAtGitBoundary(t *testing.T) {
	// Layout: root/SPAI.md, root/repo/.git, root/repo/src (start here).
	// The walk must stop at root/repo and never find root/SPAI.md.
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	src := filepath.Join(repo, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SPAI.md"), []byte("outside git root"), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadProjectContext(src)
	if got != "" {
		t.Errorf("walk must stop at .git boundary, got %q", got)
	}
}

func TestLoadProjectContextGitDirButSpaiMdPresent(t *testing.T) {
	// A SPAI.md inside the git root itself must still be found.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SPAI.md"), []byte("repo-level instructions"), 0644); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "pkg")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	got := loadProjectContext(child)
	if got != "repo-level instructions" {
		t.Errorf("got %q, want %q", got, "repo-level instructions")
	}
}

func TestLoadProjectContextSizeCap(t *testing.T) {
	dir := t.TempDir()
	// Write content that exceeds the cap.
	big := strings.Repeat("x", spaiMDMaxBytes+1000)
	if err := os.WriteFile(filepath.Join(dir, "SPAI.md"), []byte(big), 0644); err != nil {
		t.Fatal(err)
	}
	// Place a .git so the walk terminates here.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	got := loadProjectContext(dir)
	if len(got) > spaiMDMaxBytes+100 { // allow for truncation notice
		t.Errorf("result length %d exceeds cap; size cap not enforced", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice, got %q…", got[:80])
	}
}
