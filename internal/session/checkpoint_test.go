package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// newTestStore builds a checkpoint store rooted at a temp project (with a .git
// marker so projectRoot resolves to it) and returns the store plus the project
// dir. Tests operate on files inside that dir.
func newTestStore(t *testing.T) (*CheckpointStore, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return NewCheckpointStore("sess1", dir), dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckpointEditRoundTrip(t *testing.T) {
	store, dir := newTestStore(t)
	f := filepath.Join(dir, "foo.txt")
	writeFile(t, f, "original")

	// Snapshot before mutating, then overwrite.
	if err := store.Snapshot(f); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	writeFile(t, f, "edited")

	if _, err := store.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := readFile(t, f); got != "original" {
		t.Errorf("after Undo = %q, want %q", got, "original")
	}

	if _, err := store.Redo(); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if got := readFile(t, f); got != "edited" {
		t.Errorf("after Redo = %q, want %q", got, "edited")
	}
}

func TestCheckpointNewFileRoundTrip(t *testing.T) {
	store, dir := newTestStore(t)
	f := filepath.Join(dir, "new.txt")

	// Snapshot a path that does not yet exist, then create it.
	if err := store.Snapshot(f); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	writeFile(t, f, "created")

	if _, err := store.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("Undo should delete a newly-created file, stat err = %v", err)
	}

	if _, err := store.Redo(); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if got := readFile(t, f); got != "created" {
		t.Errorf("Redo should recreate the file as %q, got %q", "created", got)
	}
}

func TestCheckpointDeletedFileRoundTrip(t *testing.T) {
	store, dir := newTestStore(t)
	f := filepath.Join(dir, "gone.txt")
	writeFile(t, f, "doomed")

	// Snapshot the existing file, then delete it (mimicking apply_patch delete).
	if err := store.Snapshot(f); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := readFile(t, f); got != "doomed" {
		t.Errorf("Undo should recreate deleted file as %q, got %q", "doomed", got)
	}

	if _, err := store.Redo(); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("Redo should re-delete the file, stat err = %v", err)
	}
}

func TestCheckpointMultiFileEntry(t *testing.T) {
	store, dir := newTestStore(t)
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	writeFile(t, a, "a0")
	writeFile(t, b, "b0")

	// One snapshot covering both files → one undo restores both.
	if err := store.Snapshot(a, b); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	writeFile(t, a, "a1")
	writeFile(t, b, "b1")

	res, err := store.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(res.Paths) != 2 {
		t.Errorf("Undo should report 2 paths, got %d", len(res.Paths))
	}
	if readFile(t, a) != "a0" || readFile(t, b) != "b0" {
		t.Errorf("Undo should restore both files, got a=%q b=%q", readFile(t, a), readFile(t, b))
	}
}

func TestCheckpointCursorNoOps(t *testing.T) {
	store, _ := newTestStore(t)

	if _, err := store.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("Undo on empty history = %v, want ErrNothingToUndo", err)
	}
	if _, err := store.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("Redo at head = %v, want ErrNothingToRedo", err)
	}
}

func TestCheckpointNewSnapshotTruncatesRedoTail(t *testing.T) {
	store, dir := newTestStore(t)
	f := filepath.Join(dir, "f.txt")
	writeFile(t, f, "v0")

	if err := store.Snapshot(f); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f, "v1")

	// Undo back to v0, leaving v1 as a redoable future.
	if _, err := store.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f); got != "v0" {
		t.Fatalf("precondition: expected v0, got %q", got)
	}

	// A fresh snapshot+edit must discard the redo tail.
	if err := store.Snapshot(f); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f, "v2")

	if _, err := store.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("Redo after a new snapshot = %v, want ErrNothingToRedo (tail truncated)", err)
	}
	// Undo should now restore v0 (the pre-image of the newest edit), not v1.
	if _, err := store.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f); got != "v0" {
		t.Errorf("Undo after truncation = %q, want v0", got)
	}
}

func TestCheckpointRetentionPrune(t *testing.T) {
	store, dir := newTestStore(t)
	f := filepath.Join(dir, "f.txt")

	// Push more than maxCheckpoints mutations; the oldest entry + dir should be
	// pruned so history stays capped.
	total := maxCheckpoints + 5
	for i := 0; i < total; i++ {
		writeFile(t, f, fmt.Sprintf("v%d", i))
		if err := store.Snapshot(f); err != nil {
			t.Fatalf("Snapshot %d: %v", i, err)
		}
	}
	writeFile(t, f, "final")

	idx, err := store.loadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != maxCheckpoints {
		t.Errorf("entries = %d, want capped at %d", len(idx.Entries), maxCheckpoints)
	}
	// The first snapshot's dir (0001) must have been removed.
	if _, err := os.Stat(store.entryDir("0001")); !os.IsNotExist(err) {
		t.Errorf("oldest checkpoint dir should be pruned, stat err = %v", err)
	}
}
