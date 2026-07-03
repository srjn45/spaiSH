package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGoModuleName(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"module example.com/mymod\n\ngo 1.21\n", "example.com/mymod"},
		{"module spaish\n", "spaish"},
		{"// comment\nmodule foo/bar\n", "foo/bar"},
		{"", ""},
		{"go 1.21\n", ""},
	}
	for _, tc := range cases {
		if got := parseGoModuleName(tc.content); got != tc.want {
			t.Errorf("parseGoModuleName(%q) = %q, want %q", tc.content, got, tc.want)
		}
	}
}

func TestFirstParagraph(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"# Title\n\nSome text here.\nMore on same paragraph.\n\n## Section\n", "Some text here. More on same paragraph."},
		{"# Title\nDirect paragraph.\n", "Direct paragraph."},
		{"", ""},
		{"# Only headings\n## Sub\n", ""},
		{"No heading.\nSecond line.\n", "No heading. Second line."},
	}
	for _, tc := range cases {
		if got := firstParagraph(tc.input); got != tc.want {
			t.Errorf("firstParagraph(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestGatherProjectFacts_GoModule(t *testing.T) {
	dir := t.TempDir()

	// Write a go.mod with a known module name.
	gomod := "module example.com/testproject\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a README with a prose paragraph.
	readme := "# TestProject\n\nThis is the test project description.\n\n## Usage\n\nRun it.\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a couple of directories.
	for _, d := range []string{"cmd", "internal"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	f := gatherProjectFacts(dir)

	if f.ModuleName != "example.com/testproject" {
		t.Errorf("ModuleName = %q, want %q", f.ModuleName, "example.com/testproject")
	}
	if f.Language != "Go" {
		t.Errorf("Language = %q, want %q", f.Language, "Go")
	}
	if f.ReadmeIntro != "This is the test project description." {
		t.Errorf("ReadmeIntro = %q", f.ReadmeIntro)
	}
	if len(f.TopDirs) != 2 || f.TopDirs[0] != "cmd" || f.TopDirs[1] != "internal" {
		t.Errorf("TopDirs = %v, want [cmd internal]", f.TopDirs)
	}
}

func TestGatherProjectFacts_NoManifest(t *testing.T) {
	dir := t.TempDir()
	f := gatherProjectFacts(dir)
	if f.Language != "" || f.ModuleName != "" {
		t.Errorf("empty dir should yield no language or module, got lang=%q mod=%q", f.Language, f.ModuleName)
	}
}

func TestBuildInitContent_containsModuleName(t *testing.T) {
	f := projectFacts{
		ModuleName: "example.com/mymod",
		Language:   "Go",
		TopDirs:    []string{"cmd", "internal"},
	}
	content := buildInitContent(f)

	if !strings.Contains(content, "example.com/mymod") {
		t.Error("content should contain module name")
	}
	if !strings.Contains(content, "Go") {
		t.Error("content should mention the language")
	}
	if !strings.Contains(content, "go build") {
		t.Error("content should include Go build instructions")
	}
	if !strings.Contains(content, "`cmd/`") || !strings.Contains(content, "`internal/`") {
		t.Error("content should list top-level directories")
	}
}

func TestBuildInitContent_readmeIntroUsed(t *testing.T) {
	f := projectFacts{
		Language:    "Go",
		ReadmeIntro: "This project does amazing things.",
	}
	content := buildInitContent(f)
	if !strings.Contains(content, "This project does amazing things.") {
		t.Error("content should include the README intro")
	}
	if strings.Contains(content, "Describe what this project does") {
		t.Error("placeholder comment should be absent when README intro is present")
	}
}

func TestCanInit_refusesWhenExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SPAI.md"), []byte("existing content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if canInit(dir) {
		t.Error("canInit should return false when SPAI.md already exists")
	}
}

func TestCanInit_allowsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if !canInit(dir) {
		t.Error("canInit should return true when SPAI.md does not exist")
	}
}
