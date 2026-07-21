// Package hooks implements user-configured shell hooks run AROUND tool
// execution: a pre_tool hook may refuse an already-approved tool call, and a
// post_tool hook observes a successful call. Hooks are layered on top of (never
// in place of) the permission gate — see the CRITICAL permission invariant in
// docs/superpowers/specs/2026-07-21-wp3.1-hooks-design.md.
//
// A hook command is arbitrary shell run via `sh -c` with the user's full
// privileges: the same trust level as SPAI.md and [permissions].allow_commands.
// Hooks are the operator's own code, NOT a sandbox against the model, and a
// pre_tool hook can only ever refuse a call the user already approved — it can
// never auto-approve, satisfy a confirm prompt, or change a tool's tier.
//
// The package uses only os/exec + `sh -c` and no OS-specific syscalls, so it
// cross-compiles to linux/arm64 and darwin/arm64 with no build tags.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"spaish/internal/config"
)

// Event is the lifecycle point a hook fires at.
type Event string

const (
	PreTool  Event = "pre_tool"  // before a tool runs; may block
	PostTool Event = "post_tool" // after a tool succeeds; observe-only
)

// defaultTimeout is applied when a HookSpec sets no (or a non-positive)
// timeout_ms. It bounds an otherwise-unbounded hook command.
const defaultTimeout = 30 * time.Second

// Invocation is the tool metadata handed to a hook. Output/IsError are set for
// post_tool only.
type Invocation struct {
	Tool    string          // e.g. "write_file", "mcp__github__create_issue"
	Input   json.RawMessage // raw tool-call JSON input
	Output  string          // post_tool: the tool's textual output
	IsError bool            // post_tool: always false (post runs only on success)
}

// Hook is one compiled, validated user hook.
type Hook struct {
	Event      Event
	namePat    string         // glob for path.Match against Invocation.Tool
	inputField string         // optional top-level JSON key to test
	inputRe    *regexp.Regexp // optional RE2 pattern; nil = no input constraint
	Command    string         // shell, run via `sh -c`
	Timeout    time.Duration  // hard kill after this
}

// Runner is the per-session compiled hook set. The zero value has no hooks and
// its Run* methods are no-ops, so an unset agent.Config.Hooks reproduces legacy
// behaviour exactly (mirrors permissions.Policy's zero value).
type Runner struct {
	pre     []Hook
	post    []Hook
	workDir string // agent working directory; hook cmd.Dir
}

// BlockError is returned when a pre_tool hook refuses a tool call.
type BlockError struct {
	Command  string // the hook command that blocked
	Reason   string // trimmed hook stderr (or a timeout notice)
	ExitCode int
}

func (e *BlockError) Error() string {
	return "blocked by pre_tool hook: " + e.Reason
}

// HookFailure records a post_tool hook that exited non-zero or timed out.
type HookFailure struct {
	Command  string
	Reason   string
	ExitCode int
}

// New compiles and validates raw config hooks into a Runner. A bad glob, bad
// regexp, unknown event, or empty command is a hard error (fail closed at
// startup). An empty cfg yields a zero-hook Runner whose Run* methods are
// no-ops.
func New(cfg []config.HookSpec, workDir string) (Runner, error) {
	r := Runner{workDir: workDir}
	for i, spec := range cfg {
		h, err := compile(spec)
		if err != nil {
			return Runner{}, fmt.Errorf("hook %d: %w", i, err)
		}
		switch h.Event {
		case PreTool:
			r.pre = append(r.pre, h)
		case PostTool:
			r.post = append(r.post, h)
		}
	}
	return r, nil
}

// compile validates a single HookSpec and returns the compiled Hook.
func compile(spec config.HookSpec) (Hook, error) {
	ev := Event(spec.Event)
	if ev != PreTool && ev != PostTool {
		return Hook{}, fmt.Errorf("unknown event %q (want %q or %q)", spec.Event, PreTool, PostTool)
	}
	if spec.Match == "" {
		return Hook{}, fmt.Errorf("match is required (a glob naming the tool to target)")
	}
	// path.Match reports ErrBadPattern for a malformed glob; probe it against a
	// throwaway name so a bad pattern fails at startup rather than silently
	// never matching.
	if _, err := path.Match(spec.Match, ""); err != nil {
		return Hook{}, fmt.Errorf("invalid match glob %q: %w", spec.Match, err)
	}
	if spec.Command == "" {
		return Hook{}, fmt.Errorf("command is required")
	}
	h := Hook{
		Event:      ev,
		namePat:    spec.Match,
		inputField: spec.InputField,
		Command:    spec.Command,
		Timeout:    defaultTimeout,
	}
	if spec.InputMatch != "" {
		re, err := regexp.Compile(spec.InputMatch)
		if err != nil {
			return Hook{}, fmt.Errorf("invalid input_match regexp %q: %w", spec.InputMatch, err)
		}
		h.inputRe = re
	}
	if spec.TimeoutMS > 0 {
		h.Timeout = time.Duration(spec.TimeoutMS) * time.Millisecond
	}
	return h, nil
}

// matches reports whether the hook applies to inv. Both the tool-name glob and
// the optional input constraint must hold. It is pure and total (no exec, no
// error path) so it is trivially unit-testable.
func (h Hook) matches(inv Invocation) bool {
	ok, err := path.Match(h.namePat, inv.Tool)
	if err != nil || !ok {
		return false
	}
	if h.inputRe == nil {
		return true
	}
	return h.inputRe.MatchString(h.inputString(inv))
}

// inputString derives the string the input pattern is tested against: the named
// top-level field coerced to a scalar string, or the entire raw JSON input when
// no field is named.
func (h Hook) inputString(inv Invocation) string {
	if h.inputField == "" {
		return string(inv.Input)
	}
	return scalarField(inv.Input, h.inputField)
}

// scalarField returns the top-level JSON field named key coerced to a string:
// a string as-is, numbers/bools via their JSON text, and a missing or non-scalar
// value (object, array, null) as "".
func scalarField(raw json.RawMessage, key string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	v, ok := obj[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}
	var num json.Number
	if err := json.Unmarshal(v, &num); err == nil {
		return num.String()
	}
	var b bool
	if err := json.Unmarshal(v, &b); err == nil {
		return strconv.FormatBool(b)
	}
	return ""
}

// RunPre runs every matching pre_tool hook in config order and short-circuits on
// the first non-zero exit, returning a *BlockError (nil = allowed). The zero
// Runner has no pre hooks, so it always returns nil.
func (r Runner) RunPre(ctx context.Context, inv Invocation) *BlockError {
	for _, h := range r.pre {
		if !h.matches(inv) {
			continue
		}
		if reason, code, ok := r.exec(ctx, h, inv); !ok {
			return &BlockError{Command: h.Command, Reason: reason, ExitCode: code}
		}
	}
	return nil
}

// RunPost runs every matching post_tool hook in config order, running all of
// them regardless of individual failures, and returns any failures observed.
// The zero Runner has no post hooks, so it always returns nil.
func (r Runner) RunPost(ctx context.Context, inv Invocation) []HookFailure {
	var failures []HookFailure
	for _, h := range r.post {
		if !h.matches(inv) {
			continue
		}
		if reason, code, ok := r.exec(ctx, h, inv); !ok {
			failures = append(failures, HookFailure{Command: h.Command, Reason: reason, ExitCode: code})
		}
	}
	return failures
}

// exec runs one hook command under a timeout. It returns (reason, exitCode,
// ok): ok is true on a clean exit-0; on failure or timeout it is false with a
// human-readable reason and the process exit code (-1 when killed or never
// started). The raw JSON input is piped to stdin so scripts can `jq` it, and the
// tool metadata is exposed via SPAI_* environment variables.
func (r Runner) exec(ctx context.Context, h Hook, inv Invocation) (reason string, exitCode int, ok bool) {
	runCtx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", h.Command)
	cmd.Dir = r.workDir
	cmd.Stdin = strings.NewReader(string(inv.Input))
	cmd.Env = append(os.Environ(),
		"SPAI_HOOK_EVENT="+string(h.Event),
		"SPAI_TOOL_NAME="+inv.Tool,
		"SPAI_TOOL_INPUT="+string(inv.Input),
		"SPAI_TOOL_OUTPUT="+inv.Output,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return "", 0, true
	}

	// A deadline hit is reported as a timeout regardless of the exit shape, so a
	// killed process reads as a timeout rather than a generic non-zero exit.
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("timed out after %s", h.Timeout), -1, false
	}

	reason = strings.TrimSpace(stderr.String())
	exitCode = -1
	if ee, isExit := err.(*exec.ExitError); isExit {
		exitCode = ee.ExitCode()
	}
	if reason == "" {
		// No stderr to surface (e.g. the command was not found); fall back to the
		// exec error so the reason is never empty.
		reason = err.Error()
	}
	return reason, exitCode, false
}
