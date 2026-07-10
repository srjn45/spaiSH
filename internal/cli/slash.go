package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
		readline.PcItem("/sessions"),
		readline.PcItem("/init"),
		readline.PcItem("/quit"),
		readline.PcItem("/exit"),
	)
}

// replCompleter routes tab-completion between slash commands and @-path
// references. It implements readline.AutoCompleter.
//
// Branching, based on the word under the cursor:
//   - a word starting with '@' anywhere in the line → filesystem completion
//     (files and directories relative to cwd),
//   - otherwise, when the line begins with '/', slash-command completion
//     (delegated to the existing PrefixCompleter, unchanged),
//   - otherwise, no completion.
type replCompleter struct {
	cwd   string
	slash readline.AutoCompleter
}

// newCompleter builds the REPL's combined completer rooted at cwd.
func newCompleter(cwd string) *replCompleter {
	return &replCompleter{cwd: cwd, slash: completer()}
}

// Do implements readline.AutoCompleter. See replCompleter for the routing rules.
func (c *replCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos > len(line) {
		pos = len(line)
	}
	// Find the whitespace-delimited word ending at the cursor.
	start := pos
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	word := line[start:pos]

	if len(word) > 0 && word[0] == '@' {
		return atPathCompletions(c.cwd, string(word[1:]))
	}

	if strings.HasPrefix(strings.TrimLeft(string(line[:pos]), " "), "/") {
		return c.slash.Do(line, pos)
	}
	return nil, 0
}

// atPathCompletions lists filesystem completions for the path fragment typed
// after '@' (frag is the text after '@', up to the cursor). It returns the
// suffixes to append to the current path component and the rune length of that
// component (the offset readline uses to render candidates).
//
// Matching is a case-sensitive prefix match against the entries of the
// fragment's directory; directories get a trailing '/' so completion can
// continue into them. Hidden entries are offered only once the fragment's
// component begins with '.'. A missing or unreadable directory yields no
// suggestions rather than an error.
func atPathCompletions(cwd, frag string) ([][]rune, int) {
	dir, base := ".", frag
	if i := strings.LastIndex(frag, "/"); i >= 0 {
		dir, base = frag[:i+1], frag[i+1:]
	}

	listDir := dir
	if !filepath.IsAbs(dir) {
		listDir = filepath.Join(cwd, dir)
	}

	offset := utf8.RuneCountInString(base)
	entries, err := os.ReadDir(listDir)
	if err != nil {
		return nil, offset
	}

	var out [][]rune
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue // hide dotfiles unless explicitly reached for
		}
		suffix := name[len(base):]
		if e.IsDir() {
			suffix += "/"
		}
		out = append(out, []rune(suffix))
	}
	return out, offset
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
		if len(args) > 0 {
			r.printCommandHelp(args[0])
			break
		}
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

	case "/sessions":
		r.printSessions()

	case "/init":
		r.handleInit()

	default:
		if s := suggestCommand(cmd); s != "" {
			fmt.Printf("%s unknown command %q — did you mean %s?\n", red("✗"), cmd, cyan(s))
		} else {
			fmt.Printf("%s unknown command %q — try %s\n", red("✗"), cmd, cyan("/help"))
		}
	}
	return false
}

// knownCommands is the full set of slash commands (canonical plus aliases),
// derived from commandDetails so it can't drift from the handled set.
func knownCommands() []string {
	cmds := make([]string, 0, len(commandDetails)+2)
	for c := range commandDetails {
		cmds = append(cmds, c)
	}
	return append(cmds, "/exit", "/q")
}

// suggestCommand returns the known slash command closest to cmd within a small
// edit distance, or "" when nothing is close enough — so a genuine typo like
// "/hlep" suggests "/help" while unrelated input gets the plain error.
func suggestCommand(cmd string) string {
	best, bestDist := "", 0
	for _, k := range knownCommands() {
		d := levenshtein(cmd, k)
		if best == "" || d < bestDist {
			best, bestDist = k, d
		}
	}
	if best != "" && bestDist <= 2 {
		return best
	}
	return ""
}

// levenshtein returns the edit distance between a and b (byte-wise; slash
// command names are ASCII).
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
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

// costReport holds the formatted lines produced by buildCostReport.
type costReport struct {
	tokens   string
	cost     string
	footer   string
	isActual bool // true when sourced from API-reported usage, false when estimated
}

// buildCostReport computes the token and cost lines for the /cost display.
// When the session has API-reported usage it uses that; otherwise it falls back
// to the ~4-chars-per-token estimate. Separated from printCost for testability.
func buildCostReport(sess *session.Session, rate pricing.Rate, known bool, model string) costReport {
	if sess.ActualUsage.HasData() {
		au := sess.ActualUsage
		c := rate.CostWithCache(au.InputTokens, au.OutputTokens, au.CacheCreationTokens, au.CacheReadTokens)
		total := au.InputTokens + au.OutputTokens
		tokens := fmt.Sprintf("tokens: %s  (input %s / output %s / cache-write %s / cache-read %s)",
			commafy(total), commafy(au.InputTokens), commafy(au.OutputTokens),
			commafy(au.CacheCreationTokens), commafy(au.CacheReadTokens))
		var cost string
		switch {
		case known && rate.Local:
			cost = fmt.Sprintf("cost:   %s", dim("$0.00 (local)"))
		case known:
			cost = fmt.Sprintf("cost:   $%.4f  %s", c,
				dim(fmt.Sprintf("($%.0f/$%.0f per 1M in/out, cache 1.25×/0.1×)", rate.Input, rate.Output)))
		default:
			cost = fmt.Sprintf("cost:   %s", dim("unknown pricing for "+model))
		}
		return costReport{
			tokens:   tokens,
			cost:     cost,
			footer:   dim("actual usage — API-reported token counts."),
			isActual: true,
		}
	}

	// Fallback: estimate from message content.
	u := sess.EstimateUsage()
	c := rate.Cost(u.PromptTokens, u.GeneratedTokens)
	tokens := fmt.Sprintf("tokens: ~%s  (prompt ~%s / generated ~%s)",
		commafy(u.TotalTokens()), commafy(u.PromptTokens), commafy(u.GeneratedTokens))
	var cost string
	switch {
	case known && rate.Local:
		cost = fmt.Sprintf("cost:   %s", dim("$0.00 (local)"))
	case known:
		cost = fmt.Sprintf("cost:   ~$%.4f  %s", c,
			dim(fmt.Sprintf("($%.0f/$%.0f per 1M in/out)", rate.Input, rate.Output)))
	default:
		cost = fmt.Sprintf("cost:   %s", dim("unknown pricing for "+model))
	}
	return costReport{
		tokens:   tokens,
		cost:     cost,
		footer:   dim("estimate only — based on a ~4-chars-per-token heuristic."),
		isActual: false,
	}
}

// printCost reports the active model, token footprint, and dollar cost for the
// current session. Uses API-reported token counts when available; falls back to
// the ~4-chars-per-token estimate for old sessions and non-Anthropic providers.
func (r *REPL) printCost() {
	model := r.app.ActiveModel()
	sess, err := session.LoadByID(r.sessionID)
	if err != nil {
		fmt.Printf("%s %v\n", red("✗"), err)
		return
	}
	rate, known := pricing.Lookup(model)
	rep := buildCostReport(sess, rate, known, model)
	fmt.Printf("model:  %s\n", bold(r.app.ProviderInfo()))
	fmt.Println(rep.tokens)
	fmt.Println(rep.cost)
	fmt.Println(rep.footer)
}

func (r *REPL) printHistory() {
	sess, err := session.LoadByID(r.sessionID)
	if err != nil {
		fmt.Printf("%s %v\n", red("✗"), err)
		return
	}
	content, err := sess.ReadAllHistory()
	if err != nil {
		fmt.Printf("%s %v\n", red("✗"), err)
		return
	}
	if strings.TrimSpace(content) == "" {
		fmt.Println(dim("(no history yet)"))
		return
	}
	fmt.Println(content)
}

// printSessions lists recent sessions, mirroring the top-level `spai sessions`
// output. Because the REPL runs inside a live session, it additionally marks the
// currently active session (r.sessionID) with an arrow and a "(current)" label —
// a distinction the top-level command, which runs outside any session, can't make.
func (r *REPL) printSessions() {
	list, err := session.ListSessions()
	if err != nil {
		fmt.Printf("%s %v\n", red("✗"), err)
		return
	}

	pinned := session.ReadPinned()
	shellID := os.Getenv("SPAI_SESSION_ID")

	fmt.Println()
	fmt.Println("  Sessions")
	fmt.Println("  ────────────────────────────────────")

	if len(list) == 0 {
		fmt.Println(dim("  (no sessions yet)"))
		fmt.Println()
		return
	}

	for _, s := range list {
		current := s.ID == r.sessionID

		// Marker column: the active session wins (→), else pinned (*), else blank.
		marker := " "
		switch {
		case current:
			marker = "→"
		case s.ID == pinned:
			marker = "*"
		}

		// Label column: pinned takes precedence, then current, then shell.
		label := "        "
		switch {
		case s.ID == pinned:
			label = "(pinned)"
		case current:
			label = "(current)"
		case isShellSession(s.ID, shellID):
			label = "(shell) "
		}

		msgs := fmt.Sprintf("%d msgs", s.MsgCount)
		age := formatRelativeTime(s.ModTime)

		fmt.Printf("%s %-12s %s  %-8s  %s\n", marker, s.ID, label, msgs, age)
	}
	fmt.Println()
}

// formatRelativeTime renders a coarse "just now" / "5m ago" / "3h ago" / "2d ago"
// age. It mirrors the private helper of the same name in cmd/spai (a main package
// we can't import); the ~15 lines are duplicated deliberately.
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

// isShellSession reports whether id is the shell-launched session: either it
// matches $SPAI_SESSION_ID or it is a PID-based (all-numeric) id. Mirrors the
// private cmd/spai helper of the same name.
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

// printCommandHelp prints the detailed help for a single slash command, as
// requested via `/help <command>`. The leading slash is optional.
func (r *REPL) printCommandHelp(name string) {
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	detail, ok := commandDetails[name]
	if !ok {
		fmt.Printf("%s no help for %q — try %s\n", red("✗"), name, cyan("/help"))
		return
	}
	fmt.Printf("%s  %s\n", cyan(name), detail)
}

// commandDetails holds the long-form help shown by `/help <command>`. Its keys
// are the canonical slash commands; keep it in sync with handleSlash and
// helpText (slash_test.go asserts this).
var commandDetails = map[string]string{
	"/help":     "show the command list, or `/help <command>` for details on one command.",
	"/mode":     "show the execution mode, or set it: manual (confirm every tool), auto (run unattended), plan (draft a plan, run nothing). Shift-Tab cycles it.",
	"/model":    "show configured providers/models, or switch: `/model ollama`, `/model openai:gpt-4o`.",
	"/tools":    "list the tools available to the agent this session.",
	"/mcp":      "connect to the configured MCP servers and show their status and discovered tools.",
	"/cost":     "show the estimated token usage and dollar cost for this session.",
	"/clear":    "wipe the session's conversation context, keeping the session open.",
	"/compact":  "summarise the conversation so far and compact it to reclaim context.",
	"/history":  "print the full transcript recorded for this session.",
	"/sessions": "list recent sessions, marking the current, pinned, and shell sessions.",
	"/init":     "scaffold a SPAI.md project-context file in the current working directory.",
	"/quit":     "leave the session (aliases: /exit, /q; Ctrl+D also exits).",
}

const helpText = `
Commands:
  /help [command]    show this help, or details for one command
  /mode [m]          show or set execution mode (manual | auto | plan)
  /model [sel]       show providers, or switch (e.g. /model ollama, /model openai:gpt-4o)
  /tools             list the tools available to the agent
  /mcp               show MCP server connection status and discovered tools
  /cost              show estimated token usage and cost for this session
  /clear             wipe the session's conversation context
  /compact           summarise and compact the session to reclaim context
  /history           print the session transcript
  /sessions          list recent sessions (current, pinned, shell)
  /init              scaffold a SPAI.md project-context file here
  /quit, /exit       leave the session

References:
  @path              include a file's contents in your message; press Tab
                     after @ to complete file and directory names (a trailing
                     / marks a directory you can descend into).

Keys:
  Tab                complete /commands (at line start) and @paths (anywhere)
  Shift-Tab          cycle the execution mode (manual → auto → plan)
  Esc / Ctrl+C       cancel the current turn
  Ctrl+D             exit the session

`
