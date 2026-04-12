package spaish_test

import (
	"strings"
	"testing"

	"spaios/internal/spaish"
)

func TestParseMarker(t *testing.T) {
	tests := []struct {
		input    string
		exitCode int
		cwd      string
		ok       bool
	}{
		{"SPAISH:0:/home/user", 0, "/home/user", true},
		{"SPAISH:1:/var/log", 1, "/var/log", true},
		{"SPAISH:127:/home/user/my project", 127, "/home/user/my project", true},
		{"SPAISH:0:/path/with:colon", 0, "/path/with:colon", true},
		{"invalid", 0, "", false},
		{"SPAISH:notanumber:/home", 0, "", false},
		{"SPAISH:", 0, "", false},
	}
	for _, tt := range tests {
		ec, cwd, ok := spaish.ParseMarker(tt.input)
		if ok != tt.ok {
			t.Errorf("ParseMarker(%q): ok=%v want %v", tt.input, ok, tt.ok)
			continue
		}
		if ok && ec != tt.exitCode {
			t.Errorf("ParseMarker(%q): exitCode=%d want %d", tt.input, ec, tt.exitCode)
		}
		if ok && cwd != tt.cwd {
			t.Errorf("ParseMarker(%q): cwd=%q want %q", tt.input, cwd, tt.cwd)
		}
	}
}

func TestTailTrim(t *testing.T) {
	// Short string — unchanged
	s := "hello world"
	if got := spaish.TailTrim(s, 8192); got != s {
		t.Errorf("short: got %q, want unchanged", got)
	}

	// Long string — keep last N bytes
	long := strings.Repeat("x", 10000)
	got := spaish.TailTrim(long, 8192)
	if len(got) != 8192 {
		t.Errorf("len: got %d, want 8192", len(got))
	}
	if got != long[len(long)-8192:] {
		t.Error("wrong tail selected")
	}
}

func TestHookScript(t *testing.T) {
	bash := spaish.HookScript("/bin/bash")
	if !strings.Contains(bash, "PROMPT_COMMAND") {
		t.Error("bash hook should use PROMPT_COMMAND")
	}
	if !strings.Contains(bash, "SPAISH") {
		t.Error("bash hook should contain SPAISH marker")
	}

	zsh := spaish.HookScript("/usr/bin/zsh")
	if !strings.Contains(zsh, "precmd_functions") {
		t.Error("zsh hook should use precmd_functions")
	}
}
