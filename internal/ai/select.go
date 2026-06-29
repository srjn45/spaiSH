package ai

import "fmt"

// ProviderSet holds the available providers and the routing preference used to
// pick one. It replaces the selection logic that used to live in internal/router.
type ProviderSet struct {
	Cloud       Provider
	Local       Provider
	PreferLocal bool
	APIKeyEnv   string // env var name, used only for the "no provider" error message
}

// Select returns the provider to use for a request.
//
// If forceLocal or PreferLocal is set, the local provider is required. Otherwise
// the cloud provider is preferred, falling back to local when the cloud is
// unconfigured/unreachable.
func (s ProviderSet) Select(forceLocal bool) (Provider, error) {
	if forceLocal || s.PreferLocal {
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
