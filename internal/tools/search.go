package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// skipDirs are never descended into during glob/grep walks.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true}

// Glob finds files matching a glob pattern, walking the directory tree.
type Glob struct{}

func (Glob) Name() string { return "glob" }
func (Glob) Description() string {
	return "Find files matching a glob pattern (e.g. **/*.go, src/*.ts). Returns " +
		"matching paths relative to the search root."
}
func (Glob) Schema() map[string]any {
	return objectSchema(map[string]any{
		"pattern": strProp("Glob pattern, e.g. **/*.go or *.md."),
		"path":    strProp("Root directory to search (default: current directory)."),
	}, "pattern")
}

func (Glob) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	root := args.Path
	if root == "" {
		root = "."
	}
	// Translate ** into a base-name match by matching against the relative path.
	var matches []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if globMatch(args.Pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	return tailTrim(strings.Join(matches, "\n"), maxToolOutput), nil
}

// globMatch matches pattern against a relative path, supporting ** as "any
// number of path segments".
func globMatch(pattern, rel string) bool {
	if strings.Contains(pattern, "**") {
		// Match the suffix (e.g. **/*.go → match base pattern against basename
		// or any trailing path).
		suffix := strings.TrimPrefix(pattern, "**/")
		base := filepath.Base(rel)
		if ok, _ := filepath.Match(suffix, base); ok {
			return true
		}
		if ok, _ := filepath.Match(suffix, rel); ok {
			return true
		}
		return false
	}
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	ok, _ := filepath.Match(pattern, filepath.Base(rel))
	return ok
}

// Grep searches file contents for a regular expression.
type Grep struct{}

func (Grep) Name() string { return "grep" }
func (Grep) Description() string {
	return "Search file contents for a regular expression and return matching " +
		"lines as path:line:text. Searches recursively from the given path."
}
func (Grep) Schema() map[string]any {
	return objectSchema(map[string]any{
		"pattern": strProp("Regular expression to search for."),
		"path":    strProp("File or directory to search (default: current directory)."),
	}, "pattern")
}

func (Grep) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	root := args.Path
	if root == "" {
		root = "."
	}

	var out strings.Builder
	count := 0
	const maxMatches = 200

	search := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		for i, line := range strings.Split(string(data), "\n") {
			if count >= maxMatches {
				return
			}
			if re.MatchString(line) {
				fmt.Fprintf(&out, "%s:%d:%s\n", path, i+1, line)
				count++
			}
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if count >= maxMatches {
				return filepath.SkipAll
			}
			search(p)
			return nil
		})
	} else {
		search(root)
	}

	if out.Len() == 0 {
		return "(no matches)", nil
	}
	res := out.String()
	if count >= maxMatches {
		res += "[...more matches omitted...]\n"
	}
	return tailTrim(res, maxToolOutput), nil
}
