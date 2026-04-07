package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaios/internal/session"
)

func TestPinnedPathUsesXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	p := session.PinnedPath()
	expected := filepath.Join(dir, "spaios", "pinned_session")
	if p != expected {
		t.Errorf("expected %q, got %q", expected, p)
	}
}

func TestWriteAndReadPinned(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	if err := session.WritePinned("infra"); err != nil {
		t.Fatalf("WritePinned() error: %v", err)
	}

	id := session.ReadPinned()
	if id != "infra" {
		t.Errorf("expected 'infra', got %q", id)
	}
}

func TestReadPinnedReturnsEmptyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	id := session.ReadPinned()
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestClearPinned(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	session.WritePinned("infra")
	if err := session.ClearPinned(); err != nil {
		t.Fatalf("ClearPinned() error: %v", err)
	}

	id := session.ReadPinned()
	if id != "" {
		t.Errorf("expected empty after clear, got %q", id)
	}
}

func TestClearPinnedNoFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	if err := session.ClearPinned(); err != nil {
		t.Errorf("ClearPinned() on missing file should not error: %v", err)
	}
}

func TestWritePinnedCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	// spaios/ subdir does not exist yet
	if err := session.WritePinned("work"); err != nil {
		t.Fatalf("WritePinned() should create parent dirs: %v", err)
	}

	if _, err := os.Stat(session.PinnedPath()); err != nil {
		t.Errorf("pinned_session file not found: %v", err)
	}
}

func TestWritePinnedFilePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	session.WritePinned("work")
	info, err := os.Stat(session.PinnedPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600, got %v", info.Mode().Perm())
	}
}
