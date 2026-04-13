package spaish_test

import (
	"testing"

	"spaios/internal/spaish"
)

func TestReadLine(t *testing.T) {
	tests := []struct {
		name     string
		chunks   [][]byte
		wantLine string
		wantOK   bool
	}{
		{
			name:     "single chunk with newline",
			chunks:   [][]byte{[]byte("hello\n")},
			wantLine: "hello",
			wantOK:   true,
		},
		{
			name:     "CRLF newline stripped",
			chunks:   [][]byte{[]byte("hello\r\n")},
			wantLine: "hello",
			wantOK:   true,
		},
		{
			name:     "multiple chunks before newline",
			chunks:   [][]byte{[]byte("hel"), []byte("lo\n")},
			wantLine: "hello",
			wantOK:   true,
		},
		{
			name:     "channel closed before newline",
			chunks:   [][]byte{[]byte("partial")},
			wantLine: "",
			wantOK:   false,
		},
		{
			name:     "empty line (bare newline)",
			chunks:   [][]byte{[]byte("\n")},
			wantLine: "",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan []byte, len(tt.chunks)+1)
			for _, c := range tt.chunks {
				ch <- c
			}
			if !tt.wantOK {
				close(ch) // simulate closed channel
			} else {
				// close after sending so ReadLine can drain then see newline
				go func() { close(ch) }()
			}

			got, ok := spaish.ReadLine(ch)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantLine {
				t.Errorf("line = %q, want %q", got, tt.wantLine)
			}
		})
	}
}

func TestIsNaturalLanguage(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ls -la", false},                                  // binary in PATH
		{"cd /tmp", false},                                 // known built-in
		{"export FOO=bar", false},                          // known built-in
		{"echo hello", false},                              // known built-in
		{"git status", false},                              // binary in PATH
		{"? what is my disk usage", false},                 // ? prefix handled by caller
		{"", false},                                        // empty
		{"show me disk usage", true},                       // NL: not in PATH, not a built-in
		{"how do I find large files", true},                // NL
		{"restart nginx please", true},                     // "restart" not typically in PATH
		{"pwd; printf 'hello'", false},                     // semicolon → shell command
		{"ls | grep foo", false},                           // pipe → shell command
		{"echo hi && echo there", false},                   // && → shell command
		{"cat file > out.txt", false},                      // redirect → shell command
		{"ls /etc/hostname; printf '\\nMARKER\\n'", false}, // sentinel command form
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
