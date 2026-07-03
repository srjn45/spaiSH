package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Code execution timeouts. The model may request a shorter timeout, but never a
// longer one: the cap bounds how long a runaway script can hold a subprocess.
const (
	defaultCodeExecTimeout = 15 * time.Second
	maxCodeExecTimeout     = 30 * time.Second
)

// CodeExec runs a short snippet of Python or Node code as an ephemeral
// subprocess in a fresh temporary directory, with a hard timeout.
//
// This is NOT a security sandbox. The code runs with the exact same OS
// privileges as the spai process itself — identical to the `bash` tool. There
// is no seccomp filter, no container, no chroot, no namespace, and no resource
// limit beyond a wall-clock timeout. The only isolation is a throwaway working
// directory (so scratch files don't pollute the project) that is deleted after
// the run. Treat code_exec as exactly as dangerous as bash; it is classified at
// the same top tier for that reason.
type CodeExec struct{}

func (CodeExec) Name() string { return "code_exec" }

func (CodeExec) Description() string {
	return "Execute a short snippet of Python or Node/JavaScript code as an " +
		"ephemeral subprocess and return its combined stdout and stderr. The " +
		"code runs in a fresh temporary directory that is deleted afterward, so " +
		"you may write scratch files freely without touching the project. A hard " +
		"timeout (default 15s, max 30s) kills runaway or infinite scripts.\n\n" +
		"SECURITY: This is NOT a sandbox. The code runs with the same privileges " +
		"as this agent — identical to the `bash` tool: full filesystem, network, " +
		"and process access. It is not seccomp/container/chroot isolated. The only " +
		"differences from bash are a throwaway working directory and a timeout. " +
		"Do not treat it as a safety boundary.\n\n" +
		"Fields: `language` (one of \"python\", \"node\", \"javascript\") and " +
		"`code` (the source to run). Optional `timeout_seconds` shortens the " +
		"timeout; values above the 30s cap are clamped. Use for quick " +
		"computation, data munging, or prototyping — not for long-running or " +
		"interactive programs."
}

func (CodeExec) Schema() map[string]any {
	return objectSchema(map[string]any{
		"language": strProp("The language to run: \"python\", \"node\", or \"javascript\"."),
		"code":     strProp("The source code to execute."),
		"timeout_seconds": map[string]any{
			"type":        "integer",
			"description": fmt.Sprintf("Optional wall-clock timeout in seconds. Defaults to %d, capped at %d.", int(defaultCodeExecTimeout.Seconds()), int(maxCodeExecTimeout.Seconds())),
		},
	}, "language", "code")
}

func (CodeExec) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Language       string `json:"language"`
		Code           string `json:"code"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if strings.TrimSpace(args.Code) == "" {
		return "", fmt.Errorf("empty code")
	}

	bin, ext, err := interpreterFor(args.Language)
	if err != nil {
		return "", err
	}

	timeout := clampTimeoutSeconds(args.TimeoutSeconds)

	// Fresh throwaway working directory, removed when we return. The subprocess
	// runs with this as its cwd so scratch files land here, not in the project.
	dir, err := os.MkdirTemp("", "spai-code-exec-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	scriptPath := filepath.Join(dir, "main"+ext)
	if err := os.WriteFile(scriptPath, []byte(args.Code), 0o600); err != nil {
		return "", fmt.Errorf("write script: %w", err)
	}

	// Derive a timeout context from the caller's so both the agent's cancellation
	// and our hard cap can kill the process (exec.CommandContext handles the kill).
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, scriptPath)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()

	result := tailTrim(out.String(), maxToolOutput)

	// Timeout: report as an error so the model knows the run was cut short rather
	// than completing. Check the timeout context, not the parent's cancellation.
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("execution timed out after %s (killed)\n%s", timeout, result)
	}
	if runErr != nil {
		exitCode := 1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return "", fmt.Errorf("exited with code %d\n%s", exitCode, result)
	}
	if result == "" {
		result = "[no output; exit code 0]"
	}
	return result, nil
}

// clampTimeoutSeconds converts a requested timeout (in seconds) to a duration,
// falling back to the default for non-positive requests and clamping anything
// above the cap. The cap is what stops the model from requesting an unbounded
// (or absurdly long) run.
func clampTimeoutSeconds(sec int) time.Duration {
	if sec <= 0 {
		return defaultCodeExecTimeout
	}
	d := time.Duration(sec) * time.Second
	if d > maxCodeExecTimeout {
		return maxCodeExecTimeout
	}
	return d
}

// interpreterFor resolves the interpreter binary and script extension for a
// language. It returns a friendly error for unsupported languages and for
// supported ones whose interpreter is not installed on PATH.
func interpreterFor(language string) (bin, ext string, err error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "python", "python3", "py":
		for _, cand := range []string{"python3", "python"} {
			if p, e := exec.LookPath(cand); e == nil {
				return p, ".py", nil
			}
		}
		return "", "", fmt.Errorf("python interpreter not found on PATH")
	case "node", "javascript", "js":
		if p, e := exec.LookPath("node"); e == nil {
			return p, ".js", nil
		}
		return "", "", fmt.Errorf("node interpreter not found on PATH")
	default:
		return "", "", fmt.Errorf("unsupported language %q (supported: python, node/javascript)", language)
	}
}
