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
//   - The nested Agent is built with a plain tools.DefaultRegistry(), which does
//     NOT contain the delegate tool. That is the hard recursion limit: a nested
//     loop physically cannot delegate again, so depth can never exceed 1.
//   - The parent's real confirmFn is passed through unchanged, so any
//     Write/Elevated/Destructive tool call the nested loop makes goes through the
//     exact same tier-based confirmation gate as a top-level call — delegation
//     never auto-approves or bypasses confirmation.
//   - The nested loop runs to completion under the parent's ctx (honoring
//     cancellation) and its intermediate Response events are consumed here rather
//     than streamed back to the parent; the parent sees one tool result.
func runDelegate(ctx context.Context, provider ai.Provider, confirmFn ConfirmFunc, child Config, task string) (string, error) {
	nested := &Agent{
		provider:  provider,
		config:    child,
		confirmFn: confirmFn,
		registry:  tools.DefaultRegistry(),
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
