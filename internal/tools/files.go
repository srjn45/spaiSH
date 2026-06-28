package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFile returns the contents of a file.
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }
func (ReadFile) Description() string {
	return "Read and return the contents of a file at the given path."
}
func (ReadFile) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Path to the file to read."),
	}, "path")
}

func (ReadFile) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", err
	}
	return tailTrim(string(data), maxToolOutput), nil
}

// WriteFile creates or overwrites a file.
type WriteFile struct{}

func (WriteFile) Name() string { return "write_file" }
func (WriteFile) Description() string {
	return "Write content to a file, creating it (and parent directories) or " +
		"overwriting it if it exists."
}
func (WriteFile) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":    strProp("Path to the file to write."),
		"content": strProp("The full content to write to the file."),
	}, "path", "content")
}

func (WriteFile) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if dir := filepath.Dir(args.Path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
}

// EditFile performs an exact string replacement in a file.
type EditFile struct{}

func (EditFile) Name() string { return "edit_file" }
func (EditFile) Description() string {
	return "Replace an exact string in a file. old_string must match exactly and " +
		"uniquely unless replace_all is true. Use this for targeted edits instead " +
		"of rewriting the whole file."
}
func (EditFile) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path":        strProp("Path to the file to edit."),
		"old_string":  strProp("The exact text to replace."),
		"new_string":  strProp("The replacement text."),
		"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (default false)."},
	}, "path", "old_string", "new_string")
}

func (EditFile) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", err
	}
	content := string(data)
	n := strings.Count(content, args.OldString)
	if n == 0 {
		return "", fmt.Errorf("old_string not found in %s", args.Path)
	}
	if n > 1 && !args.ReplaceAll {
		return "", fmt.Errorf("old_string is not unique in %s (%d matches); set replace_all or add more context", args.Path, n)
	}
	if args.ReplaceAll {
		content = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		content = strings.Replace(content, args.OldString, args.NewString, 1)
	}
	info, _ := os.Stat(args.Path)
	mode := os.FileMode(0644)
	if info != nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(args.Path, []byte(content), mode); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s (%d replacement(s))", args.Path, n), nil
}

// ListDir lists the entries in a directory.
type ListDir struct{}

func (ListDir) Name() string { return "list_dir" }
func (ListDir) Description() string {
	return "List the entries in a directory (defaults to the current directory)."
}
func (ListDir) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Directory to list (default: current directory)."),
	})
}

func (ListDir) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(input, &args)
	if args.Path == "" {
		args.Path = "."
	}
	entries, err := os.ReadDir(args.Path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		b.WriteString(name)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return "(empty directory)", nil
	}
	return tailTrim(b.String(), maxToolOutput), nil
}
