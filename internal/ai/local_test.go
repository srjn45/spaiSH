package ai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spaios/internal/ai"
)

func TestLocalProviderComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hi"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":" there"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	if !p.Available() {
		t.Fatal("Available() = false, expected true")
	}

	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "user", Content: "say hi"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		result.WriteString(chunk)
	}
	if result.String() != "Hi there" {
		t.Errorf("got %q, want %q", result.String(), "Hi there")
	}
}
