package main

import (
	"os"
	"testing"
	"time"

	"spaios/internal/session"
)

func TestResolveSessionIDFlagWins(t *testing.T) {
	t.Setenv("SPAI_SESSION_ID", "env-val")
	result := resolveSessionID("flag-val")
	if result != "flag-val" {
		t.Errorf("expected flag-val, got %q", result)
	}
}

func TestResolveSessionIDEnvFallback(t *testing.T) {
	t.Setenv("SPAI_SESSION_ID", "env-val")
	result := resolveSessionID("")
	if result != "env-val" {
		t.Errorf("expected env-val, got %q", result)
	}
}

func TestResolveSessionIDDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir) // ensure no real pinned_session leaks in
	t.Setenv("SPAI_SESSION_ID", "")
	result := resolveSessionID("")
	if result != "default" {
		t.Errorf("expected 'default', got %q", result)
	}
}

func TestResolveSessionIDPinnedFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("SPAI_SESSION_ID", "")

	if err := session.WritePinned("infra"); err != nil {
		t.Fatalf("WritePinned error: %v", err)
	}

	result := resolveSessionID("")
	if result != "infra" {
		t.Errorf("expected pinned session 'infra', got %q", result)
	}
}

func TestReadStdinPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("hello from pipe\n")
	w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin; r.Close() }()

	result := readStdin()
	if result != "hello from pipe\n" {
		t.Errorf("expected pipe content, got %q", result)
	}
}

func TestReadStdinTruncatesAt64KB(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// Write data in a goroutine to avoid blocking on the pipe
	go func() {
		defer w.Close()
		big := make([]byte, 64*1024+100)
		for i := range big {
			big[i] = 'x'
		}
		w.Write(big)
	}()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin; r.Close() }()

	result := readStdin()
	expectedLen := 64*1024 + len("[truncated]")
	if len(result) != expectedLen {
		t.Errorf("expected length %d, got %d", expectedLen, len(result))
	}
	if result[len(result)-len("[truncated]"):] != "[truncated]" {
		t.Errorf("expected [truncated] suffix")
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{3 * 24 * time.Hour, "3d ago"},
	}
	for _, c := range cases {
		got := formatRelativeTime(now.Add(-c.d))
		if got != c.want {
			t.Errorf("formatRelativeTime(now-%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
