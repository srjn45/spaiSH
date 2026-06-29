package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/chzyer/readline"

	"spaish/internal/agent"
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
  /clear             wipe the session's conversation context
  /compact           summarise and compact the session
  /history           print the session history
  /quit              leave the session

Tips:
  - Reference a file with @path to include its contents in your message.
  - Shift-Tab cycles the execution mode (manual → auto → plan).
  - Esc or Ctrl+C cancels the current turn; Ctrl+D (or /quit) exits.

`
