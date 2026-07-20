package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"spaish/internal/session"
)

// gitProjectDir returns a temp dir marked with a .git entry so the checkpoint
// store's projectRoot resolves to it.
func gitProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFileInput(t *testing.T, path, content string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWriteFileCheckpointed verifies that with a checkpointer installed in ctx,
// write_file snapshots the original bytes so the store's Undo restores them.
func TestWriteFileCheckpointed(t *testing.T) {
	dir := gitProjectDir(t)
	f := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(f, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	store := session.NewCheckpointStore("wf", dir)
	ctx := WithCheckpointer(context.Background(), store)

	if _, err := (WriteFile{}).Run(ctx, writeFileInput(t, f, "overwritten")); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if got, _ := os.ReadFile(f); string(got) != "overwritten" {
		t.Fatalf("file after write = %q, want overwritten", got)
	}

	if _, err := store.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got, _ := os.ReadFile(f); string(got) != "original" {
		t.Errorf("file after Undo = %q, want original", got)
	}
}

// TestEditFileCheckpointed verifies the same for edit_file.
func TestEditFileCheckpointed(t *testing.T) {
	dir := gitProjectDir(t)
	f := filepath.Join(dir, "code.go")
	if err := os.WriteFile(f, []byte("package a\n\nvar X = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := session.NewCheckpointStore("ef", dir)
	ctx := WithCheckpointer(context.Background(), store)

	input, _ := json.Marshal(map[string]any{"path": f, "old_string": "X = 1", "new_string": "X = 2"})
	if _, err := (EditFile{}).Run(ctx, input); err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	if got, _ := os.ReadFile(f); string(got) != "package a\n\nvar X = 2\n" {
		t.Fatalf("edit not applied, got %q", got)
	}

	if _, err := store.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got, _ := os.ReadFile(f); string(got) != "package a\n\nvar X = 1\n" {
		t.Errorf("edit_file Undo did not restore original, got %q", got)
	}
}

// TestWriteFileNoCheckpointer verifies that without a checkpointer in ctx, the
// write happens normally and no .spai/checkpoints directory is created.
func TestWriteFileNoCheckpointer(t *testing.T) {
	dir := gitProjectDir(t)
	f := filepath.Join(dir, "plain.txt")

	if _, err := (WriteFile{}).Run(context.Background(), writeFileInput(t, f, "hello")); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if got, _ := os.ReadFile(f); string(got) != "hello" {
		t.Errorf("file = %q, want hello", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".spai", "checkpoints")); !os.IsNotExist(err) {
		t.Errorf("no checkpointer should create no checkpoints dir, stat err = %v", err)
	}
}
