package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// --- foreground (regression coverage) ---

func TestBashForeground(t *testing.T) {
	b := Bash{}
	input, _ := json.Marshal(map[string]any{"command": "echo hello"})
	out, err := b.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got %q", out)
	}
}

func TestBashForegroundExitCode(t *testing.T) {
	b := Bash{}
	input, _ := json.Marshal(map[string]any{"command": "exit 42"})
	out, err := b.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "exit code: 42") {
		t.Errorf("expected 'exit code: 42' in output, got %q", out)
	}
}

func TestBashForegroundNoOutput(t *testing.T) {
	b := Bash{}
	input, _ := json.Marshal(map[string]any{"command": "true"})
	out, err := b.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "no output") {
		t.Errorf("expected 'no output' sentinel, got %q", out)
	}
}

func TestBashEmptyCommand(t *testing.T) {
	b := Bash{}
	input, _ := json.Marshal(map[string]any{"command": "   "})
	_, err := b.Run(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "empty command") {
		t.Errorf("expected 'empty command' error, got %v", err)
	}
}

func TestBashInvalidInput(t *testing.T) {
	b := Bash{}
	_, err := b.Run(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

func TestCommandExtract(t *testing.T) {
	input, _ := json.Marshal(map[string]any{"command": "echo hi"})
	if got := Command(input); got != "echo hi" {
		t.Errorf("Command() = %q, want 'echo hi'", got)
	}
}

func TestCommandExtractEmpty(t *testing.T) {
	if got := Command(json.RawMessage(`{}`)); got != "" {
		t.Errorf("Command() on missing field = %q, want ''", got)
	}
}

// --- background execution ---

func TestBashBackgroundReturnsImmediately(t *testing.T) {
	globalJobs = &JobRegistry{}
	t.Cleanup(func() { globalJobs = &JobRegistry{} })

	b := Bash{}
	input, _ := json.Marshal(map[string]any{
		"command":           "sleep 10",
		"run_in_background": true,
	})
	out, err := b.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "job") {
		t.Errorf("expected job confirmation in output, got %q", out)
	}
}

func TestBashBackgroundOutputCaptured(t *testing.T) {
	globalJobs = &JobRegistry{}
	t.Cleanup(func() { globalJobs = &JobRegistry{} })

	b := Bash{}
	input, _ := json.Marshal(map[string]any{
		"command":           "echo bg-done",
		"run_in_background": true,
	})
	_, err := b.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jobs := globalJobs.List()
	if len(jobs) == 0 {
		t.Fatal("no jobs registered")
	}
	j := jobs[0]

	waitForStatus(t, j, 5*time.Second)

	if j.StatusSnapshot() != JobDone {
		t.Errorf("job status = %v, want done", j.StatusSnapshot())
	}
	if !strings.Contains(j.Output(), "bg-done") {
		t.Errorf("job output = %q, want 'bg-done'", j.Output())
	}
}

func TestBashBackgroundFailure(t *testing.T) {
	globalJobs = &JobRegistry{}
	t.Cleanup(func() { globalJobs = &JobRegistry{} })

	b := Bash{}
	input, _ := json.Marshal(map[string]any{
		"command":           "exit 5",
		"run_in_background": true,
	})
	_, err := b.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jobs := globalJobs.List()
	if len(jobs) == 0 {
		t.Fatal("no jobs registered")
	}
	j := jobs[0]
	waitForStatus(t, j, 5*time.Second)

	if j.StatusSnapshot() != JobFailed {
		t.Errorf("job status = %v, want failed", j.StatusSnapshot())
	}
	if j.ExitCode() != 5 {
		t.Errorf("job exit code = %d, want 5", j.ExitCode())
	}
}

func TestBashBackgroundStderrCaptured(t *testing.T) {
	globalJobs = &JobRegistry{}
	t.Cleanup(func() { globalJobs = &JobRegistry{} })

	b := Bash{}
	input, _ := json.Marshal(map[string]any{
		"command":           "echo err-line >&2",
		"run_in_background": true,
	})
	b.Run(context.Background(), input)

	jobs := globalJobs.List()
	if len(jobs) == 0 {
		t.Fatal("no jobs registered")
	}
	waitForStatus(t, jobs[0], 5*time.Second)

	if !strings.Contains(jobs[0].Output(), "err-line") {
		t.Errorf("stderr should be captured, got %q", jobs[0].Output())
	}
}

// --- JobRegistry ---

func TestJobRegistryGetByID(t *testing.T) {
	reg := &JobRegistry{}
	j := reg.add("echo hi")
	got := reg.Get(j.ID)
	if got != j {
		t.Error("Get should return the same job by ID")
	}
	if reg.Get("99999") != nil {
		t.Error("Get for unknown ID should return nil")
	}
}

func TestJobRegistryListOrder(t *testing.T) {
	reg := &JobRegistry{}
	reg.add("cmd1")
	reg.add("cmd2")
	reg.add("cmd3")

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(list))
	}
	// List returns newest first.
	if list[0].Command != "cmd3" {
		t.Errorf("first entry should be newest, got %q", list[0].Command)
	}
	if list[2].Command != "cmd1" {
		t.Errorf("last entry should be oldest, got %q", list[2].Command)
	}
}

func TestJobRegistryListEmpty(t *testing.T) {
	reg := &JobRegistry{}
	if got := reg.List(); len(got) != 0 {
		t.Errorf("empty registry List should return empty slice, got %v", got)
	}
}

func TestReplaceJobs(t *testing.T) {
	orig := globalJobs
	newReg := &JobRegistry{}
	restore := ReplaceJobs(newReg)
	if globalJobs != newReg {
		t.Error("ReplaceJobs should swap globalJobs")
	}
	restore()
	if globalJobs != orig {
		t.Error("restore function should reset globalJobs to original")
	}
}

func TestJobsReturnsGlobal(t *testing.T) {
	if Jobs() != globalJobs {
		t.Error("Jobs() should return globalJobs")
	}
}

// --- helpers ---

// waitForStatus polls j until it leaves JobRunning or the deadline is hit.
func waitForStatus(t *testing.T, j *Job, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if j.StatusSnapshot() != JobRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s still running after %s", j.ID, timeout)
}
