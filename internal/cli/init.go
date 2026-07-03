package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// projectFacts holds information gathered from the project directory to draft SPAI.md.
type projectFacts struct {
	ModuleName  string // Go module name from go.mod, or ""
	Language    string // detected primary language, or ""
	TopDirs     []string
	ReadmeIntro string // first prose paragraph of README.md, or ""
}

// gatherProjectFacts collects context from dir without network calls or LLM invocations.
func gatherProjectFacts(dir string) projectFacts {
	var f projectFacts

	switch {
	case fileExists(filepath.Join(dir, "go.mod")):
		f.Language = "Go"
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			f.ModuleName = parseGoModuleName(string(data))
		}
	case fileExists(filepath.Join(dir, "package.json")):
		f.Language = "Node.js / TypeScript"
	case fileExists(filepath.Join(dir, "pyproject.toml")):
		f.Language = "Python"
	case fileExists(filepath.Join(dir, "Cargo.toml")):
		f.Language = "Rust"
	}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				f.TopDirs = append(f.TopDirs, e.Name())
			}
		}
		sort.Strings(f.TopDirs)
	}

	if data, err := os.ReadFile(filepath.Join(dir, "README.md")); err == nil {
		f.ReadmeIntro = firstParagraph(string(data))
	}

	return f
}

// canInit reports whether /init can proceed: SPAI.md must not already exist in dir.
func canInit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SPAI.md"))
	return os.IsNotExist(err)
}

// fileExists reports whether the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// parseGoModuleName extracts the module name from go.mod content.
func parseGoModuleName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// firstParagraph returns the first non-empty, non-heading prose paragraph from
// markdown text (lines starting with '#' are treated as headings and skipped).
func firstParagraph(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	return strings.Join(lines, " ")
}

// buildInitContent drafts SPAI.md content from the gathered facts.
func buildInitContent(f projectFacts) string {
	var b strings.Builder

	b.WriteString("# Project Context\n\n")
	b.WriteString("> This file is injected into spai's system prompt when invoked from this\n")
	b.WriteString("> directory or any subdirectory. Edit it freely — shorter is often better.\n\n")
	b.WriteString("## Overview\n\n")

	if f.ModuleName != "" {
		fmt.Fprintf(&b, "**Module:** `%s`  \n", f.ModuleName)
	}
	if f.Language != "" {
		fmt.Fprintf(&b, "**Language:** %s\n\n", f.Language)
	} else {
		b.WriteString("\n")
	}

	if f.ReadmeIntro != "" {
		fmt.Fprintf(&b, "%s\n\n", f.ReadmeIntro)
	} else {
		b.WriteString("<!-- Describe what this project does in 1–2 sentences. -->\n\n")
	}

	if len(f.TopDirs) > 0 {
		b.WriteString("## Structure\n\n")
		for _, d := range f.TopDirs {
			fmt.Fprintf(&b, "- `%s/`\n", d)
		}
		b.WriteString("\n")
	}

	if f.Language == "Go" {
		b.WriteString("## Build & test\n\n")
		b.WriteString("```sh\n")
		b.WriteString("go build ./...\n")
		b.WriteString("go test ./...\n")
		b.WriteString("```\n\n")
	}

	b.WriteString("## Conventions & gotchas\n\n")
	b.WriteString("<!-- Add anything the AI should keep in mind: naming rules, forbidden patterns,\n")
	b.WriteString("     required reviewers, flaky tests, etc. -->\n")

	return b.String()
}

// handleInit implements the /init slash command: it scaffolds SPAI.md in the
// current working directory after showing the user a preview and asking to confirm.
func (r *REPL) handleInit() {
	spaiPath := filepath.Join(r.cwd, "SPAI.md")

	if !canInit(r.cwd) {
		fmt.Printf("%s SPAI.md already exists at %s — edit it directly.\n",
			red("✗"), cyan(spaiPath))
		return
	}

	facts := gatherProjectFacts(r.cwd)
	content := buildInitContent(facts)

	lines := computeDiff("", content, diffContextLines)
	out := renderDiff(lines, isTerminal(os.Stdout))
	if out != "" {
		fmt.Printf("\n%s %s\n\n", cyan("▶"), bold("SPAI.md (new file)"))
		fmt.Print(out)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Write %s? [y/N]: ", cyan("SPAI.md"))
	input, _ := reader.ReadString('\n')
	if s := strings.TrimSpace(strings.ToLower(input)); s != "y" && s != "yes" {
		fmt.Println(dim("aborted."))
		return
	}

	if err := os.WriteFile(spaiPath, []byte(content), 0o644); err != nil {
		fmt.Printf("%s failed to write SPAI.md: %v\n", red("✗"), err)
		return
	}
	fmt.Printf("%s wrote %s\n", green("✓"), cyan(spaiPath))
}
