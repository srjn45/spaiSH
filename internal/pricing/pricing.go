// Package pricing holds a small static table of per-model API prices and helpers
// to estimate the dollar cost of a token count. Prices are expressed in US
// dollars per 1,000,000 tokens and are approximate list prices; they are used
// only for local cost *estimates* (e.g. the /cost command), never for billing.
package pricing

import "strings"

// tokensPerUnit is the token count each Rate price is quoted against (per 1M).
const tokensPerUnit = 1_000_000

// Rate is the price of a model in US dollars per 1,000,000 tokens, split by
// input (prompt) and output (generated) tokens.
type Rate struct {
	Model  string  // canonical model id this rate was matched from
	Input  float64 // $ per 1M input tokens
	Output float64 // $ per 1M output tokens
	Local  bool    // true for local/self-hosted models that cost nothing
}

// table maps a model id (or id prefix) to its Rate. Lookup matches the longest
// key that is a substring of the requested model, so versioned ids such as
// "claude-opus-4-8-20260101" resolve to the base entry.
var table = []Rate{
	{Model: "claude-opus-4-8", Input: 5, Output: 25},
	{Model: "claude-sonnet-4-6", Input: 3, Output: 15},
	{Model: "claude-haiku-4-5", Input: 1, Output: 5},
}

// localPrefixes identifies model ids served by local/self-hosted runtimes, which
// have no per-token API cost.
var localPrefixes = []string{"ollama", "llama", "qwen", "mistral", "gemma", "phi", "deepseek", "local"}

// Lookup returns the Rate for a model id and whether a known entry matched.
// Matching is case-insensitive and by substring, so versioned or vendor-prefixed
// ids (e.g. "anthropic/claude-opus-4-8") still resolve. Local/self-hosted models
// return a zero-cost Rate with Local set and ok=true. Unknown models return a
// zero Rate with ok=false so callers can render an "unknown pricing" notice.
func Lookup(model string) (Rate, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return Rate{}, false
	}

	for _, p := range localPrefixes {
		if strings.Contains(m, p) {
			return Rate{Model: model, Local: true}, true
		}
	}

	// Prefer the longest matching key so more specific entries win.
	var best Rate
	found := false
	for _, r := range table {
		if strings.Contains(m, r.Model) && (!found || len(r.Model) > len(best.Model)) {
			best = r
			found = true
		}
	}
	if found {
		return best, true
	}
	return Rate{}, false
}

// Cost estimates the dollar cost of a request given input (prompt) and output
// (generated) token counts at the given Rate. Local rates always cost $0.
func (r Rate) Cost(inputTokens, outputTokens int) float64 {
	if r.Local {
		return 0
	}
	in := float64(inputTokens) / tokensPerUnit * r.Input
	out := float64(outputTokens) / tokensPerUnit * r.Output
	return in + out
}

// CostWithCache estimates the dollar cost of a request that used Anthropic
// prompt caching. Cache writes (5-minute TTL) cost 1.25× the base input price;
// cache reads cost 0.1×. Local rates always cost $0.
func (r Rate) CostWithCache(inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int) float64 {
	if r.Local {
		return 0
	}
	in := float64(inputTokens) / tokensPerUnit * r.Input
	out := float64(outputTokens) / tokensPerUnit * r.Output
	cacheWrite := float64(cacheCreationTokens) / tokensPerUnit * r.Input * 1.25
	cacheRead := float64(cacheReadTokens) / tokensPerUnit * r.Input * 0.1
	return in + out + cacheWrite + cacheRead
}

// EstimateCost is a convenience wrapper: it looks up the model and returns the
// estimated cost for the token counts plus whether a pricing entry was found.
func EstimateCost(model string, inputTokens, outputTokens int) (float64, bool) {
	r, ok := Lookup(model)
	if !ok {
		return 0, false
	}
	return r.Cost(inputTokens, outputTokens), true
}
