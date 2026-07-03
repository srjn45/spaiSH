package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	// Assert the baseline set is present rather than an exact count: several
	// tools are added concurrently at this same registration point, so a
	// hard-coded number is a needless merge hotspot.
	if len(specs) < 13 {
		t.Errorf("expected at least 13 default tools, got %d", len(specs))
	}
	if _, ok := r.Get("bash"); !ok {
		t.Error("bash tool missing from default registry")
	}
	for _, name := range []string{"web_fetch", "apply_patch", "code_exec", "read_image"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("%s tool missing from default registry", name)
		}
	}
}

func TestWebFetchHTMLToText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected a User-Agent header")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>T</title><style>.x{color:red}</style></head>` +
			`<body><h1>Hello</h1><script>var x=1;</script><p>World &amp; more</p></body></html>`))
	}))
	defer srv.Close()

	out := run(t, WebFetch{}, `{"url":"`+srv.URL+`"}`)
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "World & more") {
		t.Errorf("expected readable text, got %q", out)
	}
	if strings.Contains(out, "var x=1") || strings.Contains(out, "color:red") {
		t.Errorf("script/style content must be stripped, got %q", out)
	}
	if strings.Contains(out, "<p>") || strings.Contains(out, "<h1>") {
		t.Errorf("tags must be stripped, got %q", out)
	}
}

func TestWebFetchNonHTMLPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"a":1,"b":"<x>"}`))
	}))
	defer srv.Close()

	out := run(t, WebFetch{}, `{"url":"`+srv.URL+`"}`)
	if !strings.Contains(out, `{"a":1,"b":"<x>"}`) {
		t.Errorf("non-HTML content must pass through unchanged, got %q", out)
	}
}

func TestWebFetchByteCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("a", 5000)))
	}))
	defer srv.Close()

	out := run(t, WebFetch{}, `{"url":"`+srv.URL+`","max_bytes":100}`)
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation notice, got %q", out)
	}
	// Body portion (after the status header) should be capped near max_bytes.
	if len(out) > 100+512 {
		t.Errorf("output not capped: len=%d", len(out))
	}
}

func TestWebFetchRejectsNonHTTPScheme(t *testing.T) {
	_, err := WebFetch{}.Run(context.Background(), json.RawMessage(`{"url":"file:///etc/passwd"}`))
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Errorf("expected scheme rejection, got err=%v", err)
	}
}

func TestWebFetchContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WebFetch{}.Run(ctx, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestApplyPatchModify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0644)

	patch := "*** Update File: " + path + "\n@@\n one\n-two\n+TWO\n three\n"
	run(t, ApplyPatch{}, mustJSON(patch))

	got, _ := os.ReadFile(path)
	if string(got) != "one\nTWO\nthree\n" {
		t.Errorf("modify got %q", got)
	}
}

func TestApplyPatchCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "new.txt")
	patch := "*** Add File: " + path + "\n+line1\n+line2\n"
	run(t, ApplyPatch{}, mustJSON(patch))

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(got) != "line1\nline2\n" {
		t.Errorf("create got %q", got)
	}
}

func TestApplyPatchDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	os.WriteFile(path, []byte("x"), 0644)

	patch := "*** Delete File: " + path + "\n"
	run(t, ApplyPatch{}, mustJSON(patch))

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file deleted, stat err=%v", err)
	}
}

func TestApplyPatchContextMismatchNoPartialWrite(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	bad := filepath.Join(dir, "bad.txt")
	os.WriteFile(good, []byte("a\nb\n"), 0644)
	os.WriteFile(bad, []byte("hello\nworld\n"), 0644)

	// First section is valid; second section's context does not match, so the
	// entire patch must be rejected and the first file left untouched.
	patch := "*** Update File: " + good + "\n@@\n a\n-b\n+B\n" +
		"*** Update File: " + bad + "\n@@\n hello\n-NOPE\n+x\n"
	_, err := ApplyPatch{}.Run(context.Background(), json.RawMessage(mustJSON(patch)))
	if err == nil {
		t.Fatal("expected context-mismatch error")
	}
	if got, _ := os.ReadFile(good); string(got) != "a\nb\n" {
		t.Errorf("first file must be untouched on failure, got %q", got)
	}
}

func TestApplyHunksPure(t *testing.T) {
	out, err := applyHunks("one\ntwo\nthree\n", []hunk{{lines: []hunkLine{
		{kind: ' ', text: "one"},
		{kind: '-', text: "two"},
		{kind: '+', text: "TWO"},
		{kind: ' ', text: "three"},
	}}})
	if err != nil {
		t.Fatalf("applyHunks: %v", err)
	}
	if out != "one\nTWO\nthree\n" {
		t.Errorf("applyHunks got %q", out)
	}

	if _, err := applyHunks("one\ntwo\n", []hunk{{lines: []hunkLine{
		{kind: ' ', text: "nope"},
		{kind: '-', text: "two"},
	}}}); err == nil {
		t.Error("expected mismatch error from applyHunks")
	}
}

// mustJSON wraps a patch string into the apply_patch JSON input.
func mustJSON(patch string) string {
	b, err := json.Marshal(struct {
		Patch string `json:"patch"`
	}{patch})
	if err != nil {
		panic(err)
	}
	return string(b)
}
