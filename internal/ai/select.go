package ai

import (
	"fmt"
	"strings"
)

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
