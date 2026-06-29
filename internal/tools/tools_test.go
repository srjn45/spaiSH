package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, tool Tool, input string) string {
	t.Helper()
	out, err := tool.Run(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("%s.Run: %v", tool.Name(), err)
	}
	return out
}

func TestWriteReadEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")

	run(t, WriteFile{}, `{"path":"`+path+`","content":"hello world"}`)
	if got := run(t, ReadFile{}, `{"path":"`+path+`"}`); got != "hello world" {
		t.Errorf("read got %q", got)
	}

	run(t, EditFile{}, `{"path":"`+path+`","old_string":"world","new_string":"there"}`)
	if got := run(t, ReadFile{}, `{"path":"`+path+`"}`); got != "hello there" {
		t.Errorf("after edit got %q", got)
	}
}

func TestPreviewEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	// edit_file: preview computes new content without writing.
	p, oldC, newC, ok := PreviewEdit("edit_file", json.RawMessage(`{"path":"`+path+`","old_string":"world","new_string":"there"}`))
	if !ok {
		t.Fatal("expected ok for edit_file preview")
	}
	if p != path || oldC != "hello world" || newC != "hello there" {
		t.Errorf("edit preview got path=%q old=%q new=%q", p, oldC, newC)
	}
	// Disk must be untouched by the preview.
	if data, _ := os.ReadFile(path); string(data) != "hello world" {
		t.Errorf("preview must not write; file is now %q", data)
	}

	// write_file overwriting an existing file: old is current content.
	_, oldC, newC, ok = PreviewEdit("write_file", json.RawMessage(`{"path":"`+path+`","content":"brand new"}`))
	if !ok || oldC != "hello world" || newC != "brand new" {
		t.Errorf("write preview got ok=%v old=%q new=%q", ok, oldC, newC)
	}

	// write_file to a new path: old is empty (renders as all-additions).
	newPath := filepath.Join(dir, "new.txt")
	_, oldC, newC, ok = PreviewEdit("write_file", json.RawMessage(`{"path":"`+newPath+`","content":"fresh"}`))
	if !ok || oldC != "" || newC != "fresh" {
		t.Errorf("new-file write preview got ok=%v old=%q new=%q", ok, oldC, newC)
	}

	// Non-editing tool: no preview.
	if _, _, _, ok := PreviewEdit("read_file", json.RawMessage(`{"path":"`+path+`"}`)); ok {
		t.Error("expected ok=false for non-editing tool")
	}

	// edit_file with a missing old_string: preview gracefully declines.
	if _, _, _, ok := PreviewEdit("edit_file", json.RawMessage(`{"path":"`+path+`","old_string":"nope","new_string":"x"}`)); ok {
		t.Error("expected ok=false when old_string is absent")
	}
}

func TestEditFileNonUnique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("a a a"), 0644)
	_, err := EditFile{}.Run(context.Background(), json.RawMessage(`{"path":"`+path+`","old_string":"a","new_string":"b"}`))
	if err == nil {
		t.Error("expected error for non-unique old_string without replace_all")
	}
}

func TestBashRunsAndReportsExit(t *testing.T) {
	out := run(t, Bash{}, `{"command":"echo hi"}`)
	if !strings.Contains(out, "hi") {
		t.Errorf("bash output = %q", out)
	}
	fail := run(t, Bash{}, `{"command":"exit 3"}`)
	if !strings.Contains(fail, "exit code: 3") {
		t.Errorf("expected exit code in output, got %q", fail)
	}
}

func TestGlobAndGrep(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nfunc Foo() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing here"), 0644)

	globOut := run(t, Glob{}, `{"pattern":"*.go","path":"`+dir+`"}`)
	if !strings.Contains(globOut, "a.go") || strings.Contains(globOut, "b.txt") {
		t.Errorf("glob got %q", globOut)
	}

	grepOut := run(t, Grep{}, `{"pattern":"func \\w+","path":"`+dir+`"}`)
	if !strings.Contains(grepOut, "Foo") {
		t.Errorf("grep got %q", grepOut)
	}
}

func TestRegistrySpecs(t *testing.T) {
	r := DefaultRegistry()
	specs := r.Specs()
	if len(specs) != 7 {
		t.Errorf("expected 7 default tools, got %d", len(specs))
	}
	if _, ok := r.Get("bash"); !ok {
		t.Error("bash tool missing from default registry")
	}
}
