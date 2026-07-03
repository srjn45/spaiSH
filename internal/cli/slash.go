package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/chzyer/readline"

	"spaish/internal/agent"
	"spaish/internal/mcp"
	"spaish/internal/pricing"
	"spaish/internal/protocol"
	"spaish/internal/session"
	"spaish/internal/tools"
)

// completer provides tab-completion for slash commands.
func completer() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/mode", readline.PcItem("manual"), readline.PcItem("auto"), readline.PcItem("plan")),
		readline.PcItem("/model", readline.PcItem("anthropic"), readline.PcItem("openai"), readline.PcItem("ollama")),
		readline.PcItem("/tools"),
		readline.PcItem("/mcp"),
		readline.PcItem("/cost"),
		readline.PcItem("/clear"),
		readline.PcItem("/compact"),
		readline.PcItem("/history"),
		readline.PcItem("/quit"),
		readline.PcItem("/exit"),
	)
}

// handleSlash runs a slash command. It returns true if the REPL should exit.
func (r *REPL) handleSlash(line string) bool {
	fields := strings.Fields(line)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/quit", "/exit", "/q":
		return true

	case "/help":
		fmt.Print(helpText)

	case "/mode":
		if len(args) == 0 {
			fmt.Printf("mode: %s  (manual | auto | plan)\n", bold(r.mode))
			break
		}
		switch args[0] {
		case agent.ModeManual, agent.ModeAuto, agent.ModePlan:
			r.mode = args[0]
			fmt.Printf("mode → %s\n", bold(r.mode))
		default:
			fmt.Printf("%s unknown mode %q (use manual, auto, or plan)\n", red("✗"), args[0])
		}

	case "/model":
		if len(args) == 0 {
			r.printModels()
			break
		}
		desc, err := r.app.SetModel(args)
		if err != nil {
			fmt.Printf("%s %v\n", red("✗"), err)
			break
		}
		fmt.Printf("model → %s\n", bold(desc))

	case "/tools":
		for _, t := range tools.DefaultRegistry().Specs() {
			fmt.Printf("  %s  %s\n", cyan(t.Name), dim(t.Description))
		}

	case "/mcp":
		r.printMCP()

	case "/cost":
		r.printCost()

	case "/clear":
		r.runSession("clear")

	case "/compact":
		r.runSession("compact")

	case "/history":
		r.printHistory()

	default:
		fmt.Printf("%s unknown command %q — try %s\n", red("✗"), cmd, cyan("/help"))
	}
	return false
}

func (r *REPL) runSession(command string) {
	r.app.RunSession(context.Background(), &protocol.Request{
		SessionID: r.sessionID,
		Session:   &protocol.SessionRequest{Command: command},
	}, RenderResponse)
	fmt.Println()
}

// printModels shows the active provider/model and the configured options.
func (r *REPL) printModels() {
	fmt.Printf("active: %s\n", bold(r.app.ProviderInfo()))
	fmt.Println(dim("available:"))
	for _, o := range r.app.ModelOptions() {
		label := o.Provider
		if o.Model != "" {
			label += " / " + o.Model
		}
		status := dim("offline")
		if o.Available {
			status = "ready"
		}
		marker := "  "
		if o.Active {
			marker = cyan("→ ")
		}
		fmt.Printf("%s%s  %s\n", marker, cyan(label), dim("("+status+")"))
	}
	fmt.Println(dim("switch with: /model <provider> [model]  (e.g. /model ollama, /model openai:gpt-4o)"))
}

// printMCP triggers MCP server discovery and prints, per configured server, a
// ✓/✗ marker, the server name, its tool count, and the namespaced tool names —
// or the error for a server that failed to connect. The first call may block
// briefly while servers handshake, so it prints a hint beforehand.
func (r *REPL) printMCP() {
	if r.app.MCPServerCount() == 0 {
		fmt.Println(dim("no MCP servers configured (add [[mcp.servers]] in spaid.toml)"))
		return
	}
	if !r.app.MCPLoaded() {
		fmt.Println(dim("connecting to MCP servers…"))
	}
	for _, s := range r.app.MCPStatus() {
		name := s.Name
		if name == "" {
			name = "(unnamed)"
		}
		if !s.OK {
			fmt.Printf("%s %s  %s\n", red("✗"), bold(name), dim(s.Err))
			continue
		}
		fmt.Printf("%s %s  %s\n", cyan("✓"), bold(name), dim(fmt.Sprintf("(%d tools)", len(s.Tools))))
		for _, t := range s.Tools {
			fmt.Printf("    %s\n", cyan(mcp.ToolName(s.Name, t)))
		}
	}
}

// printCost reports the active model, the estimated token footprint of the
// current session, and the estimated dollar cost using the pricing table.
// Estimates come from the session's ~4-chars-per-token heuristic; local and
// unknown models degrade gracefully.
func (r *REPL) printCost() {
	model := r.app.ActiveModel()

	sess, err := session.LoadByID(r.sessionID)
	if err != nil {
		fmt.Printf("%s %v\n", red("✗"), err)
		return
	}
	usage := sess.EstimateUsage()

	rate, known := pricing.Lookup(model)
	cost := rate.Cost(usage.PromptTokens, usage.GeneratedTokens)

	fmt.Printf("model:  %s\n", bold(r.app.ProviderInfo()))
	fmt.Printf("tokens: ~%s  (prompt ~%s / generated ~%s)\n",
		commafy(usage.TotalTokens()), commafy(usage.PromptTokens), commafy(usage.GeneratedTokens))
	switch {
	case known && rate.Local:
		fmt.Printf("cost:   %s\n", dim("$0.00 (local)"))
	case known:
		fmt.Printf("cost:   ~$%.4f  %s\n", cost,
			dim(fmt.Sprintf("($%.0f/$%.0f per 1M in/out)", rate.Input, rate.Output)))
	default:
		fmt.Printf("cost:   %s\n", dim("unknown pricing for "+model))
	}
	fmt.Println(dim("estimate only — based on a ~4-chars-per-token heuristic."))
}

func (r *REPL) printHistory() {
	sess, err := session.LoadByID(r.sessionID)
	if err != nil {
		fmt.Printf("%s %v\n", red("✗"), err)
		return
	}
	content, _ := sess.ReadAllHistory()
	if strings.TrimSpace(content) == "" {
		fmt.Println(dim("(no history yet)"))
		return
	}
	fmt.Println(content)
}

const helpText = `
Commands:
  /help              show this help
  /mode [m]          show or set execution mode (manual | auto | plan)
  /model [sel]       show providers, or switch (e.g. /model ollama, /model openai:gpt-4o)
  /tools             list available tools
  /mcp               show MCP server connection status and discovered tools
  /cost              show estimated token usage and cost for this session
  /clear             wipe the session's conversation context
  /compact           summarise and compact the session
  /history           print the session history
  /quit              leave the session

Tips:
  - Reference a file with @path to include its contents in your message.
  - Shift-Tab cycles the execution mode (manual → auto → plan).
  - Esc or Ctrl+C cancels the current turn; Ctrl+D (or /quit) exits.

`
