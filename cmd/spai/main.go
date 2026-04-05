package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"spaios/internal/permissions"
	"spaios/internal/protocol"
	"spaios/internal/socket"
)

const disclaimer = `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  spaiOS — experimental personal project
  Not affiliated with any AI provider or Linux distribution.
  You are responsible for your API key usage and costs.
  Run 'spai --legal' for full disclaimer and license.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
`

const legalText = `spaiOS is an experimental personal project provided AS IS with no warranties.

You are responsible for all actions taken on your system. Every command is
shown to you before execution and requires your confirmation.

You must supply your own API key for any cloud AI provider. spaiOS does not
provide API access and is not affiliated with any AI provider or Linux distribution.

Full license: Apache 2.0 — https://www.apache.org/licenses/LICENSE-2.0
`

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaios")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaios")
}

func sockPath() string  { return filepath.Join(dataDir(), "spaid.sock") }
func stampPath() string { return filepath.Join(dataDir(), ".first_run_done") }

func daemonBin() string {
	self, _ := os.Executable()
	return filepath.Join(filepath.Dir(self), "spaid")
}

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

// handleLLMCommand handles `spai llm <cmd> [args...]`.
// It sends an "llm" typed request to spaid and streams the response.
func handleLLMCommand(args []string) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("Usage: spai llm <command> [args]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  status          show runtime and model status")
		fmt.Println("  install         install Ollama on this machine")
		fmt.Println("  list            list installed and recommended models")
		fmt.Println("  pull <model>    download a model (e.g. qwen2.5-coder:7b)")
		fmt.Println("  remove <model>  delete a model from local storage")
		fmt.Println("  use <model>     set the active model for local inference")
		os.Exit(0)
	}

	showDisclaimer()

	if err := socket.EnsureRunning(sockPath(), daemonBin()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	req := &protocol.Request{
		Type: "llm",
		LLM: &protocol.LLMRequest{
			Command: args[0],
			Args:    args[1:],
		},
	}

	client := socket.NewClient(sockPath())
	fmt.Println()

	var plan []protocol.CommandItem
	err := client.Send(req, func(resp protocol.Response) error {
		switch resp.Type {
		case "text":
			fmt.Print(resp.Content)
		case "plan":
			plan = resp.Plan
		case "error":
			fmt.Fprintf(os.Stderr, "\nerror: %s\n", resp.Content)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()

	if len(plan) == 0 {
		return
	}

	// Reuse existing confirmation flow for install/pull commands
	fmt.Println("I will run:")
	for _, item := range plan {
		fmt.Printf("  [%s] %s\n", item.Display, item.Command)
	}

	if confirmPlan(plan) == nil {
		fmt.Println("Cancelled.")
		return
	}

	fmt.Println()
	runConfirmed(plan, client)
}

func main() {
	// Handle `spai llm <cmd>` before flag parsing so flags don't interfere.
	if len(os.Args) >= 2 && os.Args[1] == "llm" {
		handleLLMCommand(os.Args[2:])
		return
	}

	dryRun := flag.Bool("dry-run", false, "show plan without executing")
	forceLocal := flag.Bool("local", false, "force local model")
	verbose := flag.Bool("verbose", false, "show full command output and iteration details")
	autonomous := flag.Bool("autonomous", false, "run all commands without confirmation prompts")
	legal := flag.Bool("legal", false, "print legal disclaimer and exit")
	flag.Usage = func() {
		fmt.Println("Usage: spai [flags] <query>")
		fmt.Println("       spai !!          analyse last failed command")
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

	showDisclaimer()

	if err := socket.EnsureRunning(sockPath(), daemonBin()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	req := &protocol.Request{
		Type:       "agent",
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

	client := socket.NewClient(sockPath())
	fmt.Println()

	err := client.SendInteractive(req, func(resp protocol.Response, enc *json.Encoder) error {
		switch resp.Type {
		case "text":
			fmt.Print(resp.Content)
		case "output":
			fmt.Print(resp.Content)
		case "error":
			fmt.Fprintf(os.Stderr, "\nerror: %s\n", resp.Content)
		case "confirm_request":
			var confirmReq protocol.ConfirmRequest
			if err := json.Unmarshal([]byte(resp.Content), &confirmReq); err != nil {
				return err
			}
			fmt.Printf("\n[%s] %s\n", confirmReq.Display, confirmReq.Command)
			approved := confirmSingle(confirmReq)
			return enc.Encode(&protocol.Request{
				Type:            "confirm_response",
				ConfirmResponse: &protocol.ConfirmResponse{Approved: approved},
			})
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
}

// runConfirmed executes confirmed commands after plan approval.
// Elevated and destructive commands run directly in the terminal so sudo can
// prompt for a password — they must not go through the socket executor which
// has no TTY. All other commands stream through spaid as usual.
func runConfirmed(plan []protocol.CommandItem, client *socket.Client) {
	var socketCmds []string
	for _, item := range plan {
		if item.Tier == permissions.TierElevated.String() || item.Tier == permissions.TierDestructive.String() {
			fmt.Printf("$ %s\n", item.Command)
			cmd := exec.Command("sh", "-c", item.Command)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "error: command failed: %v\n", err)
				return
			}
		} else {
			socketCmds = append(socketCmds, item.Command)
		}
	}

	if len(socketCmds) == 0 {
		return
	}

	execReq := &protocol.Request{
		Type:     "execute",
		Commands: socketCmds,
	}
	if err := client.Send(execReq, func(resp protocol.Response) error {
		switch resp.Type {
		case "output":
			fmt.Print(resp.Content)
		case "error":
			fmt.Fprintf(os.Stderr, "error: %s\n", resp.Content)
		}
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

// confirmSingle prompts the user to approve or deny a single command.
// Used by the agent flow for mid-loop tier-gated confirmation.
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

// confirmPlan prompts the user for confirmation.
// Returns the list of commands to run, or nil if cancelled.
func confirmPlan(plan []protocol.CommandItem) []string {
	reader := bufio.NewReader(os.Stdin)

	// Destructive commands require individual hard confirmation
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

	// Single confirmation for all remaining commands
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
