package ai

import (
	"fmt"
	"strings"
)

// TaskKind classifies the nature of an LLM call for model routing.
type TaskKind int

const (
	// TaskKindReasoning is the main agent tool-calling loop (the default).
	TaskKindReasoning TaskKind = iota
	// TaskKindCheap covers summarisation, auto-compact, and other text-only calls.
	TaskKindCheap
)

// ModelRouter selects a per-task model override from the [routing] config
// section. The zero value disables routing: ModelFor always returns "" so all
// providers fall through to their own configured model. Safe for concurrent use
// (immutable after construction).
type ModelRouter struct {
	Small  string // model for TaskKindCheap; "" = provider default
	Strong string // model for TaskKindReasoning; "" = provider default
}

// ModelFor returns the model override for kind, or "" when that kind is not
// configured. A "" return means "use the provider's own model" — callers may
// pass it directly to ai.Request.Model without branching.
func (r ModelRouter) ModelFor(kind TaskKind) string {
	switch kind {
	case TaskKindCheap:
		return r.Small
	case TaskKindReasoning:
		return r.Strong
	}
	return ""
}

// Enabled reports whether any routing is configured. It is false on the zero
// value, matching the "identical to today" default.
func (r ModelRouter) Enabled() bool {
	return r.Small != "" || r.Strong != ""
}

// ProviderSet holds the available providers and the routing preference used to
// pick one. It replaces the selection logic that used to live in internal/router.
type ProviderSet struct {
	Override    Provider // explicit session choice (e.g. set via the REPL's /model)
	Cloud       Provider
	Local       Provider
	PreferLocal bool
	APIKeyEnv   string // env var name, used only for the "no provider" error message
}

// Select returns the provider to use for a request.
//
// forceLocal always requires the local provider. Otherwise an explicit Override
// (set interactively via /model) wins; failing that the cloud provider is
// preferred, falling back to local when the cloud is unconfigured/unreachable.
// PreferLocal flips the default to local-first when no Override is set.
func (s ProviderSet) Select(forceLocal bool) (Provider, error) {
	if forceLocal {
		if s.Local != nil && s.Local.Available() {
			return s.Local, nil
		}
		return nil, fmt.Errorf("local model not available — is your local model runtime running?")
	}
	if s.Override != nil && s.Override.Available() {
		return s.Override, nil
	}
	if s.PreferLocal {
		if s.Local != nil && s.Local.Available() {
			return s.Local, nil
		}
		return nil, fmt.Errorf("local model not available — is your local model runtime running?")
	}
	if s.Cloud != nil && s.Cloud.Available() {
		return s.Cloud, nil
	}
	if s.Local != nil && s.Local.Available() {
		return s.Local, nil
	}
	env := s.APIKeyEnv
	if env == "" {
		env = "the provider API key"
	}
	return nil, fmt.Errorf("no AI provider available — set %s or start a local model", env)
}

// ParseModelSelection interprets the arguments to a /model command into a
// provider name and a model name. Either may be empty. It accepts:
//
//	anthropic                 → provider only
//	anthropic claude-opus-4-8 → provider + model (two tokens)
//	anthropic:claude-opus-4-8 → provider + model (colon-separated)
//	openai/gpt-4o             → provider + model (slash-separated)
//	claude-opus-4-8           → model only (bare token that isn't a provider)
//
// isProvider reports whether a bare token names a known provider; a bare token
// that is not a provider is treated as a model for the currently active
// provider.
func ParseModelSelection(args []string, isProvider func(string) bool) (provider, model string) {
	switch len(args) {
	case 0:
		return "", ""
	case 1:
		s := strings.TrimSpace(args[0])
		if i := strings.IndexAny(s, ":/"); i >= 0 {
			return s[:i], s[i+1:]
		}
		if isProvider != nil && isProvider(s) {
			return s, ""
		}
		return "", s
	default:
		return strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
	}
}
