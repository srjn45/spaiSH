package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/config"
)

// mustNew compiles a single-hook Runner or fails the test.
func mustNew(t *testing.T, spec config.HookSpec, workDir string) Runner {
	t.Helper()
	r, err := New([]config.HookSpec{spec}, workDir)
	if err != nil {
		t.Fatalf("New(%+v) error: %v", spec, err)
	}
	return r
}

// TestHook_matches drives the pure matcher: name globs, event is not part of the
// match (RunPre/RunPost route by event), and the optional input constraint with
// and without an input_field.
func TestHook_matches(t *testing.T) {
	cases := []struct {
		name       string
		match      string
		inputField string
		inputMatch string
		tool       string
		input      string
		want       bool
	}{
		{"exact name", "write_file", "", "", "write_file", `{}`, true},
		{"exact name mismatch", "write_file", "", "", "edit_file", `{}`, false},
		{"suffix glob", "*_file", "", "", "edit_file", `{}`, true},
		{"suffix glob mismatch", "*_file", "", "", "bash", `{}`, false},
		{"mcp prefix glob", "mcp__*", "", "", "mcp__github__create", `{}`, true},
		{"star matches all", "*", "", "", "anything", `{}`, true},
		{"field present matches", "write_file", "path", "^vendor/", "write_file", `{"path":"vendor/x.go"}`, true},
		{"field present no match", "write_file", "path", "^vendor/", "write_file", `{"path":"src/x.go"}`, false},
		{"field absent => empty string", "write_file", "path", "^$", "write_file", `{"other":"y"}`, true},
		{"field absent vs nonempty pattern", "write_file", "path", ".+", "write_file", `{"other":"y"}`, false},
		{"numeric field coerced", "t", "n", "^42$", "t", `{"n":42}`, true},
		{"bool field coerced", "t", "b", "^true$", "t", `{"b":true}`, true},
		{"non-scalar field => empty", "t", "obj", ".+", "t", `{"obj":{"a":1}}`, false},
		{"whole input match no field", "bash", "", "rm -rf", "bash", `{"command":"rm -rf /"}`, true},
		{"whole input no match", "bash", "", "rm -rf", "bash", `{"command":"ls"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := compile(config.HookSpec{
				Event: "pre_tool", Match: tc.match,
				InputField: tc.inputField, InputMatch: tc.inputMatch,
				Command: "true",
			})
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
			inv := Invocation{Tool: tc.tool, Input: json.RawMessage(tc.input)}
			if got := h.matches(inv); got != tc.want {
				t.Errorf("matches(tool=%q input=%s) = %v, want %v", tc.tool, tc.input, got, tc.want)
			}
		})
	}
}

func TestRunPre_allow(t *testing.T) {
	r := mustNew(t, config.HookSpec{Event: "pre_tool", Match: "*", Command: "exit 0"}, t.TempDir())
	if be := r.RunPre(context.Background(), Invocation{Tool: "write_file", Input: json.RawMessage(`{}`)}); be != nil {
		t.Errorf("RunPre with exit 0 = %+v, want nil", be)
	}
}

func TestRunPre_block(t *testing.T) {
	r := mustNew(t, config.HookSpec{Event: "pre_tool", Match: "*", Command: "echo nope >&2; exit 3"}, t.TempDir())
	be := r.RunPre(context.Background(), Invocation{Tool: "write_file", Input: json.RawMessage(`{}`)})
	if be == nil {
		t.Fatal("RunPre with exit 3 = nil, want BlockError")
	}
	if be.Reason != "nope" {
		t.Errorf("Reason = %q, want %q", be.Reason, "nope")
	}
	if be.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", be.ExitCode)
	}
	if !strings.Contains(be.Error(), "blocked by pre_tool hook: nope") {
		t.Errorf("Error() = %q", be.Error())
	}
}

// TestRunPre_shortCircuit verifies that when the first matching pre hook blocks,
// a later matching hook's side effect never fires.
func TestRunPre_shortCircuit(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "second-ran")
	r, err := New([]config.HookSpec{
		{Event: "pre_tool", Match: "*", Command: "exit 1"},
		{Event: "pre_tool", Match: "*", Command: "touch " + marker},
	}, dir)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if be := r.RunPre(context.Background(), Invocation{Tool: "bash", Input: json.RawMessage(`{}`)}); be == nil {
		t.Fatal("expected first hook to block")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("second hook ran (marker exists) despite first block")
	}
}

// TestRunPost_observe verifies all matching post hooks run even when one fails,
// and only the failing one is returned as a HookFailure.
func TestRunPost_observe(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "third-ran")
	r, err := New([]config.HookSpec{
		{Event: "post_tool", Match: "*", Command: "exit 0"},
		{Event: "post_tool", Match: "*", Command: "echo boom >&2; exit 2"},
		{Event: "post_tool", Match: "*", Command: "touch " + marker},
	}, dir)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	failures := r.RunPost(context.Background(), Invocation{Tool: "edit_file", Input: json.RawMessage(`{}`), Output: "ok"})
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1: %+v", len(failures), failures)
	}
	if failures[0].Reason != "boom" || failures[0].ExitCode != 2 {
		t.Errorf("failure = %+v, want Reason=boom ExitCode=2", failures[0])
	}
	// The third hook must still have run despite the second failing.
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("third post hook did not run after an earlier failure: %v", err)
	}
}

func TestRunPre_timeoutBlocks(t *testing.T) {
	r := mustNew(t, config.HookSpec{Event: "pre_tool", Match: "*", Command: "sleep 5", TimeoutMS: 50}, t.TempDir())
	be := r.RunPre(context.Background(), Invocation{Tool: "bash", Input: json.RawMessage(`{}`)})
	if be == nil {
		t.Fatal("timed-out pre hook = nil, want BlockError")
	}
	if !strings.Contains(be.Reason, "timed out") {
		t.Errorf("Reason = %q, want a timeout notice", be.Reason)
	}
}

func TestRunPost_timeoutFails(t *testing.T) {
	r := mustNew(t, config.HookSpec{Event: "post_tool", Match: "*", Command: "sleep 5", TimeoutMS: 50}, t.TempDir())
	failures := r.RunPost(context.Background(), Invocation{Tool: "bash", Input: json.RawMessage(`{}`), Output: "x"})
	if len(failures) != 1 || !strings.Contains(failures[0].Reason, "timed out") {
		t.Errorf("timed-out post hook failures = %+v, want one timeout", failures)
	}
}

// TestExec_envAndStdin verifies the SPAI_* env vars and the raw JSON stdin reach
// the hook process.
func TestExec_envAndStdin(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "captured")
	// The hook writes its event, tool name, output var, and stdin to a file.
	cmd := "{ echo \"$SPAI_HOOK_EVENT\"; echo \"$SPAI_TOOL_NAME\"; echo \"$SPAI_TOOL_OUTPUT\"; cat; } > " + out
	r := mustNew(t, config.HookSpec{Event: "post_tool", Match: "*", Command: cmd}, dir)
	inv := Invocation{Tool: "write_file", Input: json.RawMessage(`{"path":"a.go"}`), Output: "wrote a.go"}
	if f := r.RunPost(context.Background(), inv); len(f) != 0 {
		t.Fatalf("unexpected failures: %+v", f)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading captured output: %v", err)
	}
	want := "post_tool\nwrite_file\nwrote a.go\n" + `{"path":"a.go"}`
	if string(got) != want {
		t.Errorf("captured = %q, want %q", got, want)
	}
}

// TestExec_cmdDir verifies the hook runs in the configured working directory.
func TestExec_cmdDir(t *testing.T) {
	dir := t.TempDir()
	r := mustNew(t, config.HookSpec{Event: "pre_tool", Match: "*", Command: "test \"$(pwd)\" = \"$EXPECT\" || { echo wrongdir >&2; exit 1; }"}, dir)
	// Resolve symlinks (macOS /var -> /private/var) so the comparison holds.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECT", real)
	if be := r.RunPre(context.Background(), Invocation{Tool: "bash", Input: json.RawMessage(`{}`)}); be != nil {
		t.Errorf("hook did not run in workDir: %+v", be)
	}
}

func TestNew_rejects(t *testing.T) {
	cases := []struct {
		name string
		spec config.HookSpec
	}{
		{"unknown event", config.HookSpec{Event: "on_turn", Match: "*", Command: "true"}},
		{"empty match", config.HookSpec{Event: "pre_tool", Match: "", Command: "true"}},
		{"bad glob", config.HookSpec{Event: "pre_tool", Match: "[", Command: "true"}},
		{"bad regexp", config.HookSpec{Event: "pre_tool", Match: "*", InputMatch: "(", Command: "true"}},
		{"empty command", config.HookSpec{Event: "pre_tool", Match: "*", Command: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New([]config.HookSpec{tc.spec}, t.TempDir()); err == nil {
				t.Errorf("New(%+v) = nil error, want rejection", tc.spec)
			}
		})
	}
}

func TestNew_defaultTimeout(t *testing.T) {
	r := mustNew(t, config.HookSpec{Event: "pre_tool", Match: "*", Command: "true"}, t.TempDir())
	if got := r.pre[0].Timeout; got != defaultTimeout {
		t.Errorf("default timeout = %v, want %v", got, defaultTimeout)
	}
}

// TestRunner_zeroValueNoop verifies the zero Runner has no hooks and its Run*
// methods are no-ops for any invocation, so an unset Config.Hooks is legacy.
func TestRunner_zeroValueNoop(t *testing.T) {
	var r Runner
	inv := Invocation{Tool: "write_file", Input: json.RawMessage(`{"path":"x"}`)}
	if be := r.RunPre(context.Background(), inv); be != nil {
		t.Errorf("zero Runner RunPre = %+v, want nil", be)
	}
	if f := r.RunPost(context.Background(), inv); len(f) != 0 {
		t.Errorf("zero Runner RunPost = %+v, want empty", f)
	}
}

// TestRunPre_eventRouting verifies a post_tool hook never fires on RunPre and a
// pre_tool hook never fires on RunPost.
func TestRunPre_eventRouting(t *testing.T) {
	dir := t.TempDir()
	r, err := New([]config.HookSpec{
		{Event: "post_tool", Match: "*", Command: "exit 1"},
	}, dir)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if be := r.RunPre(context.Background(), Invocation{Tool: "bash", Input: json.RawMessage(`{}`)}); be != nil {
		t.Errorf("post_tool hook fired on RunPre: %+v", be)
	}
}
