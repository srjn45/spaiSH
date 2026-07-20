package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"spaish/internal/sandbox"
)

const maxToolOutput = 16 * 1024

// JobStatus is the lifecycle state of a background bash job.
type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

// Job tracks a single background bash process.
// ID and Command are set on creation and are immutable after that; all other
// fields are protected by mu.
type Job struct {
	ID      string
	Command string

	mu       sync.Mutex
	status   JobStatus
	exitCode int
	buf      bytes.Buffer
}

// StatusSnapshot returns the current lifecycle state.
func (j *Job) StatusSnapshot() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

// ExitCode returns the process exit code (only meaningful when done or failed).
func (j *Job) ExitCode() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.exitCode
}

// Output returns a snapshot of combined stdout+stderr captured so far.
func (j *Job) Output() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.buf.String()
}

// jobWriter adapts a *Job to io.Writer, routing writes into its output buffer.
type jobWriter struct{ j *Job }

func (w jobWriter) Write(p []byte) (int, error) {
	w.j.mu.Lock()
	defer w.j.mu.Unlock()
	return w.j.buf.Write(p)
}

// JobRegistry is an in-memory store of background jobs keyed by auto-assigned ID.
type JobRegistry struct {
	mu   sync.Mutex
	jobs []*Job
	seq  int
}

// add creates a new running job, registers it, and returns it.
func (r *JobRegistry) add(command string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	j := &Job{
		ID:      strconv.Itoa(r.seq),
		Command: command,
		status:  JobRunning,
	}
	r.jobs = append(r.jobs, j)
	return j
}

// List returns a snapshot of all registered jobs, newest first.
func (r *JobRegistry) List() []*Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Job, len(r.jobs))
	for i, j := range r.jobs {
		out[len(r.jobs)-1-i] = j
	}
	return out
}

// Get returns the job with the given string ID, or nil if not found.
func (r *JobRegistry) Get(id string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// globalJobs is the process-wide job registry used by background bash invocations
// and read by the /jobs REPL command.
var globalJobs = &JobRegistry{}

// Jobs returns the global job registry.
func Jobs() *JobRegistry { return globalJobs }

// ReplaceJobs swaps the process-wide job registry and returns a restore function.
// Intended for use in tests only.
func ReplaceJobs(r *JobRegistry) func() {
	saved := globalJobs
	globalJobs = r
	return func() { globalJobs = saved }
}

// Bash runs a shell command and returns its combined stdout+stderr.
//
// When Sandbox is set and enabled, the child process is wrapped in the execution
// sandbox unless Trusted reports the command as explicitly blessed (the
// allow_commands carve-out). Sandbox setup that fails is fatal: the command is
// refused rather than run unsandboxed. Both fields are nil-safe — a zero-value
// Bash sandboxes nothing, matching the pre-sandbox behavior.
type Bash struct {
	// Sandbox restricts the spawned process. nil (or a disabled sandbox) means
	// no wrapping.
	Sandbox sandbox.Sandbox
	// Trusted reports whether a command is exempt from the sandbox. nil means
	// nothing is trusted (every command is wrapped when the sandbox is enabled).
	Trusted func(cmd string) bool
}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Execute a shell command via `bash -c` and return its combined " +
		"stdout and stderr. Use non-interactive commands only (no vim, nano, top, " +
		"less, or anything that waits for input). Chain steps with && when needed. " +
		"Set run_in_background=true for long-running commands; use /jobs in the REPL " +
		"to inspect output and status."
}

func (Bash) Schema() map[string]any {
	return objectSchema(map[string]any{
		"command": strProp("The shell command to run."),
		"run_in_background": map[string]any{
			"type":        "boolean",
			"description": "When true, start the command in the background and return immediately with a job id. The command is classified and gated identically to foreground commands. Use /jobs in the REPL to inspect output and status.",
		},
	}, "command")
}

func (b Bash) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", fmt.Errorf("empty command")
	}
	if args.RunInBackground {
		return b.runBackground(args.Command)
	}
	return b.runForeground(ctx, args.Command)
}

func (b Bash) runForeground(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out

	// Containment is applied here, AFTER the permission gate has already allowed
	// this command to run — it never alters classification or confirmation.
	// Commands the user has explicitly blessed (Trusted) bypass the sandbox.
	if b.Sandbox != nil && b.Sandbox.Enabled() &&
		!(b.Trusted != nil && b.Trusted(command)) {
		if err := b.Sandbox.Wrap(cmd); err != nil {
			return "", fmt.Errorf("sandbox setup failed (refusing to run unsandboxed): %w", err)
		}
	}

	err := cmd.Run()

	result := tailTrim(out.String(), maxToolOutput)
	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		// Report failure as content (not a Go error) so the model sees the
		// output and exit code and can diagnose.
		return fmt.Sprintf("%s\n[exit code: %d]", result, exitCode), nil
	}
	if result == "" {
		result = "[no output; exit code 0]"
	}
	return result, nil
}

// runBackground starts command without waiting for it to complete. The command
// runs detached from the caller's context so cancellation of the agent turn
// does not kill the background process. Output is streamed into the job's
// in-memory buffer and the job status is updated by a goroutine when the
// process exits.
func (b Bash) runBackground(command string) (string, error) {
	job := globalJobs.add(command)

	cmd := exec.Command("bash", "-c", command)
	w := jobWriter{j: job}
	cmd.Stdout = w
	cmd.Stderr = w

	if b.Sandbox != nil && b.Sandbox.Enabled() &&
		!(b.Trusted != nil && b.Trusted(command)) {
		if err := b.Sandbox.Wrap(cmd); err != nil {
			job.mu.Lock()
			job.status = JobFailed
			job.exitCode = -1
			job.mu.Unlock()
			return "", fmt.Errorf("sandbox setup failed (refusing to run unsandboxed): %w", err)
		}
	}

	if err := cmd.Start(); err != nil {
		job.mu.Lock()
		job.status = JobFailed
		job.exitCode = -1
		job.mu.Unlock()
		return "", fmt.Errorf("start failed: %w", err)
	}

	go func() {
		err := cmd.Wait()
		job.mu.Lock()
		defer job.mu.Unlock()
		if err != nil {
			job.status = JobFailed
			job.exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				job.exitCode = exitErr.ExitCode()
			}
		} else {
			job.status = JobDone
			job.exitCode = 0
		}
	}()

	return fmt.Sprintf("[job %s started]  command: %s\nUse /jobs to monitor output and status.", job.ID, command), nil
}

// Command extracts the command string from a bash tool call input, for display
// and permission classification.
func Command(input json.RawMessage) string {
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &args)
	return args.Command
}
