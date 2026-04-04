package executor_test

import (
	"strings"
	"testing"

	"spaios/internal/executor"
)

func TestExecuteSimpleCommand(t *testing.T) {
	var out strings.Builder
	err := executor.Execute("echo hello", &out)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("expected 'hello' in output, got %q", out.String())
	}
}

func TestExecuteCaputuresStderr(t *testing.T) {
	var out strings.Builder
	// ls on a nonexistent path writes to stderr
	executor.Execute("ls /nonexistent_path_xyz 2>&1", &out)
	if out.Len() == 0 {
		t.Error("expected stderr output, got nothing")
	}
}

func TestExecuteFailingCommand(t *testing.T) {
	var out strings.Builder
	err := executor.Execute("exit 1", &out)
	if err == nil {
		t.Error("expected error for failing command")
	}
}
