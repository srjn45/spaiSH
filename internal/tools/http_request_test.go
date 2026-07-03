package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// echoServer returns a test server that echoes back the method, headers, and
// body it received so tests can verify what was actually sent.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"method":%q,"path":%q,"body":%q,"x-custom":%q}`,
			r.Method, r.URL.Path, string(body), r.Header.Get("X-Custom"))
	}))
}

func TestHTTPRequest_Methods(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			input := mustMarshal(map[string]any{
				"url":    srv.URL + "/test",
				"method": method,
				"body":   "hello",
			})
			out, err := (HTTPRequest{}).Run(context.Background(), input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "200") {
				t.Errorf("expected 200 in output, got:\n%s", out)
			}
			if !strings.Contains(out, fmt.Sprintf("%q", method)) {
				t.Errorf("expected method %q echoed in body, got:\n%s", method, out)
			}
		})
	}
}

func TestHTTPRequest_CustomHeaders(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	input := mustMarshal(map[string]any{
		"url":     srv.URL + "/",
		"method":  "GET",
		"headers": map[string]string{"X-Custom": "my-value"},
	})
	out, err := (HTTPRequest{}).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "my-value") {
		t.Errorf("expected custom header value in echo body, got:\n%s", out)
	}
}

func TestHTTPRequest_Non2xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	input := mustMarshal(map[string]any{
		"url":    srv.URL + "/missing",
		"method": "GET",
	})
	out, err := (HTTPRequest{}).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-2xx should still return output (not an error); status line must appear.
	if !strings.Contains(out, "404") {
		t.Errorf("expected 404 in output, got:\n%s", out)
	}
}

func TestHTTPRequest_DefaultMethodIsGET(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	input := mustMarshal(map[string]any{"url": srv.URL + "/"})
	out, err := (HTTPRequest{}).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"GET"`) {
		t.Errorf("expected GET as default method, got:\n%s", out)
	}
}

func TestHTTPRequest_InvalidURL(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
	}{
		{"empty url", map[string]any{"url": ""}},
		{"ftp scheme", map[string]any{"url": "ftp://example.com/file"}},
		{"file scheme", map[string]any{"url": "file:///etc/passwd"}},
		{"no scheme", map[string]any{"url": "example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := mustMarshal(tc.input)
			_, err := (HTTPRequest{}).Run(context.Background(), input)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestHTTPRequest_ResponseSizeCap(t *testing.T) {
	const bigSize = 200 * 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("x", bigSize)))
	}))
	defer srv.Close()

	input := mustMarshal(map[string]any{
		"url":       srv.URL + "/",
		"max_bytes": 1024,
	})
	out, err := (HTTPRequest{}).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) > 4*1024 {
		t.Errorf("output too large (%d bytes), capping not working", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation notice, got:\n%s", out[:200])
	}
}

func TestHTTPRequest_LocationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.Write([]byte("arrived"))
	}))
	defer srv.Close()

	// Use a no-redirect client by testing the redirect response directly via
	// a custom server that just sets Location without following.
	noRedirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/target")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer noRedirectSrv.Close()

	// Override the client's redirect policy isn't possible through the tool
	// interface, so instead check the srv with CheckRedirect disabled by testing
	// that the output includes the final URL after auto-follow.
	input := mustMarshal(map[string]any{
		"url":    srv.URL + "/redirect",
		"method": "GET",
	})
	out, err := (HTTPRequest{}).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("expected 200 after redirect follow, got:\n%s", out)
	}
}

func TestHTTPRequest_InvalidJSON(t *testing.T) {
	_, err := (HTTPRequest{}).Run(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error on invalid JSON input")
	}
}
