package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"spaish/internal/permissions"
)

// gitSubcommands is the allow-listed set of git subcommands the tool exposes,
// in a stable order (used for the schema enum and error messages).
var gitSubcommands = []string{
	"status", "diff", "log", "blame", "show",
	"branch", "add", "commit", "checkout", "reset", "push",
}

var allowedGitSubcommands = func() map[string]bool {
	m := make(map[string]bool, len(gitSubcommands))
	for _, s := range gitSubcommands {
		m[s] = true
	}
	return m
}()

// Git runs a structured git subcommand via os/exec (no shell) and returns its
// combined stdout+stderr. Unlike the free-form bash tool, it exposes a discrete
// set of subcommands so each can be permission-tiered independently (see
// GitTier): read-only inspection (status/diff/log/blame/show) auto-runs while
// mutating operations (checkout/reset/commit/push/...) stay gated.
type Git struct{}

func (Git) Name() string { return "git" }

func (Git) Description() string {
	return "Run a structured git command. Provide `subcommand` (one of: " +
		strings.Join(gitSubcommands, ", ") + ") and optional `args` (an array of " +
		"flags/paths, e.g. [\"-n\", \"20\"] for log or a file path for diff). " +
		"Executes git directly (no shell) in the current working directory and " +
		"returns combined stdout+stderr. Read-only subcommands run without " +
		"confirmation; mutating ones require approval."
}

func (Git) Schema() map[string]any {
	return objectSchema(map[string]any{
		"subcommand": map[string]any{
			"type":        "string",
			"description": "The git subcommand to run.",
			"enum":        gitSubcommands,
		},
		"args": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Additional arguments/flags for the subcommand, e.g. [\"-n\", \"20\"] or a file path. A single string is also accepted and split on whitespace.",
		},
	}, "subcommand")
}

func (Git) Run(ctx context.Context, input json.RawMessage) (string, error) {
	sub, args := GitCall(input)
	if sub == "" {
		return "", fmt.Errorf("missing subcommand")
	}
	if !allowedGitSubcommands[sub] {
		return "", fmt.Errorf("unsupported git subcommand %q (allowed: %s)", sub, strings.Join(gitSubcommands, ", "))
	}

	cmd := exec.CommandContext(ctx, "git", append([]string{sub}, args...)...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	result := tailTrim(out.String(), maxToolOutput)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("git not found: is git installed?")
		}
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		// Report failure (e.g. "not a git repository") as content, not a Go
		// error, so the model sees git's message and exit code and can diagnose.
		return fmt.Sprintf("%s\n[exit code: %d]", result, exitCode), nil
	}
	if result == "" {
		result = "[no output; exit code 0]"
	}
	return result, nil
}

// GitCall extracts the subcommand and argument list from a git tool call input,
// for display and permission classification. `args` may be encoded as a JSON
// array of strings or as a single whitespace-delimited string.
func GitCall(input json.RawMessage) (subcommand string, args []string) {
	var a struct {
		Subcommand string          `json:"subcommand"`
		Args       json.RawMessage `json:"args"`
	}
	_ = json.Unmarshal(input, &a)
	return strings.TrimSpace(a.Subcommand), parseGitArgs(a.Args)
}

func parseGitArgs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.Fields(s)
	}
	return nil
}

// GitTier returns the permission tier for a git subcommand + args combination.
// It is a pure, offline decision (no AI, no shell) consumed by the agent's
// classify(): read-only inspection is TierRead; branch listing is TierRead while
// branch create/delete/rename is TierWrite; add/commit/checkout/reset are
// TierWrite; reset --hard is TierDestructive; push is TierElevated and a
// force-push (data-loss-adjacent, like rm -rf) is TierDestructive.
func GitTier(subcommand string, args []string) permissions.Tier {
	switch subcommand {
	case "status", "diff", "log", "blame", "show":
		return permissions.TierRead
	case "branch":
		if branchMutates(args) {
			return permissions.TierWrite
		}
		return permissions.TierRead
	case "add", "commit", "checkout":
		return permissions.TierWrite
	case "reset":
		if hasArg(args, "--hard") {
			return permissions.TierDestructive
		}
		return permissions.TierWrite
	case "push":
		if hasArg(args, "--force", "-f", "--force-with-lease") {
			return permissions.TierDestructive
		}
		return permissions.TierElevated
	default:
		// Unknown/unsupported subcommands are gated at Write (requires confirm).
		return permissions.TierWrite
	}
}

// branchMutates reports whether a `git branch` invocation modifies refs (delete,
// rename, copy, or create) rather than merely listing them.
func branchMutates(args []string) bool {
	if hasArg(args, "-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy") {
		return true
	}
	// A positional (non-flag) argument means a branch name is being created.
	return firstNonFlag(args) != ""
}

func hasArg(args []string, want ...string) bool {
	for _, a := range args {
		for _, w := range want {
			if a == w {
				return true
			}
		}
	}
	return false
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}
