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

// MultiEdit performs a regex find-and-replace across all files matched by a
// glob pattern in a single tool call. It is intentionally wider-blast-radius
// than edit_file, so it sits at Write tier and supports dry_run for inspection
// before committing changes.
type MultiEdit struct{}

func (MultiEdit) Name() string { return "multi_edit" }
func (MultiEdit) Description() string {
	return "Apply a regex find-and-replace across all files matched by a glob " +
		"pattern. Uses Go regexp syntax; the replacement string may reference " +
		"capture groups ($1, ${name}). Set dry_run to preview matches without " +
		"writing. Reports each changed file and the number of substitutions made."
}
func (MultiEdit) Schema() map[string]any {
	return objectSchema(map[string]any{
		"glob":        strProp("Glob pattern selecting the files to edit (e.g. **/*.go, src/*.ts)."),
		"pattern":     strProp("Go regular expression to find."),
		"replacement": strProp("Replacement string; may use $1, ${name} for capture groups."),
		"path":        strProp("Root directory to search (default: current directory)."),
		"dry_run": map[string]any{
			"type":        "boolean",
			"description": "If true, report matches without writing any files (default false).",
		},
	}, "glob", "pattern", "replacement")
}

type multiEditArgs struct {
	Glob        string `json:"glob"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Path        string `json:"path"`
	DryRun      bool   `json:"dry_run"`
}

func (MultiEdit) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args multiEditArgs
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

	// Collect matched files via the same walk+globMatch logic as the Glob tool.
	var files []string
	if walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
		if globMatch(args.Glob, rel) {
			files = append(files, p)
		}
		return nil
	}); walkErr != nil {
		return "", fmt.Errorf("glob walk failed: %w", walkErr)
	}

	if len(files) == 0 {
		return "(no files matched glob pattern)", nil
	}

	type fileResult struct {
		path  string
		count int
	}
	var changed []fileResult
	totalMatches := 0

	for _, fpath := range files {
		data, readErr := os.ReadFile(fpath)
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", fpath, readErr)
		}
		content := string(data)
		matches := re.FindAllStringIndex(content, -1)
		if len(matches) == 0 {
			continue
		}
		n := len(matches)
		totalMatches += n

		if !args.DryRun {
			newContent := re.ReplaceAllString(content, args.Replacement)
			info, _ := os.Stat(fpath)
			mode := os.FileMode(0644)
			if info != nil {
				mode = info.Mode()
			}
			if writeErr := os.WriteFile(fpath, []byte(newContent), mode); writeErr != nil {
				return "", fmt.Errorf("write %s: %w", fpath, writeErr)
			}
		}
		changed = append(changed, fileResult{path: fpath, count: n})
	}

	if len(changed) == 0 {
		return fmt.Sprintf("(no matches in %d file(s))", len(files)), nil
	}

	var sb strings.Builder
	verb := "edited"
	if args.DryRun {
		verb = "would edit"
		fmt.Fprintf(&sb, "[dry-run] %d substitution(s) across %d file(s):\n", totalMatches, len(changed))
	} else {
		fmt.Fprintf(&sb, "%d substitution(s) across %d file(s):\n", totalMatches, len(changed))
	}
	for _, r := range changed {
		fmt.Fprintf(&sb, "  %s %s (%d match(es))\n", verb, r.path, r.count)
	}
	return tailTrim(sb.String(), maxToolOutput), nil
}
