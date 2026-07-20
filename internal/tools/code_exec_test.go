package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// requireInterpreter skips the test when the language's interpreter is not
// installed, so CI machines missing python3/node pass cleanly rather than fail.
func requireInterpreter(t *testing.T, language string) {
	t.Helper()
	if _, _, _, err := interpreterFor(language); err != nil {
		t.Skipf("skipping: %v", err)
	}
}

// codeExecInput builds a valid JSON input, escaping code safely via json.Marshal.
func codeExecInput(t *testing.T, language, code string, timeoutSeconds int) json.RawMessage {
	t.Helper()
	in := map[string]any{"language": language, "code": code}
	if timeoutSeconds > 0 {
		in["timeout_seconds"] = timeoutSeconds
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCodeExecSuccess(t *testing.T) {
	cases := []struct {
		name     string
		language string
		code     string
		want     string
	}{
		{"python", "python", "print('hello from python')", "hello from python"},
		{"node", "node", "console.log('hello from node')", "hello from node"},
		{"javascript alias", "javascript", "console.log(1 + 2)", "3"},
		{"ruby", "ruby", "puts 'hello from ruby'", "hello from ruby"},
		{"go", "go", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello from go\") }", "hello from go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireInterpreter(t, tc.language)
			out, err := CodeExec{}.Run(context.Background(), codeExecInput(t, tc.language, tc.code, 0))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output %q does not contain %q", out, tc.want)
			}
		})
	}
}

func TestCodeExecNonZeroExit(t *testing.T) {
	cases := []struct {
		name     string
		language string
		code     string
	}{
		{"python raises", "python", "import sys; sys.stderr.write('boom\\n'); sys.exit(3)"},
		{"node throws", "node", "console.error('boom'); process.exit(3)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireInterpreter(t, tc.language)
			out, err := CodeExec{}.Run(context.Background(), codeExecInput(t, tc.language, tc.code, 0))
			if err == nil {
				t.Fatalf("expected error for non-zero exit, got output %q", out)
			}
			// The captured stderr and exit code must be surfaced in the error.
			if !strings.Contains(err.Error(), "boom") {
				t.Errorf("error %q does not include stderr output", err)
			}
			if !strings.Contains(err.Error(), "code 3") {
				t.Errorf("error %q does not include exit code", err)
			}
		})
	}
}

func TestCodeExecTimeout(t *testing.T) {
	cases := []struct {
		name     string
		language string
		code     string
	}{
		{"python infinite loop", "python", "while True: pass"},
		{"node infinite loop", "node", "while (true) {}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireInterpreter(t, tc.language)
			// A 1s cap keeps the test fast while still exercising the kill path.
			_, err := CodeExec{}.Run(context.Background(), codeExecInput(t, tc.language, tc.code, 1))
			if err == nil {
				t.Fatal("expected timeout error, got nil")
			}
			if !strings.Contains(err.Error(), "timed out") {
				t.Errorf("error %q is not a timeout", err)
			}
		})
	}
}

func TestCodeExecTimeoutClamp(t *testing.T) {
	// A request above the cap must be clamped, not honored verbatim.
	if got := clampTimeoutSeconds(9999); got != maxCodeExecTimeout {
		t.Errorf("clamp(9999) = %v, want %v", got, maxCodeExecTimeout)
	}
	if got := clampTimeoutSeconds(0); got != defaultCodeExecTimeout {
		t.Errorf("clamp(0) = %v, want default %v", got, defaultCodeExecTimeout)
	}
	if got := clampTimeoutSeconds(2); got != 2*time.Second {
		t.Errorf("clamp(2) = %v, want 2s", got)
	}
}

func TestCodeExecUnsupportedLanguage(t *testing.T) {
	_, err := CodeExec{}.Run(context.Background(),
		json.RawMessage(`{"language":"rust","code":"fn main() {}"}`))
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("error %q is not an unsupported-language error", err)
	}
}

func TestCodeExecEmptyCode(t *testing.T) {
	if _, err := (CodeExec{}).Run(context.Background(),
		json.RawMessage(`{"language":"python","code":"  "}`)); err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestCodeExecTempDirCleanup(t *testing.T) {
	requireInterpreter(t, "python")
	// The script prints its own working directory (the tool's temp dir). After
	// Run returns, that directory must no longer exist.
	out, err := CodeExec{}.Run(context.Background(),
		codeExecInput(t, "python", "import os; print(os.getcwd())", 0))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tmp := strings.TrimSpace(out)
	if tmp == "" {
		t.Fatal("expected the temp dir path in output")
	}
	if _, statErr := os.Stat(tmp); !os.IsNotExist(statErr) {
		t.Errorf("temp dir %q was not cleaned up (stat err: %v)", tmp, statErr)
	}
}

func TestInterpreterForNotFound(t *testing.T) {
	// Sanity: when neither python3 nor python is on PATH, resolution must error.
	_, py3 := exec.LookPath("python3")
	_, py := exec.LookPath("python")
	if py3 != nil && py != nil {
		if _, _, _, e := interpreterFor("python"); e == nil {
			t.Error("expected not-found error when python is absent")
		}
	}
}
