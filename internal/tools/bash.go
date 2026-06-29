package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const maxToolOutput = 16 * 1024

// Bash runs a shell command and returns its combined stdout+stderr.
type Bash struct{}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Execute a shell command via `bash -c` and return its combined " +
		"stdout and stderr. Use non-interactive commands only (no vim, nano, top, " +
		"less, or anything that waits for input). Chain steps with && when needed."
}

func (Bash) Schema() map[string]any {
	return objectSchema(map[string]any{
		"command": strProp("The shell command to run."),
	}, "command")
}

func (Bash) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	result := tailTrim(out.String(), maxToolOutput)
	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		// Report failure as content (not a Go error) so the model sees the
		// output and exit code and can diagnose.
		return fmt.Sprintf("%s\n[exit code: %d]", result, exitCode), nil
	}
	if result == "" {
		result = "[no output; exit code 0]"
	}
	return result, nil
}

// Command extracts the command string from a bash tool call input, for display
// and permission classification.
func Command(input json.RawMessage) string {
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &args)
	return args.Command
}
