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

const httpRequestMaxBytes = 100 * 1024
const httpRequestTimeout = 30 * time.Second
const httpRequestUserAgent = "spai-agent/1.0 (+https://github.com/srjn45/spaiSH)"

// HTTPRequest sends an HTTP request with a configurable method, headers, and body.
type HTTPRequest struct{}

func (HTTPRequest) Name() string { return "http_request" }

func (HTTPRequest) Description() string {
	return "Send an HTTP request (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS) to a URL " +
		"and return the response status, selected headers, and body. Only http and https " +
		"URLs are allowed. Response body is capped at 100 KB. Use this for API calls, " +
		"webhooks, or any HTTP interaction that may mutate remote state."
}

func (HTTPRequest) Schema() map[string]any {
	return objectSchema(map[string]any{
		"url": strProp("The http or https URL to send the request to."),
		"method": map[string]any{
			"type":        "string",
			"description": "HTTP method: GET, POST, PUT, PATCH, DELETE, HEAD, or OPTIONS (default: GET).",
			"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		},
		"headers": map[string]any{
			"type":                 "object",
			"description":          "Optional map of request headers (string keys and string values).",
			"additionalProperties": map[string]any{"type": "string"},
		},
		"body": strProp("Optional request body (e.g. JSON string). Sent as-is."),
		"max_bytes": map[string]any{
			"type":        "integer",
			"description": "Maximum bytes of response body to return (default 102400).",
		},
	}, "url")
}

func (HTTPRequest) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		URL      string            `json:"url"`
		Method   string            `json:"method"`
		Headers  map[string]string `json:"headers"`
		Body     string            `json:"body"`
		MaxBytes int               `json:"max_bytes"`
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

	method := strings.ToUpper(args.Method)
	if method == "" {
		method = http.MethodGet
	}

	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = httpRequestMaxBytes
	}

	ctx, cancel := context.WithTimeout(ctx, httpRequestTimeout)
	defer cancel()

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return "", fmt.Errorf("build request failed: %w", err)
	}
	req.Header.Set("User-Agent", httpRequestUserAgent)
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: httpRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, int64(maxBytes)+1024)
	rawBody, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s\n", method, resp.Request.URL.String())
	fmt.Fprintf(&sb, "Status: %s\n", resp.Status)
	for _, h := range []string{"Content-Type", "Content-Length", "Location", "X-Request-Id"} {
		if v := resp.Header.Get(h); v != "" {
			fmt.Fprintf(&sb, "%s: %s\n", h, v)
		}
	}
	sb.WriteString("\n")
	sb.WriteString(tailTrim(string(rawBody), maxBytes))

	return sb.String(), nil
}
