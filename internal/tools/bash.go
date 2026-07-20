package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"spaish/internal/sandbox"
)

const maxToolOutput = 16 * 1024

// Bash runs a shell command and returns its combined stdout+stderr.
//
// When Sandbox is set and enabled, the child process is wrapped in the execution
// sandbox unless Trusted reports the command as explicitly blessed (the
// allow_commands carve-out). Sandbox setup that fails is fatal: the command is
// refused rather than run unsandboxed. Both fields are nil-safe — a zero-value
// Bash sandboxes nothing, matching the pre-sandbox behavior.
type Bash struct {
	// Sandbox restricts the spawned process. nil (or a disabled sandbox) means
	// no wrapping.
	Sandbox sandbox.Sandbox
	// Trusted reports whether a command is exempt from the sandbox. nil means
	// nothing is trusted (every command is wrapped when the sandbox is enabled).
	Trusted func(cmd string) bool
}

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

func (b Bash) Run(ctx context.Context, input json.RawMessage) (string, error) {
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

	// Containment is applied here, AFTER the permission gate has already allowed
	// this command to run — it never alters classification or confirmation.
	// Commands the user has explicitly blessed (Trusted) bypass the sandbox.
	if b.Sandbox != nil && b.Sandbox.Enabled() &&
		!(b.Trusted != nil && b.Trusted(args.Command)) {
		if err := b.Sandbox.Wrap(cmd); err != nil {
			return "", fmt.Errorf("sandbox setup failed (refusing to run unsandboxed): %w", err)
		}
	}

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
