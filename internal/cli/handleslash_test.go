package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"spaish/internal/agent"
	"spaish/internal/app"
)

// captureStdout is defined in render_test.go and reused here to observe the
// print helpers, which write directly to os.Stdout via fmt.

// newREPLWithApp builds a REPL backed by a real, offline *app.App. Config and
// data directories are redirected to temp dirs so tests never touch the user's
// real state and stay isolated from one another. The optional configTOML is
// written to spaid.toml so tests can, e.g., declare MCP servers.
func newREPLWithApp(t *testing.T, sessionID, configTOML string) *REPL {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if configTOML != "" {
		dir := filepath.Join(cfgDir, "spaish")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "spaid.toml"), []byte(configTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &REPL{app: app.New(), sessionID: sessionID, mode: agent.ModeManual}
}

// ---------- handleSlash dispatch ----------

func TestHandleSlashHelp(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	var exit bool
	out := captureStdout(t, func() { exit = r.handleSlash("/help") })
	if exit {
		t.Error("/help should not signal exit")
	}
	if !strings.Contains(out, "Commands:") {
		t.Errorf("help output missing 'Commands:', got %q", out)
	}
}

func TestHandleSlashHelpForCommand(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	out := captureStdout(t, func() { r.handleSlash("/help mode") })
	if !strings.Contains(out, "execution mode") {
		t.Errorf("/help mode should print the /mode detail, got %q", out)
	}
}

func TestHandleSlashModeShowsCurrent(t *testing.T) {
	r := &REPL{mode: agent.ModePlan}
	var exit bool
	out := captureStdout(t, func() { exit = r.handleSlash("/mode") })
	if exit {
		t.Error("/mode (no args) should not signal exit")
	}
	if !strings.Contains(out, agent.ModePlan) {
		t.Errorf("/mode should echo the current mode %q, got %q", agent.ModePlan, out)
	}
}

func TestHandleSlashModeMutatesState(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	out := captureStdout(t, func() { r.handleSlash("/mode auto") })
	if r.mode != agent.ModeAuto {
		t.Errorf("mode = %q, want %q", r.mode, agent.ModeAuto)
	}
	if !strings.Contains(out, agent.ModeAuto) {
		t.Errorf("output should confirm the new mode, got %q", out)
	}
}

func TestHandleSlashModeUnknown(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	out := captureStdout(t, func() { r.handleSlash("/mode sideways") })
	if r.mode != agent.ModeManual {
		t.Errorf("mode should be unchanged on bad input, got %q", r.mode)
	}
	if !strings.Contains(out, "unknown mode") {
		t.Errorf("output should reject the bad mode, got %q", out)
	}
}

func TestHandleSlashExitSignals(t *testing.T) {
	for _, cmd := range []string{"/quit", "/exit", "/q"} {
		r := &REPL{mode: agent.ModeManual}
		var exit bool
		out := captureStdout(t, func() { exit = r.handleSlash(cmd) })
		if !exit {
			t.Errorf("%s should signal exit", cmd)
		}
		if out != "" {
			t.Errorf("%s should print nothing, got %q", cmd, out)
		}
	}
}

func TestHandleSlashTools(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	var exit bool
	out := captureStdout(t, func() { exit = r.handleSlash("/tools") })
	if exit {
		t.Error("/tools should not signal exit")
	}
	if strings.TrimSpace(out) == "" {
		t.Error("/tools should list at least one tool")
	}
}

func TestHandleSlashUnknownSuggests(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	var exit bool
	out := captureStdout(t, func() { exit = r.handleSlash("/hlep") })
	if exit {
		t.Error("unknown command should not signal exit")
	}
	if !strings.Contains(out, "did you mean") || !strings.Contains(out, "/help") {
		t.Errorf("expected a 'did you mean /help' suggestion, got %q", out)
	}
}

func TestHandleSlashUnknownNoSuggestion(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	out := captureStdout(t, func() { r.handleSlash("/zzzzzzzz") })
	if strings.Contains(out, "did you mean") {
		t.Errorf("a far-off command should not suggest anything, got %q", out)
	}
	if !strings.Contains(out, "/help") {
		t.Errorf("the plain error should still point at /help, got %q", out)
	}
}

// TestHandleSlashClear exercises the state-mutating /clear branch, which routes
// through runSession into app.RunSession.
func TestHandleSlashClear(t *testing.T) {
	r := newREPLWithApp(t, "cleartest", "")
	var exit bool
	out := captureStdout(t, func() { exit = r.handleSlash("/clear") })
	if exit {
		t.Error("/clear should not signal exit")
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("/clear should report the session was cleared, got %q", out)
	}
}

// ---------- printModels ----------

func TestPrintModels(t *testing.T) {
	r := newREPLWithApp(t, "modelstest", "")
	out := captureStdout(t, r.printModels)
	if !strings.Contains(out, "active:") {
		t.Errorf("printModels should show the active provider, got %q", out)
	}
	if !strings.Contains(out, "anthropic") {
		t.Errorf("printModels should list the anthropic provider, got %q", out)
	}
	if !strings.Contains(out, "/model") {
		t.Errorf("printModels should print the switch hint, got %q", out)
	}
}

// ---------- printMCP ----------

func TestPrintMCPNoServers(t *testing.T) {
	r := newREPLWithApp(t, "mcpnone", "")
	out := captureStdout(t, r.printMCP)
	if !strings.Contains(out, "no MCP servers configured") {
		t.Errorf("printMCP should report the empty state, got %q", out)
	}
}

func TestPrintMCPWithServer(t *testing.T) {
	// A server declared with no command can't connect; printMCP must still list
	// it with a failure marker rather than skipping the loop entirely.
	cfg := "[[mcp.servers]]\nname = \"brokenserver\"\ncommand = \"\"\n"
	r := newREPLWithApp(t, "mcpserver", cfg)
	if got := r.app.MCPServerCount(); got != 1 {
		t.Fatalf("expected 1 configured MCP server, got %d", got)
	}
	out := captureStdout(t, r.printMCP)
	if !strings.Contains(out, "brokenserver") {
		t.Errorf("printMCP should name the configured server, got %q", out)
	}
	if !strings.Contains(out, "connecting") {
		t.Errorf("printMCP should print the connecting hint on first call, got %q", out)
	}
}

// ---------- printCost ----------

func TestPrintCostHappyPath(t *testing.T) {
	r := newREPLWithApp(t, "costprint", "")
	out := captureStdout(t, r.printCost)
	if !strings.Contains(out, "model:") {
		t.Errorf("printCost should print a model line, got %q", out)
	}
	if !strings.Contains(out, "tokens:") {
		t.Errorf("printCost should print a tokens line, got %q", out)
	}
	if !strings.Contains(out, "cost:") {
		t.Errorf("printCost should print a cost line, got %q", out)
	}
}

func TestPrintCostLoadError(t *testing.T) {
	r := newREPLWithApp(t, "costerr", "")
	seedUnreadableSession(t, "costerr")
	out := captureStdout(t, r.printCost)
	if !strings.Contains(out, "✗") {
		t.Errorf("printCost should surface the load error, got %q", out)
	}
}

// ---------- printHistory ----------

func TestPrintHistoryEmpty(t *testing.T) {
	r := newREPLWithApp(t, "histempty", "")
	out := captureStdout(t, r.printHistory)
	if !strings.Contains(out, "no history yet") {
		t.Errorf("printHistory should report the empty state, got %q", out)
	}
}

func TestPrintHistoryWithContent(t *testing.T) {
	r := newREPLWithApp(t, "histfull", "")
	dir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "spaish", "sessions", "histfull")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "history.md"), []byte("## turn — user\nhello there\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, r.printHistory)
	if !strings.Contains(out, "hello there") {
		t.Errorf("printHistory should print the recorded transcript, got %q", out)
	}
}

func TestPrintHistoryLoadError(t *testing.T) {
	r := newREPLWithApp(t, "histerr", "")
	seedUnreadableSession(t, "histerr")
	out := captureStdout(t, r.printHistory)
	if !strings.Contains(out, "✗") {
		t.Errorf("printHistory should surface the load error, got %q", out)
	}
}

// ---------- printCommandHelp ----------

func TestPrintCommandHelpKnown(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	// The leading slash is optional; exercise the auto-prefix path too.
	out := captureStdout(t, func() { r.printCommandHelp("cost") })
	if !strings.Contains(out, "/cost") {
		t.Errorf("printCommandHelp should echo the canonical command name, got %q", out)
	}
	if !strings.Contains(out, "token usage") {
		t.Errorf("printCommandHelp should print the /cost detail, got %q", out)
	}
}

func TestPrintCommandHelpUnknown(t *testing.T) {
	r := &REPL{mode: agent.ModeManual}
	out := captureStdout(t, func() { r.printCommandHelp("/nope") })
	if !strings.Contains(out, "no help for") {
		t.Errorf("printCommandHelp should report the missing command, got %q", out)
	}
}

// seedUnreadableSession makes session.LoadByID fail for id by planting a
// directory where its cache.json file is expected, so os.ReadFile returns a
// non-IsNotExist error. Relies on XDG_DATA_HOME already pointing at a temp dir.
func seedUnreadableSession(t *testing.T, id string) {
	t.Helper()
	cachePath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "spaish", "sessions", id, "cache.json")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
}
