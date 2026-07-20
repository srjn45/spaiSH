package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// webSearchDefaultResults is the number of results returned when the caller
// does not specify max_results.
const webSearchDefaultResults = 5

// webSearchMaxResults caps how many results the tool will ever return, to keep
// output bounded regardless of what the caller asks for.
const webSearchMaxResults = 10

// webSearchMaxBytes caps total returned text. A results list is far smaller
// than a full page fetch, but we still bound it defensively.
const webSearchMaxBytes = 32 * 1024

// duckDuckGoHTMLEndpoint is the no-key search backend. It is a package-level var
// (not a const) so tests can point the tool at an httptest server serving a
// canned fixture instead of the live network.
var duckDuckGoHTMLEndpoint = "https://html.duckduckgo.com/html/"

// resultTitleRe matches a result's title anchor, capturing the (redirect) href
// and the title HTML. DuckDuckGo's HTML markup uses stable class names.
var resultTitleRe = regexp.MustCompile(`(?is)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)

// resultSnippetRe matches a result's snippet anchor, capturing the snippet HTML.
var resultSnippetRe = regexp.MustCompile(`(?is)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)

// tagRe strips any remaining HTML tag.
var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// wsRe collapses runs of whitespace to a single space.
var wsRe = regexp.MustCompile(`\s+`)

// WebSearch performs a keyless web search and returns a capped list of results.
type WebSearch struct{}

func (WebSearch) Name() string { return "web_search" }

func (WebSearch) Description() string {
	return "Search the web and return a ranked list of results, each with a " +
		"title, URL, and short snippet. Useful for finding pages to then read " +
		"with web_fetch. No API key required. Provide a query; optionally set " +
		"max_results (default 5, capped at 10)."
}

func (WebSearch) Schema() map[string]any {
	return objectSchema(map[string]any{
		"query": strProp("The search query."),
		"max_results": map[string]any{
			"type":        "integer",
			"description": "Maximum number of results to return (default 5, capped at 10).",
		},
	}, "query")
}

// searchResult is a single parsed result.
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

func (WebSearch) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = webSearchDefaultResults
	}
	if maxResults > webSearchMaxResults {
		maxResults = webSearchMaxResults
	}

	ctx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()

	endpoint, err := url.Parse(duckDuckGoHTMLEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid search endpoint: %w", err)
	}
	q := endpoint.Query()
	q.Set("q", query)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: webFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("search failed: upstream returned %s", resp.Status)
	}

	// Read a bounded amount: HTML for a page of results is well under a MiB.
	limited := io.LimitReader(resp.Body, int64(webFetchMaxBytes)*4)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	results := parseDuckDuckGoResults(string(body), maxResults)
	if len(results) == 0 {
		return fmt.Sprintf("No results for %q.", query), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q:\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
		b.WriteByte('\n')
	}
	return tailTrim(strings.TrimSpace(b.String()), webSearchMaxBytes), nil
}

// parseDuckDuckGoResults extracts up to max results from DuckDuckGo's HTML
// response. Each result contributes a title anchor (result__a) and, usually, a
// snippet anchor (result__snippet). Snippets are associated with their result by
// scanning only the slice of HTML between one title anchor and the next, so a
// missing snippet never shifts snippets onto the wrong result.
func parseDuckDuckGoResults(html string, max int) []searchResult {
	titleMatches := resultTitleRe.FindAllStringSubmatchIndex(html, -1)
	results := make([]searchResult, 0, len(titleMatches))
	for i, m := range titleMatches {
		href := html[m[2]:m[3]]
		titleHTML := html[m[4]:m[5]]

		link := extractRealURL(href)
		title := cleanHTMLText(titleHTML)
		if link == "" || title == "" {
			continue
		}

		// Search for this result's snippet only within its own block, bounded by
		// the start of the next result's title anchor.
		blockEnd := len(html)
		if i+1 < len(titleMatches) {
			blockEnd = titleMatches[i+1][0]
		}
		snippet := ""
		if sm := resultSnippetRe.FindStringSubmatch(html[m[1]:blockEnd]); sm != nil {
			snippet = cleanHTMLText(sm[1])
		}

		results = append(results, searchResult{Title: title, URL: link, Snippet: snippet})
		if len(results) >= max {
			break
		}
	}
	return results
}

// extractRealURL resolves a DuckDuckGo result href to the underlying target URL.
// DuckDuckGo wraps results in a redirector ("//duckduckgo.com/l/?uddg=<encoded>"),
// so the real destination is the url-decoded "uddg" query parameter. Hrefs that
// are already absolute (or protocol-relative) are returned normalized.
func extractRealURL(href string) string {
	href = decodeEntities(href)
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}

	// Protocol-relative URLs (very common in this markup): give them a scheme so
	// url.Parse populates Host and Query.
	parseTarget := href
	if strings.HasPrefix(parseTarget, "//") {
		parseTarget = "https:" + parseTarget
	}

	u, err := url.Parse(parseTarget)
	if err != nil {
		return ""
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return u.String()
	}
	return ""
}

// cleanHTMLText strips tags (e.g. the <b> highlighting DuckDuckGo wraps around
// matched terms), decodes common entities, and normalizes whitespace to a single
// line suitable for a compact result listing.
func cleanHTMLText(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = decodeEntities(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
