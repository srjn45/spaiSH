package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// webFetchMaxBytes is the default cap on returned content.
const webFetchMaxBytes = 100 * 1024

// webFetchTimeout bounds a single fetch.
const webFetchTimeout = 30 * time.Second

// webFetchUserAgent identifies the agent to servers.
const webFetchUserAgent = "spai-agent/1.0 (+https://github.com/srjn45/spaiSH)"

// WebFetch retrieves a URL over HTTP(S) and returns its text content.
type WebFetch struct{}

func (WebFetch) Name() string { return "web_fetch" }

func (WebFetch) Description() string {
	return "Fetch a URL over http or https and return its text content. HTML is " +
		"reduced to readable text (tags stripped, scripts/styles removed); JSON, " +
		"plaintext, and markdown are returned as-is. Only http and https URLs are " +
		"allowed. Output is capped (default 100KB; override with max_bytes)."
}

func (WebFetch) Schema() map[string]any {
	return objectSchema(map[string]any{
		"url": strProp("The http or https URL to fetch."),
		"max_bytes": map[string]any{
			"type":        "integer",
			"description": "Maximum bytes of text to return (default 102400).",
		},
	}, "url")
}

func (WebFetch) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		URL      string `json:"url"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(args.URL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported url scheme %q: only http and https are allowed", u.Scheme)
	}

	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = webFetchMaxBytes
	}

	ctx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: webFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	// Read a bounded amount so a huge response cannot exhaust memory. We read a
	// little extra so tailTrim can still report truncation accurately.
	limited := io.LimitReader(resp.Body, int64(maxBytes)*4+1024)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	text := string(body)
	if isHTML(contentType, text) {
		text = htmlToText(text)
	}
	text = strings.TrimSpace(text)

	header := fmt.Sprintf("%s %s\n\n", resp.Request.URL.String(), resp.Status)
	return header + tailTrim(text, maxBytes), nil
}

// isHTML reports whether the content should be treated as HTML, based on the
// Content-Type header and, as a fallback, a sniff of the body.
func isHTML(contentType, body string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
		return true
	}
	if ct != "" {
		return false
	}
	// No content type: sniff for an html tag near the start.
	head := strings.ToLower(body)
	if len(head) > 1024 {
		head = head[:1024]
	}
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html")
}

// htmlToText performs a lightweight conversion of HTML into readable plain text:
// it drops <script>/<style> contents, strips all remaining tags, decodes a few
// common entities, and collapses excess blank lines. This is intentionally not a
// full readability/parser implementation — just enough to give the model usable
// text without any third-party dependency.
func htmlToText(html string) string {
	html = stripElement(html, "script")
	html = stripElement(html, "style")
	html = stripElement(html, "head")

	var b strings.Builder
	inTag := false
	for i := 0; i < len(html); i++ {
		c := html[i]
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
			// Treat block-ish tags as line breaks so text doesn't run together.
			b.WriteByte('\n')
		case !inTag:
			b.WriteByte(c)
		}
	}

	text := decodeEntities(b.String())
	return collapseBlankLines(text)
}

// stripElement removes <name ...>...</name> blocks (including their content),
// case-insensitively. Unclosed elements are dropped through end of string.
func stripElement(s, name string) string {
	lower := strings.ToLower(s)
	open := "<" + name
	closeTag := "</" + name + ">"
	var b strings.Builder
	for {
		i := strings.Index(lower, open)
		if i < 0 {
			b.WriteString(s)
			break
		}
		// Ensure the match is a tag start (next char is space, >, or /).
		after := i + len(open)
		if after < len(s) {
			nc := s[after]
			if nc != ' ' && nc != '>' && nc != '/' && nc != '\t' && nc != '\n' {
				// Not actually this element (e.g. <scripted>). Emit up to here.
				b.WriteString(s[:after])
				s = s[after:]
				lower = lower[after:]
				continue
			}
		}
		b.WriteString(s[:i])
		j := strings.Index(lower[i:], closeTag)
		if j < 0 {
			// No closing tag: drop the rest.
			s = ""
			lower = ""
			break
		}
		cut := i + j + len(closeTag)
		s = s[cut:]
		lower = lower[cut:]
	}
	return b.String()
}

// decodeEntities replaces a small set of common HTML entities.
func decodeEntities(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
	)
	return r.Replace(s)
}

// collapseBlankLines trims trailing whitespace per line and squeezes runs of
// blank lines down to a single blank line.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t\r")
		if strings.TrimSpace(ln) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
