package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runMultiEdit(t *testing.T, args multiEditArgs) (string, error) {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return MultiEdit{}.Run(context.Background(), json.RawMessage(b))
}

func TestMultiEditSingleMatch(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	p := write("a.txt", "hello world")
	out, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.txt",
		Pattern:     "world",
		Replacement: "there",
		Path:        dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "1 substitution") {
		t.Errorf("expected substitution count in output, got %q", out)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hello there" {
		t.Errorf("file content = %q, want %q", got, "hello there")
	}
}

func TestMultiEditMultipleFilesAndMatches(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	pa := write("a.go", "foo foo foo")
	pb := write("b.go", "foo bar")
	write("c.txt", "foo ignored") // not matched by *.go

	out, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.go",
		Pattern:     "foo",
		Replacement: "baz",
		Path:        dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// a.go has 3 matches, b.go has 1 → total 4
	if !strings.Contains(out, "4 substitution") {
		t.Errorf("expected 4 substitutions in output, got %q", out)
	}
	if !strings.Contains(out, "2 file") {
		t.Errorf("expected 2 files in output, got %q", out)
	}
	gotA, _ := os.ReadFile(pa)
	if string(gotA) != "baz baz baz" {
		t.Errorf("a.go = %q, want %q", gotA, "baz baz baz")
	}
	gotB, _ := os.ReadFile(pb)
	if string(gotB) != "baz bar" {
		t.Errorf("b.go = %q, want %q", gotB, "baz bar")
	}
	gotC, _ := os.ReadFile(filepath.Join(dir, "c.txt"))
	if string(gotC) != "foo ignored" {
		t.Errorf("c.txt must be untouched, got %q", gotC)
	}
}

func TestMultiEditNoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world"), 0644)

	out, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.txt",
		Pattern:     "xyz",
		Replacement: "abc",
		Path:        dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "no matches") {
		t.Errorf("expected no-match notice, got %q", out)
	}
}

func TestMultiEditNoFilesMatchGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)

	out, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.go",
		Pattern:     "hello",
		Replacement: "bye",
		Path:        dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "no files matched") {
		t.Errorf("expected no-files-matched notice, got %q", out)
	}
}

func TestMultiEditInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)

	_, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.txt",
		Pattern:     "[invalid",
		Replacement: "x",
		Path:        dir,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected invalid-regex error, got %v", err)
	}
}

func TestMultiEditDryRun(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("foo bar foo"), 0644)

	out, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.txt",
		Pattern:     "foo",
		Replacement: "baz",
		Path:        dir,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run notice in output, got %q", out)
	}
	if !strings.Contains(out, "would edit") {
		t.Errorf("expected 'would edit' in output, got %q", out)
	}
	// File must be untouched.
	got, _ := os.ReadFile(p)
	if string(got) != "foo bar foo" {
		t.Errorf("dry_run must not write; file is now %q", got)
	}
}

func TestMultiEditCaptureGroupReplacement(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	os.WriteFile(p, []byte("func Foo() {}\nfunc Bar() {}\n"), 0644)

	out, err := runMultiEdit(t, multiEditArgs{
		Glob:        "*.go",
		Pattern:     `func (\w+)\(\)`,
		Replacement: "func ${1}Renamed()",
		Path:        dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "2 substitution") {
		t.Errorf("expected 2 substitutions, got %q", out)
	}
	got, _ := os.ReadFile(p)
	want := "func FooRenamed() {}\nfunc BarRenamed() {}\n"
	if string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}
