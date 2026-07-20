package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ddgResult renders one DuckDuckGo-shaped result block. The href is the
// redirector form the live site uses ("//duckduckgo.com/l/?uddg=<encoded>"),
// with entity-encoded ampersands, so the fixture exercises real URL extraction.
func ddgResult(encodedURL, titleHTML, snippetHTML string) string {
	redirect := `//duckduckgo.com/l/?uddg=` + encodedURL + `&amp;rut=deadbeef`
	return `<div class="result results_links results_links_deep web-result ">
  <div class="links_main links_deep result__body">
    <h2 class="result__title">
      <a rel="nofollow" class="result__a" href="` + redirect + `">` + titleHTML + `</a>
    </h2>
    <a class="result__snippet" href="` + redirect + `">` + snippetHTML + `</a>
  </div>
</div>`
}

// ddgPage wraps result blocks in the outer results container.
func ddgPage(results ...string) string {
	return `<!DOCTYPE html><html><body><div class="serp__results"><div class="results">` +
		strings.Join(results, "\n") + `</div></div></body></html>`
}

// withSearchEndpoint points the tool at a test server for the duration of fn,
// restoring the live endpoint afterward.
func withSearchEndpoint(t *testing.T, url string, fn func()) {
	t.Helper()
	prev := duckDuckGoHTMLEndpoint
	duckDuckGoHTMLEndpoint = url
	defer func() { duckDuckGoHTMLEndpoint = prev }()
	fn()
}

func TestWebSearch_MultiResultParse(t *testing.T) {
	page := ddgPage(
		ddgResult("https%3A%2F%2Fpkg.go.dev%2Fcontext",
			"context package - Go Packages",
			"<b>Package</b> context defines the Context type."),
		ddgResult("https%3A%2F%2Fgo.dev%2Fblog%2Fcontext",
			"Go Concurrency Patterns: Context",
			"The context package makes it easy to pass request-scoped values."),
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected a User-Agent header")
		}
		if got := r.URL.Query().Get("q"); got != "golang context" {
			t.Errorf("query not forwarded: got %q", got)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	withSearchEndpoint(t, srv.URL, func() {
		out := run(t, WebSearch{}, `{"query":"golang context"}`)

		for _, want := range []string{
			"context package - Go Packages",
			"https://pkg.go.dev/context",
			"Package context defines the Context type.",
			"Go Concurrency Patterns: Context",
			"https://go.dev/blog/context",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q, got:\n%s", want, out)
			}
		}
		// <b> highlighting must be stripped from snippets.
		if strings.Contains(out, "<b>") {
			t.Errorf("expected HTML tags stripped from snippet, got:\n%s", out)
		}
		// The redirector URL must never leak; only the real target should appear.
		if strings.Contains(out, "duckduckgo.com/l/") {
			t.Errorf("redirector URL leaked into output:\n%s", out)
		}
	})
}

func TestWebSearch_MaxResultsCap(t *testing.T) {
	var blocks []string
	for _, host := range []string{"a", "b", "c", "d", "e"} {
		blocks = append(blocks, ddgResult(
			"https%3A%2F%2Fexample.com%2F"+host,
			"Result "+host, "Snippet "+host))
	}
	page := ddgPage(blocks...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	withSearchEndpoint(t, srv.URL, func() {
		out := run(t, WebSearch{}, `{"query":"x","max_results":2}`)
		if !strings.Contains(out, "1. Result a") || !strings.Contains(out, "2. Result b") {
			t.Errorf("expected first two results, got:\n%s", out)
		}
		if strings.Contains(out, "Result c") {
			t.Errorf("max_results=2 not honored, got:\n%s", out)
		}
	})
}

func TestWebSearch_MaxResultsHardCap(t *testing.T) {
	// Asking for more than the hard cap must not exceed webSearchMaxResults.
	var blocks []string
	for i := 0; i < 15; i++ {
		id := string(rune('a' + i))
		blocks = append(blocks, ddgResult("https%3A%2F%2Fexample.com%2F"+id, "Result "+id, "Snip "+id))
	}
	page := ddgPage(blocks...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(page))
	}))
	defer srv.Close()

	withSearchEndpoint(t, srv.URL, func() {
		out := run(t, WebSearch{}, `{"query":"x","max_results":99}`)
		// Result number 11 (index 10) must not appear.
		if strings.Contains(out, "11. ") {
			t.Errorf("output exceeded hard cap of %d, got:\n%s", webSearchMaxResults, out)
		}
		if !strings.Contains(out, "10. ") {
			t.Errorf("expected %d results up to the hard cap, got:\n%s", webSearchMaxResults, out)
		}
	})
}

func TestWebSearch_EmptyResults(t *testing.T) {
	page := ddgPage() // container present, no result blocks
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	withSearchEndpoint(t, srv.URL, func() {
		out := run(t, WebSearch{}, `{"query":"asdfqwerzxcv"}`)
		if !strings.Contains(strings.ToLower(out), "no results") {
			t.Errorf("expected a no-results message, got:\n%s", out)
		}
	})
}

func TestWebSearch_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	withSearchEndpoint(t, srv.URL, func() {
		_, err := (WebSearch{}).Run(context.Background(), json.RawMessage(`{"query":"x"}`))
		if err == nil {
			t.Fatal("expected an error on non-2xx upstream response")
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("expected status in error, got: %v", err)
		}
	})
}

func TestWebSearch_MissingQuery(t *testing.T) {
	_, err := (WebSearch{}).Run(context.Background(), json.RawMessage(`{"query":"  "}`))
	if err == nil {
		t.Error("expected error when query is empty")
	}
}

func TestWebSearch_InvalidJSON(t *testing.T) {
	_, err := (WebSearch{}).Run(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error on invalid JSON input")
	}
}

func TestWebSearch_SnippetNotMisaligned(t *testing.T) {
	// A result with no snippet must not borrow the next result's snippet.
	noSnippet := `<div class="result">
  <h2 class="result__title">
    <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Ffirst&amp;rut=x">First</a>
  </h2>
</div>`
	withSnippet := ddgResult("https%3A%2F%2Fexample.com%2Fsecond", "Second", "Second snippet body")
	page := ddgPage(noSnippet, withSnippet)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(page))
	}))
	defer srv.Close()

	withSearchEndpoint(t, srv.URL, func() {
		out := run(t, WebSearch{}, `{"query":"x"}`)
		firstIdx := strings.Index(out, "1. First")
		secondSnippetIdx := strings.Index(out, "Second snippet body")
		if firstIdx < 0 || secondSnippetIdx < 0 {
			t.Fatalf("expected both results present, got:\n%s", out)
		}
		// The second result's snippet must appear after the second result's title,
		// not attached to the first result.
		secondTitleIdx := strings.Index(out, "2. Second")
		if secondSnippetIdx < secondTitleIdx {
			t.Errorf("snippet misaligned onto wrong result, got:\n%s", out)
		}
	})
}
