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

// ghSubcommands is the allow-listed set of gh subcommands the tool exposes,
// in a stable order (used for the schema enum and error messages).
var ghSubcommands = []string{
	"pr-view", "pr-list", "pr-status",
	"pr-checkout", "create-branch",
	"push",
	"pr-create", "pr-comment", "pr-merge", "pr-close",
}

var allowedGHSubcommands = func() map[string]bool {
	m := make(map[string]bool, len(ghSubcommands))
	for _, s := range ghSubcommands {
		m[s] = true
	}
	return m
}()

// GH runs structured GitHub CLI and git operations, complementing the git tool
// with GitHub-native actions: PR lifecycle (create, view, list, merge, close),
// pull-request comments and checkout, plus convenience create-branch and push
// wrappers. Each subcommand is permission-tiered independently (see GHTier):
// read-only queries run silently; local mutations require Write confirmation;
// outward-facing operations require Elevated approval.
type GH struct{}

func (GH) Name() string { return "gh" }

func (GH) Description() string {
	return "GitHub CLI tool for PR and repository workflows. Subcommands: " +
		strings.Join(ghSubcommands, ", ") + ". " +
		"Read-only operations (pr-view, pr-list, pr-status) run without confirmation; " +
		"local mutations (pr-checkout, create-branch) require Write confirmation; " +
		"remote/outward-facing operations (push, pr-create, pr-comment, pr-merge, pr-close) " +
		"require Elevated approval. Requires gh CLI and git to be installed."
}

func (GH) Schema() map[string]any {
	return objectSchema(map[string]any{
		"subcommand": map[string]any{
			"type":        "string",
			"description": "The operation to perform.",
			"enum":        ghSubcommands,
		},
		"args": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Additional flags and arguments for the subcommand, e.g. [\"--title\", \"My PR\", \"--body\", \"...\"] for pr-create.",
		},
	}, "subcommand")
}

func (GH) Run(ctx context.Context, input json.RawMessage) (string, error) {
	sub, args := GHCall(input)
	if sub == "" {
		return "", fmt.Errorf("missing subcommand")
	}
	if !allowedGHSubcommands[sub] {
		return "", fmt.Errorf("unsupported gh subcommand %q (allowed: %s)", sub, strings.Join(ghSubcommands, ", "))
	}

	prog, cmdArgs := ghCommand(sub, args)
	cmd := exec.CommandContext(ctx, prog, cmdArgs...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	result := tailTrim(out.String(), maxToolOutput)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s not found: is %s installed?", prog, prog)
		}
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		// Return failure as content so the model sees the message and exit code.
		return fmt.Sprintf("%s\n[exit code: %d]", result, exitCode), nil
	}
	if result == "" {
		result = "[no output; exit code 0]"
	}
	return result, nil
}

// ghCommand translates a GH tool subcommand into the binary and arguments to
// execute. `create-branch` and `push` delegate to git; all others use gh.
func ghCommand(sub string, args []string) (string, []string) {
	switch sub {
	case "pr-view":
		return "gh", append([]string{"pr", "view"}, args...)
	case "pr-list":
		return "gh", append([]string{"pr", "list"}, args...)
	case "pr-status":
		return "gh", append([]string{"pr", "status"}, args...)
	case "pr-create":
		return "gh", append([]string{"pr", "create"}, args...)
	case "pr-merge":
		return "gh", append([]string{"pr", "merge"}, args...)
	case "pr-close":
		return "gh", append([]string{"pr", "close"}, args...)
	case "pr-comment":
		return "gh", append([]string{"pr", "comment"}, args...)
	case "pr-checkout":
		return "gh", append([]string{"pr", "checkout"}, args...)
	case "create-branch":
		return "git", append([]string{"checkout", "-b"}, args...)
	case "push":
		return "git", append([]string{"push"}, args...)
	default:
		return "gh", append([]string{sub}, args...)
	}
}

// GHCall extracts the subcommand and argument list from a gh tool call input,
// for display and permission classification. `args` may be encoded as a JSON
// array of strings or as a single whitespace-delimited string.
func GHCall(input json.RawMessage) (subcommand string, args []string) {
	var a struct {
		Subcommand string          `json:"subcommand"`
		Args       json.RawMessage `json:"args"`
	}
	_ = json.Unmarshal(input, &a)
	return strings.TrimSpace(a.Subcommand), parseGitArgs(a.Args)
}

// GHTier returns the permission tier for a gh tool subcommand. It is a pure,
// offline decision consumed by the agent's classifier: read-only queries
// (pr-view, pr-list, pr-status) are TierRead; local mutations (pr-checkout,
// create-branch) are TierWrite; outward-facing operations (push, pr-create,
// pr-comment, pr-merge, pr-close) are TierElevated.
func GHTier(subcommand string) permissions.Tier {
	switch subcommand {
	case "pr-view", "pr-list", "pr-status":
		return permissions.TierRead
	case "pr-checkout", "create-branch":
		return permissions.TierWrite
	case "push", "pr-create", "pr-comment", "pr-merge", "pr-close":
		return permissions.TierElevated
	default:
		return permissions.TierWrite
	}
}
