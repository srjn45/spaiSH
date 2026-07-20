package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// maxCheckpoints caps the mutation history; the oldest entry (and its blob dir)
// is pruned when a new snapshot pushes the count past this bound.
const maxCheckpoints = 50

// ErrNothingToUndo and ErrNothingToRedo report that the undo/redo cursor is at
// an end of the history and there is nothing to apply.
var (
	ErrNothingToUndo = errors.New("nothing to undo")
	ErrNothingToRedo = errors.New("nothing to redo")
)

// CheckpointStore records file mutations for one (project, session) pair so they
// can be undone and redone. It satisfies tools.Checkpointer. Snapshots live in
// the project tree at <projectRoot>/.spai/checkpoints/<session>/ so the agent
// (writer) and the REPL /undo, /redo commands (readers) resolve the same store
// from the same working directory.
type CheckpointStore struct {
	root string // <projectRoot>/.spai/checkpoints/<session>
}

// checkpointFileState is one file within a checkpoint entry. preExisted /
// postExisted distinguish content restores from file creation/deletion so new
// and deleted files round-trip.
type checkpointFileState struct {
	Path        string `json:"path"` // absolute path of the mutated file
	PreExisted  bool   `json:"preExisted"`
	PostExisted bool   `json:"postExisted"`
}

// checkpointEntry is one mutation — a single tool call, possibly touching
// several files (multi_edit, apply_patch). Blobs are stored positionally: file
// i's pre-image is pre/<i>, its post-image post/<i>.
type checkpointEntry struct {
	ID    string                `json:"id"`
	Files []checkpointFileState `json:"files"`
}

// checkpointIndex is the persisted history and undo/redo cursor. entries is
// append-ordered; cursor counts how many are currently applied, so
// entries[cursor:] are undone (redoable).
type checkpointIndex struct {
	Cursor  int               `json:"cursor"`
	Seq     int               `json:"seq"`
	Entries []checkpointEntry `json:"entries"`
}

// CheckpointResult reports the paths affected by an Undo or Redo, for display.
type CheckpointResult struct {
	Paths []string
}

// projectRoot walks up from workingDir to the nearest ancestor containing a
// .git entry, falling back to workingDir itself. It mirrors the SPAI.md and
// .spai/settings.toml discovery walk so all three agree on the project root.
func projectRoot(workingDir string) string {
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return workingDir
	}
	for {
		if _, statErr := os.Lstat(filepath.Join(abs, ".git")); statErr == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return abs // filesystem root: fall back to the top of the walk
		}
		abs = parent
	}
}

// NewCheckpointStore builds the checkpoint store for sessionID rooted at the
// project containing workingDir. It does not touch disk until a snapshot is
// taken.
func NewCheckpointStore(sessionID, workingDir string) *CheckpointStore {
	if sessionID == "" {
		sessionID = "default"
	}
	root := filepath.Join(projectRoot(workingDir), ".spai", "checkpoints", sessionID)
	return &CheckpointStore{root: root}
}

func (s *CheckpointStore) indexPath() string         { return filepath.Join(s.root, "index.json") }
func (s *CheckpointStore) entryDir(id string) string { return filepath.Join(s.root, id) }

// loadIndex reads index.json, returning a zero index when it is absent.
func (s *CheckpointStore) loadIndex() (*checkpointIndex, error) {
	data, err := os.ReadFile(s.indexPath())
	if os.IsNotExist(err) {
		return &checkpointIndex{}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx checkpointIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// saveIndex persists idx, creating the store root if needed.
func (s *CheckpointStore) saveIndex(idx *checkpointIndex) error {
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.indexPath(), data, 0644)
}

// Snapshot records the pre-mutation bytes of each path (before the caller
// writes). A fresh snapshot truncates the redo tail — entries[cursor:] and their
// dirs — then appends one entry and advances the cursor, giving standard
// undo-stack semantics. History is capped at maxCheckpoints.
func (s *CheckpointStore) Snapshot(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}

	// Truncate the redo tail: a new edit after an undo discards the redoable
	// future.
	for i := idx.Cursor; i < len(idx.Entries); i++ {
		os.RemoveAll(s.entryDir(idx.Entries[i].ID))
	}
	idx.Entries = idx.Entries[:idx.Cursor]

	idx.Seq++
	entry := checkpointEntry{ID: fmt.Sprintf("%04d", idx.Seq)}
	preDir := filepath.Join(s.entryDir(entry.ID), "pre")
	if err := os.MkdirAll(preDir, 0755); err != nil {
		return err
	}
	for i, p := range paths {
		abs := absPath(p)
		fs := checkpointFileState{Path: abs}
		if data, readErr := os.ReadFile(abs); readErr == nil {
			fs.PreExisted = true
			if err := os.WriteFile(filepath.Join(preDir, blobName(i)), data, 0644); err != nil {
				return err
			}
		}
		entry.Files = append(entry.Files, fs)
	}
	idx.Entries = append(idx.Entries, entry)
	idx.Cursor = len(idx.Entries)

	s.prune(idx)
	return s.saveIndex(idx)
}

// prune drops the oldest entries (and their dirs) until the history fits within
// maxCheckpoints, decrementing the cursor for each applied entry removed.
func (s *CheckpointStore) prune(idx *checkpointIndex) {
	for len(idx.Entries) > maxCheckpoints {
		oldest := idx.Entries[0]
		os.RemoveAll(s.entryDir(oldest.ID))
		idx.Entries = idx.Entries[1:]
		if idx.Cursor > 0 {
			idx.Cursor--
		}
	}
}

// Undo reverts the most recently applied checkpoint. It first captures each
// file's current bytes as the post-image (the redo target), then restores the
// pre-image: writing the original bytes back, or deleting a file that did not
// previously exist. Returns ErrNothingToUndo when the cursor is at the start.
func (s *CheckpointStore) Undo() (CheckpointResult, error) {
	idx, err := s.loadIndex()
	if err != nil {
		return CheckpointResult{}, err
	}
	if idx.Cursor == 0 {
		return CheckpointResult{}, ErrNothingToUndo
	}

	entry := &idx.Entries[idx.Cursor-1]
	postDir := filepath.Join(s.entryDir(entry.ID), "post")
	if err := os.MkdirAll(postDir, 0755); err != nil {
		return CheckpointResult{}, err
	}

	var paths []string
	for i := range entry.Files {
		f := &entry.Files[i]
		// Capture the current on-disk state as the post-image so Redo can
		// re-apply it.
		if data, readErr := os.ReadFile(f.Path); readErr == nil {
			f.PostExisted = true
			if err := os.WriteFile(filepath.Join(postDir, blobName(i)), data, 0644); err != nil {
				return CheckpointResult{}, err
			}
		} else {
			f.PostExisted = false
		}
		// Restore the pre-image.
		if f.PreExisted {
			data, err := os.ReadFile(filepath.Join(s.entryDir(entry.ID), "pre", blobName(i)))
			if err != nil {
				return CheckpointResult{}, err
			}
			if err := writeFileRestoring(f.Path, data); err != nil {
				return CheckpointResult{}, err
			}
		} else {
			os.Remove(f.Path)
		}
		paths = append(paths, f.Path)
	}
	idx.Cursor--
	if err := s.saveIndex(idx); err != nil {
		return CheckpointResult{}, err
	}
	return CheckpointResult{Paths: paths}, nil
}

// Redo re-applies the next undone checkpoint, restoring each file's post-image:
// writing the mutated bytes back, or deleting a file the mutation had removed.
// Returns ErrNothingToRedo when the cursor is at the head.
func (s *CheckpointStore) Redo() (CheckpointResult, error) {
	idx, err := s.loadIndex()
	if err != nil {
		return CheckpointResult{}, err
	}
	if idx.Cursor >= len(idx.Entries) {
		return CheckpointResult{}, ErrNothingToRedo
	}

	entry := &idx.Entries[idx.Cursor]
	postDir := filepath.Join(s.entryDir(entry.ID), "post")

	var paths []string
	for i := range entry.Files {
		f := &entry.Files[i]
		if f.PostExisted {
			data, err := os.ReadFile(filepath.Join(postDir, blobName(i)))
			if err != nil {
				return CheckpointResult{}, err
			}
			if err := writeFileRestoring(f.Path, data); err != nil {
				return CheckpointResult{}, err
			}
		} else {
			os.Remove(f.Path)
		}
		paths = append(paths, f.Path)
	}
	idx.Cursor++
	if err := s.saveIndex(idx); err != nil {
		return CheckpointResult{}, err
	}
	return CheckpointResult{Paths: paths}, nil
}

// Remove deletes the store's on-disk directory. Called when a session is
// cleared so stale checkpoints do not outlive the conversation.
func (s *CheckpointStore) Remove() error {
	return os.RemoveAll(s.root)
}

// writeFileRestoring writes data to path, creating parent directories as needed
// so a restored file lands even if its directory was since removed.
func writeFileRestoring(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0644)
}

// absPath resolves p against the process working directory. The mutating tools
// run in that directory, so resolving here keeps the stored path stable even if
// the cwd later changes within the session.
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// blobName is the positional file name for the i-th snapshotted file's blob.
func blobName(i int) string { return fmt.Sprintf("%d", i) }
