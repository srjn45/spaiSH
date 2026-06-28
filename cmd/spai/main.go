package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"spaish/internal/app"
	"spaish/internal/permissions"
	"spaish/internal/protocol"
	"spaish/internal/session"
)

const disclaimer = `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  spaiSH — experimental personal project
  Not affiliated with any AI provider or Linux distribution.
  You are responsible for your API key usage and costs.
  Run 'spai --legal' for full disclaimer and license.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
`

const legalText = `spaiSH is an experimental personal project provided AS IS with no warranties.

You are responsible for all actions taken on your system. Every command is
shown to you before execution and requires your confirmation.

You must supply your own API key for any cloud AI provider. spaiSH does not
provide API access and is not affiliated with any AI provider or Linux distribution.

Full license: Apache 2.0 — https://www.apache.org/licenses/LICENSE-2.0
`

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaish")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaish")
}

func stampPath() string { return filepath.Join(dataDir(), ".first_run_done") }

func showDisclaimer() {
	if _, err := os.Stat(stampPath()); err == nil {
		return
	}
	fmt.Print(disclaimer)
	os.MkdirAll(dataDir(), 0700)
	os.WriteFile(stampPath(), []byte("done"), 0600)
}

func gitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveSessionID returns the session ID to use for this invocation.
// Priority: explicit flag value > $SPAI_SESSION_ID env var > pinned session > "default".
func resolveSessionID(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if id := os.Getenv("SPAI_SESSION_ID"); id != "" {
		return id
	}
	if id := session.ReadPinned(); id != "" {
		return id
	}
	return "default"
}

// readStdin reads piped stdin up to 64 KB. Returns "" if stdin is a TTY.
// Appends "[truncated]" if the input exceeded the size cap.
func readStdin() string {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	const maxBytes = 64 * 1024
	r := io.LimitReader(os.Stdin, int64(maxBytes)+1)
	data, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to read stdin: %v\n", err)
		return ""
	}
	if len(data) > maxBytes {
		return string(data[:maxBytes]) + "[truncated]"
	}
	return string(data)
}

// printStream is the default streaming handler: text/output to stdout, errors to stderr.
func printStream(resp protocol.Response) {
	switch resp.Type {
	case "text", "output":
		fmt.Print(resp.Content)
	case "error":
		fmt.Fprintf(os.Stderr, "\nerror: %s\n", resp.Content)
	}
}

// handleLLMCommand handles `spai llm <cmd> [args...]`.
func handleLLMCommand(args []string) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("Usage: spai llm <command> [args]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  status                  show runtime and model status")
		fmt.Println("  install [runtime]       install a runtime (ollama, bitnet) — default: ollama")
		fmt.Println("  use-runtime <runtime>   switch active runtime (ollama, bitnet)")
		fmt.Println("  list                    list installed and recommended models")
		fmt.Println("  pull <model>            download a model (e.g. qwen2.5-coder:7b)")
		fmt.Println("  remove <model>          delete a model from local storage")
		fmt.Println("  use <model>             set the active model for local inference")
		fmt.Println()
		fmt.Println("Runtimes:")
		fmt.Println("  ollama   Popular local runtime, wide model support (default)")
		fmt.Println("  bitnet   Microsoft 1-bit AI — extreme CPU efficiency, no GPU required")
		os.Exit(0)
	}

	showDisclaimer()

	a := app.New()
	fmt.Println()

	var plan []protocol.CommandItem
	a.RunLLM(&protocol.LLMRequest{Command: args[0], Args: args[1:]}, func(resp protocol.Response) {
		switch resp.Type {
		case "text":
			fmt.Print(resp.Content)
		case "plan":
			plan = resp.Plan
		case "error":
			fmt.Fprintf(os.Stderr, "\nerror: %s\n", resp.Content)
		}
	})
	fmt.Println()

	if len(plan) == 0 {
		return
	}

	fmt.Println("I will run:")
	for _, item := range plan {
		fmt.Printf("  [%s] %s\n", item.Display, item.Command)
	}

	if confirmPlan(plan) == nil {
		fmt.Println("Cancelled.")
		return
	}

	fmt.Println()
	runConfirmed(plan)
}

// handleSessionMaintenance handles clear / compact / rebuild-context, which all
// stream text back through the app's session handler.
func handleSessionMaintenance(cmdName string, args []string) {
	fs := flag.NewFlagSet(cmdName, flag.ExitOnError)
	var lines int
	if cmdName == "clear" {
		fs.IntVar(&lines, "lines", 0, "keep only the last N messages (default: clear all)")
	}
	sessionFlag := fs.String("session", "", "named session (default: $SPAI_SESSION_ID or 'default')")
	fs.Parse(args)

	showDisclaimer()

	a := app.New()
	fmt.Println()
	a.RunSession(context.Background(), &protocol.Request{
		SessionID: resolveSessionID(*sessionFlag),
		Session:   &protocol.SessionRequest{Command: cmdName, Lines: lines},
	}, printStream)
	fmt.Println()
}

// handleHistoryCommand handles `spai history [--session <id>]`.
// Client-side only — reads history files directly, pipes to less (or more).
func handleHistoryCommand(args []string) {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	sessionFlag := fs.String("session", "", "named session (default: resolved session ID)")
	fs.Parse(args)

	id := resolveSessionID(*sessionFlag)
	sess, err := session.LoadByID(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	content, err := sess.ReadAllHistory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading history: %v\n", err)
		os.Exit(1)
	}
	if content == "" {
		fmt.Printf("No history for session '%s'.\n", id)
		return
	}

	pager := "less"
	if _, err := exec.LookPath("less"); err != nil {
		pager = "more"
	}

	cmd := exec.Command(pager)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Print(content)
	}
}

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// handleSessionsListCommand prints the sessions listing.
func handleSessionsListCommand() {
	list, err := session.ListSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	pinned := session.ReadPinned()
	shellID := os.Getenv("SPAI_SESSION_ID")

	fmt.Println()
	fmt.Println("  Sessions")
	fmt.Println("  ────────────────────────────────────")

	for _, s := range list {
		marker := " "
		if s.ID == pinned {
			marker = "*"
		}

		label := ""
		if s.ID == pinned {
			label = "(pinned)"
		} else if isShellSession(s.ID, shellID) {
			label = "(shell) "
		} else {
			label = "        "
		}

		msgs := fmt.Sprintf("%d msgs", s.MsgCount)
		age := formatRelativeTime(s.ModTime)

		fmt.Printf("%s %-12s %s  %-8s  %s\n", marker, s.ID, label, msgs, age)
	}
	fmt.Println()
}

// isShellSession returns true if id is a PID-based (all-numeric) or matches $SPAI_SESSION_ID.
func isShellSession(id, shellID string) bool {
	if shellID != "" && id == shellID {
		return true
	}
	if len(id) == 0 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// handleSessionsCommand handles `spai sessions [<id> | --reset]`.
func handleSessionsCommand(args []string) {
	if len(args) == 0 {
		handleSessionsListCommand()
		return
	}

	if args[0] == "--reset" {
		if err := session.ClearPinned(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Pinned session cleared. Falling back to $SPAI_SESSION_ID or 'default'.")
		return
	}

	id := args[0]
	sessDir := filepath.Join(session.SessionsDir(), id)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating session dir: %v\n", err)
		os.Exit(1)
	}
	if err := session.WritePinned(id); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Switched to session '%s'. Future queries will use this context.\n", id)
}

func main() {
	// Handle subcommands before flag parsing so flags don't interfere.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "llm":
			handleLLMCommand(os.Args[2:])
			return
		case "clear", "compact", "rebuild-context":
			handleSessionMaintenance(os.Args[1], os.Args[2:])
			return
		case "history":
			handleHistoryCommand(os.Args[2:])
			return
		case "sessions":
			handleSessionsCommand(os.Args[2:])
			return
		}
	}

	dryRun := flag.Bool("dry-run", false, "show plan without executing")
	forceLocal := flag.Bool("local", false, "force local model")
	verbose := flag.Bool("verbose", false, "show full command output and iteration details")
	autonomous := flag.Bool("autonomous", false, "run all commands without confirmation prompts")
	legal := flag.Bool("legal", false, "print legal disclaimer and exit")
	sessionFlag := flag.String("session", "", "named session (default: $SPAI_SESSION_ID or 'default')")
	flag.Usage = func() {
		fmt.Println("Usage: spai [flags] <query>")
		fmt.Println("       spai !!                  analyse last failed command")
		fmt.Println("       spai clear [--lines N]   wipe session or keep latest N messages")
		fmt.Println("       spai compact             AI-summarise session history")
		fmt.Println("       spai history             browse session history in pager")
		fmt.Println("       spai sessions            list or switch sessions")
		fmt.Println("       spai rebuild-context     rebuild AI context from history")
		fmt.Println()
		flag.PrintDefaults()
	}
	flag.Parse()

	if *legal {
		fmt.Print(legalText)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	query := strings.Join(args, " ")
	if query == "!!" {
		query = "My last command failed. What went wrong and how do I fix it?"
	}

	stdin := readStdin()

	showDisclaimer()

	cwd, _ := os.Getwd()
	req := &protocol.Request{
		Type:       "agent",
		SessionID:  resolveSessionID(*sessionFlag),
		Stdin:      stdin,
		WorkingDir: cwd,
		GitBranch:  gitBranch(),
		ForceLocal: *forceLocal,
		DryRun:     *dryRun,
		Agent: &protocol.AgentRequest{
			Query:      query,
			Verbose:    *verbose,
			Autonomous: *autonomous,
		},
	}

	confirmFn := func(creq protocol.ConfirmRequest) bool {
		fmt.Printf("\n[%s] %s\n", creq.Display, creq.Command)
		return confirmSingle(creq)
	}

	a := app.New()
	fmt.Println()
	if err := a.RunAgent(context.Background(), req, confirmFn, printStream); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
}

// runConfirmed executes confirmed commands locally after plan approval.
func runConfirmed(plan []protocol.CommandItem) {
	for _, item := range plan {
		fmt.Printf("$ %s\n", item.Command)
		cmd := exec.Command("sh", "-c", item.Command)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "error: command failed: %v\n", err)
			return
		}
	}
}

// confirmSingle prompts the user to approve or deny a single command.
func confirmSingle(req protocol.ConfirmRequest) bool {
	reader := bufio.NewReader(os.Stdin)
	switch req.Tier {
	case permissions.TierDestructive.String():
		fmt.Printf("\n⚠  DESTRUCTIVE — cannot be undone:\n   %s\n", req.Command)
		fmt.Print("Type YES to confirm: ")
		input, _ := reader.ReadString('\n')
		return strings.TrimSpace(input) == "YES"
	case permissions.TierElevated.String():
		fmt.Printf("\n⚠  ELEVATED — requires elevated privileges:\n   %s\n", req.Command)
		fmt.Print("Allow? [y/n]: ")
	default:
		fmt.Print("Allow? [y/n]: ")
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

// confirmPlan prompts the user for confirmation of a batch plan.
func confirmPlan(plan []protocol.CommandItem) []string {
	reader := bufio.NewReader(os.Stdin)

	for _, item := range plan {
		if item.Tier == permissions.TierDestructive.String() {
			fmt.Printf("\n⚠  DESTRUCTIVE — cannot be undone:\n   %s\n", item.Command)
			fmt.Print("Type YES to confirm this specific command: ")
			input, _ := reader.ReadString('\n')
			if strings.TrimSpace(input) != "YES" {
				return nil
			}
		}
	}

	fmt.Print("\nApply? [y/n]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input != "y" && input != "yes" {
		return nil
	}

	cmds := make([]string, len(plan))
	for i, item := range plan {
		cmds[i] = item.Command
	}
	return cmds
}
