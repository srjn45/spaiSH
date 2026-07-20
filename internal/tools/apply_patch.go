package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ApplyPatch applies a structured, multi-file patch.
//
// Patch format (line-oriented, deterministic):
//
//	*** Begin Patch        (optional wrapper)
//	*** Add File: path/to/new.txt
//	+first line of new file
//	+second line
//	*** Update File: path/to/existing.txt
//	@@
//	 unchanged context line
//	-line to remove
//	+line to add
//	 another context line
//	*** Delete File: path/to/old.txt
//	*** End Patch          (optional wrapper)
//
// Each file section starts with one of the "*** Add/Update/Delete File:" headers.
//   - Add File:    following lines are the new file content, each prefixed with '+'.
//   - Update File: following lines are one or more hunks. Each hunk begins with a
//     '@@' marker line, then context lines (prefixed with a space), removals
//     ('-'), and additions ('+'). Context and removal lines must match the
//     current file exactly; otherwise the whole patch is rejected.
//   - Delete File: removes the file; no body.
type ApplyPatch struct{}

func (ApplyPatch) Name() string { return "apply_patch" }

func (ApplyPatch) Description() string {
	return strings.TrimSpace(`
Apply a structured patch that can create, update, and delete multiple files in
one call. The patch is validated against the current files before anything is
written; if any hunk fails to match, no file is modified.

Format:
*** Begin Patch          (optional wrapper line)
*** Add File: path        -> following '+'-prefixed lines are the new file's content
+new line 1
+new line 2
*** Update File: path     -> one or more hunks follow
@@                        -> hunk marker (text after @@ is ignored)
 context line (leading space, must match the file)
-removed line (must match the file)
+added line
 trailing context line
*** Delete File: path     -> deletes the file (no body)
*** End Patch            (optional wrapper line)

Rules:
- Context lines start with a single space; removed lines with '-'; added lines
  with '+'. Include a few context lines so hunks anchor unambiguously.
- Context and removed lines must match the existing file exactly (whitespace
  included), or the patch is rejected with no changes written.
- Add File fails if the file already exists; Update/Delete fail if it does not.`)
}

func (ApplyPatch) Schema() map[string]any {
	return objectSchema(map[string]any{
		"patch": strProp("The structured patch text (see the tool description for the format)."),
	}, "patch")
}

func (ApplyPatch) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.Patch) == "" {
		return "", fmt.Errorf("patch is required")
	}

	files, err := parsePatch(args.Patch)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("patch contains no file sections")
	}

	// Pass 1: validate everything and compute the resulting content. Nothing is
	// written until all sections succeed, so a mismatch never leaves a partial
	// write behind.
	type plan struct {
		op      patchOp
		path    string
		content string
	}
	plans := make([]plan, 0, len(files))
	for _, fp := range files {
		if fp.path == "" {
			return "", fmt.Errorf("patch file section is missing a path")
		}
		switch fp.op {
		case opAdd:
			if _, err := os.Stat(fp.path); err == nil {
				return "", fmt.Errorf("cannot add %s: file already exists", fp.path)
			}
			plans = append(plans, plan{op: opAdd, path: fp.path, content: strings.Join(fp.addLines, "\n")})
		case opUpdate:
			data, err := os.ReadFile(fp.path)
			if err != nil {
				return "", fmt.Errorf("cannot update %s: %w", fp.path, err)
			}
			newContent, err := applyHunks(string(data), fp.hunks)
			if err != nil {
				return "", fmt.Errorf("cannot update %s: %w", fp.path, err)
			}
			plans = append(plans, plan{op: opUpdate, path: fp.path, content: newContent})
		case opDelete:
			if _, err := os.Stat(fp.path); err != nil {
				return "", fmt.Errorf("cannot delete %s: %w", fp.path, err)
			}
			plans = append(plans, plan{op: opDelete, path: fp.path})
		}
	}

	// Snapshot every file the plan touches (add/update/delete) as one atomic
	// checkpoint before Pass 2 writes, so /undo reverts the whole patch at once.
	paths := make([]string, len(plans))
	for i, p := range plans {
		paths[i] = p.path
	}
	snapshot(ctx, paths...)

	// Pass 2: execute the validated plan.
	var summary []string
	for _, p := range plans {
		switch p.op {
		case opAdd:
			if dir := filepath.Dir(p.path); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return "", err
				}
			}
			if err := os.WriteFile(p.path, []byte(p.content), 0644); err != nil {
				return "", err
			}
			summary = append(summary, "added "+p.path)
		case opUpdate:
			mode := os.FileMode(0644)
			if info, err := os.Stat(p.path); err == nil {
				mode = info.Mode()
			}
			if err := os.WriteFile(p.path, []byte(p.content), mode); err != nil {
				return "", err
			}
			summary = append(summary, "updated "+p.path)
		case opDelete:
			if err := os.Remove(p.path); err != nil {
				return "", err
			}
			summary = append(summary, "deleted "+p.path)
		}
	}
	return "applied patch:\n" + strings.Join(summary, "\n"), nil
}

// patchOp is the kind of change for a single file.
type patchOp int

const (
	opUpdate patchOp = iota
	opAdd
	opDelete
)

// filePatch is a parsed change to one file.
type filePatch struct {
	op       patchOp
	path     string
	addLines []string // for opAdd: literal content lines
	hunks    []hunk   // for opUpdate
}

// hunk is one contiguous change region within an updated file.
type hunk struct {
	lines []hunkLine
}

// hunkLine is a single line within a hunk; kind is ' ' (context), '-' (remove),
// or '+' (add).
type hunkLine struct {
	kind byte
	text string
}

// parsePatch parses the structured patch text into per-file changes.
func parsePatch(patch string) ([]filePatch, error) {
	lines := strings.Split(patch, "\n")
	var files []filePatch
	var cur *filePatch
	var curHunk *hunk

	flushHunk := func() {
		if cur != nil && curHunk != nil {
			cur.hunks = append(cur.hunks, *curHunk)
			curHunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for _, raw := range lines {
		switch {
		case strings.HasPrefix(raw, "*** Begin Patch"), strings.HasPrefix(raw, "*** End Patch"):
			continue
		case strings.HasPrefix(raw, "*** Add File:"):
			flushFile()
			cur = &filePatch{op: opAdd, path: strings.TrimSpace(strings.TrimPrefix(raw, "*** Add File:"))}
			continue
		case strings.HasPrefix(raw, "*** Update File:"):
			flushFile()
			cur = &filePatch{op: opUpdate, path: strings.TrimSpace(strings.TrimPrefix(raw, "*** Update File:"))}
			continue
		case strings.HasPrefix(raw, "*** Delete File:"):
			flushFile()
			cur = &filePatch{op: opDelete, path: strings.TrimSpace(strings.TrimPrefix(raw, "*** Delete File:"))}
			continue
		}

		if cur == nil {
			// Preamble before the first section; ignore.
			continue
		}

		switch cur.op {
		case opAdd:
			line := raw
			if strings.HasPrefix(line, "+") {
				line = line[1:]
			}
			cur.addLines = append(cur.addLines, line)
		case opUpdate:
			if strings.HasPrefix(raw, "@@") {
				flushHunk()
				curHunk = &hunk{}
				continue
			}
			if curHunk == nil {
				curHunk = &hunk{}
			}
			switch {
			case raw == "":
				curHunk.lines = append(curHunk.lines, hunkLine{kind: ' ', text: ""})
			case raw[0] == ' ' || raw[0] == '+' || raw[0] == '-':
				curHunk.lines = append(curHunk.lines, hunkLine{kind: raw[0], text: raw[1:]})
			default:
				return nil, fmt.Errorf("invalid patch line in update of %s: %q", cur.path, raw)
			}
		case opDelete:
			if strings.TrimSpace(raw) != "" {
				return nil, fmt.Errorf("unexpected content after delete of %s: %q", cur.path, raw)
			}
		}
	}
	flushFile()
	return files, nil
}

// applyHunks applies the hunks to old content and returns the new content. It is
// pure: it never touches the filesystem. An error is returned (and no content)
// when any hunk's context/removed lines do not match the source in order.
func applyHunks(old string, hunks []hunk) (string, error) {
	src := strings.Split(old, "\n")
	var out []string
	cursor := 0

	for hi, h := range hunks {
		var search, replace []string
		for _, l := range h.lines {
			switch l.kind {
			case ' ':
				search = append(search, l.text)
				replace = append(replace, l.text)
			case '-':
				search = append(search, l.text)
			case '+':
				replace = append(replace, l.text)
			}
		}
		idx := indexOfBlock(src, search, cursor)
		if idx < 0 {
			return "", fmt.Errorf("hunk %d does not match the file (context or removed lines not found)", hi+1)
		}
		out = append(out, src[cursor:idx]...)
		out = append(out, replace...)
		cursor = idx + len(search)
	}
	out = append(out, src[cursor:]...)
	return strings.Join(out, "\n"), nil
}

// indexOfBlock returns the first index >= from at which block occurs contiguously
// in lines, or -1. An empty block matches at from (pure insertion).
func indexOfBlock(lines, block []string, from int) int {
	if len(block) == 0 {
		return from
	}
	for i := from; i+len(block) <= len(lines); i++ {
		match := true
		for j := range block {
			if lines[i+j] != block[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
