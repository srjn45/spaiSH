package spaish_test

import (
	"testing"

	"spaios/internal/spaish"
)

func TestIsNaturalLanguage(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ls -la", false},              // binary in PATH
		{"cd /tmp", false},             // known built-in
		{"export FOO=bar", false},      // known built-in
		{"echo hello", false},          // known built-in
		{"git status", false},          // binary in PATH
		{"? what is my disk usage", false}, // ? prefix handled by caller
		{"", false},                    // empty
		{"show me disk usage", true},   // NL: not in PATH, not a built-in
		{"how do I find large files", true}, // NL
		{"restart nginx please", true}, // "restart" not typically in PATH
	}
	for _, tt := range tests {
		got := spaish.IsNaturalLanguage(tt.input)
		if got != tt.want {
			t.Errorf("IsNaturalLanguage(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRingDetectAlias(t *testing.T) {
	r := &spaish.Ring{}
	cmd := "git push origin main"
	for i := 0; i < 3; i++ {
		r.Add(cmd)
	}
	result := r.Detect(3)
	if result == nil {
		t.Fatal("expected a pattern result, got nil")
	}
	if result.Kind != "alias" {
		t.Errorf("Kind: got %q, want %q", result.Kind, "alias")
	}
	if result.Count != 3 {
		t.Errorf("Count: got %d, want 3", result.Count)
	}
}

func TestRingDetectLongCommand(t *testing.T) {
	r := &spaish.Ring{}
	// Command longer than 60 chars
	cmd := "kubectl get pods --namespace production --output wide --watch"
	for i := 0; i < 3; i++ {
		r.Add(cmd)
	}
	result := r.Detect(3)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Kind != "long" {
		t.Errorf("Kind: got %q, want %q", result.Kind, "long")
	}
}

func TestRingDetectScript(t *testing.T) {
	r := &spaish.Ring{}
	seq := []string{"git add .", "git commit -m 'wip'", "git push origin main"}
	// Repeat the sequence 3 times
	for i := 0; i < 3; i++ {
		for _, c := range seq {
			r.Add(c)
		}
	}
	result := r.Detect(3)
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Kind != "script" {
		t.Errorf("Kind: got %q, want %q", result.Kind, "script")
	}
	if len(result.Commands) != 3 {
		t.Errorf("Commands len: got %d, want 3", len(result.Commands))
	}
}

func TestRingDetectNone(t *testing.T) {
	r := &spaish.Ring{}
	r.Add("ls")
	r.Add("pwd")
	r.Add("git status")
	if result := r.Detect(3); result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}
