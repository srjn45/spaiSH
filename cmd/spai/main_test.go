package main

import (
	"os"
	"testing"
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
	t.Setenv("SPAI_SESSION_ID", "")
	result := resolveSessionID("")
	if result != "default" {
		t.Errorf("expected 'default', got %q", result)
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
