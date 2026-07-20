package agent

import (
	"context"
	"errors"
	"strings"

	"spaish/internal/ai"
	"spaish/internal/protocol"
	"spaish/internal/session"
	"spaish/internal/tools"
)

// defaultMaxIterations mirrors the fallback iteration budget used by loop() when
// Config.MaxIterations is unset (see agent.go). Kept in sync so childConfig can
// derive a strictly-smaller budget from an unset parent.
const defaultMaxIterations = 25

// SubagentProfile describes a named delegation target. When the parent model
// names a profile in a delegate tool call, the sub-agent runs with this system
// prompt and is restricted to the listed tools. An empty Tools slice means
// "inherit all parent tools without restriction".
type SubagentProfile struct {
	Name         string
	Description  string
	SystemPrompt string
	Tools        []string // empty = all parent tools
}

// builtinProfiles provides the zero-config defaults. User-configured profiles
// with the same name take precedence (resolveProfile checks user list first).
var builtinProfiles = []SubagentProfile{
	{
		Name:        "reviewer",
		Description: "Read-only code reviewer with a focused analysis prompt.",
		SystemPrompt: "You are a precise code reviewer. Analyse the code for correctness, " +
			"clarity, and security issues. Read files, search patterns, and report findings " +
			"concisely. Do not modify any files.",
		Tools: []string{
			"read_file", "grep", "glob", "list_dir", "web_search", "web_fetch",
		},
	},
	{
		Name:        "tester",
		Description: "Test writer and runner focused on correctness.",
		SystemPrompt: "You are a software testing expert. Write tests, run the test suite, " +
			"and diagnose failures. Prefer read tools for exploration and bash only for " +
			"running tests or compiling.",
		Tools: []string{
			"bash", "read_file", "write_file", "edit_file", "glob", "grep", "list_dir",
		},
	},
	{
		Name:         "general",
		Description:  "Generic subagent with the full parent tool set (default when no profile is named).",
		SystemPrompt: "", // use the standard agent system prompt
		Tools:        nil,
	},
}

// resolveProfile returns the named profile, checking userProfiles first so user
// config can override a builtin. Returns the zero SubagentProfile and false when
// name is empty or unknown.
func resolveProfile(userProfiles []SubagentProfile, name string) (SubagentProfile, bool) {
	if name == "" {
		return SubagentProfile{}, false
	}
	for _, p := range userProfiles {
		if p.Name == name {
			return p, true
		}
	}
	for _, p := range builtinProfiles {
		if p.Name == name {
			return p, true
		}
	}
	return SubagentProfile{}, false
}

// childConfig derives the Config for a delegated (nested) run from the parent's.
// The nested loop gets a strictly smaller MaxIterations budget so a runaway
// sub-agent can never burn as many turns as its parent, and it does not inherit
// the parent's piped stdin (that belonged to the top-level query, not the
// sub-task). WorkingDir, GitBranch, Mode, and Policy are inherited unchanged so
// the nested loop is gated exactly like the parent — same confirmation tiers,
// same policy — just scoped to a smaller budget.
func childConfig(parent Config) Config {
	child := parent
	base := parent.MaxIterations
	if base <= 0 {
		base = defaultMaxIterations
	}
	child.MaxIterations = base / 2
	if child.MaxIterations < 1 {
		child.MaxIterations = 1
	}
	child.Stdin = ""
	return child
}

// runDelegate executes a scoped sub-task on a freshly-built nested Agent and
// returns only its final summary text. It is the DelegateRunner wired into the
// "delegate" tool by NewWithRegistry.
//
// Safety-critical invariants enforced here:
//   - The nested Agent is built with a registry that does NOT contain the
//     delegate tool. That is the hard recursion limit: a nested loop physically
//     cannot delegate again, so depth can never exceed 1.
//   - When a profile specifies a tool allowlist, the registry is filtered to
//     that subset of the parent's tools — never expanded. A profile cannot grant
//     capabilities the parent lacks.
//   - The nested registry inherits the parent's Sandbox (and Trusted predicate),
//     so a delegated task is contained exactly like a top-level one and cannot
//     escape the sandbox.
//   - The parent's real confirmFn is passed through unchanged, so any
//     Write/Elevated/Destructive tool call the nested loop makes goes through the
//     exact same tier-based confirmation gate as a top-level call — delegation
//     never auto-approves or bypasses confirmation.
//   - The nested loop runs to completion under the parent's ctx (honoring
//     cancellation) and its intermediate Response events are consumed here rather
//     than streamed back to the parent; the parent sees one tool result.
func runDelegate(ctx context.Context, provider ai.Provider, confirmFn ConfirmFunc, child Config, task, profileName string) (string, error) {
	reg := tools.RegistryWithSandbox(child.Sandbox, child.Trusted)

	if profile, ok := resolveProfile(child.SubagentProfiles, profileName); ok {
		if len(profile.Tools) > 0 {
			reg = reg.Filter(profile.Tools)
		}
		if profile.SystemPrompt != "" {
			child.SystemPromptOverride = profile.SystemPrompt
		}
	}

	nested := &Agent{
		provider:  provider,
		config:    child,
		confirmFn: confirmFn,
		registry:  reg,
	}

	// A fresh, in-memory session: the sub-agent does not see the parent
	// conversation's history, and nothing here is persisted to disk.
	sess := &session.Session{}

	var summary strings.Builder
	var lastErr string
	for resp := range nested.Run(ctx, &protocol.AgentRequest{Query: task}, sess) {
		switch resp.Type {
		case "text":
			summary.WriteString(resp.Content)
		case "error":
			lastErr = resp.Content
		}
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	out := strings.TrimSpace(summary.String())
	if out == "" {
		if lastErr != "" {
			return "", errors.New(lastErr)
		}
		return "(sub-agent produced no output)", nil
	}
	return out, nil
}
