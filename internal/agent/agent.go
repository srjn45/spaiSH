package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"spaish/internal/ai"
	"spaish/internal/hooks"
	"spaish/internal/permissions"
	"spaish/internal/protocol"
	"spaish/internal/sandbox"
	"spaish/internal/session"
	"spaish/internal/tools"
)

// Config holds agent runtime settings, derived from spaid.toml [agent] section.
// Execution modes for tool calls.
const (
	ModeManual = "manual" // confirm Write/Elevated/Destructive tool calls
	ModeAuto   = "auto"   // execute everything without confirmation
	ModePlan   = "plan"   // show the planned tool calls but never execute
)

type Config struct {
	Autonomous    bool
	Mode          string // "manual" (default) | "auto" | "plan"; overrides Autonomous
	MaxIterations int
	Verbose       bool
	WorkingDir    string
	GitBranch     string
	Stdin         string // content from piped stdin; injected before the query

	// Policy is the configurable per-tool / per-MCP-server / bash-allowlist
	// gating layer consulted before the tier-based confirm gate. The zero value
	// is an empty policy, so leaving it unset preserves the legacy behavior.
	Policy permissions.Policy

	// Sandbox is the opt-in execution containment applied inside bash/code_exec,
	// under (never in place of) the permission gate. nil means no sandbox. It is
	// inherited by delegated sub-agents so defense-in-depth extends to them.
	Sandbox sandbox.Sandbox

	// Hooks are user-configured shell hooks run AROUND tool execution, layered on
	// top of (never in place of) the permission gate. The zero value has no hooks,
	// preserving legacy behaviour. A pre_tool hook may only BLOCK an already-
	// approved call; it can never auto-approve or change a tool's tier. Like
	// Sandbox, it is inherited by delegated sub-agents.
	Hooks hooks.Runner
	// Trusted marks bash commands exempt from the sandbox (the allow_commands
	// carve-out). nil trusts nothing.
	Trusted func(cmd string) bool

	// SubagentProfiles is the merged list of user-configured and built-in named
	// agent profiles. It is consulted by runDelegate when the model names a profile
	// in a delegate tool call. The zero value (nil) falls back to built-in defaults.
	SubagentProfiles []SubagentProfile

	// SystemPromptOverride, when non-empty, replaces the default agent system
	// prompt for this agent instance. It is set on delegated sub-agents to inject
	// a profile's focused system prompt without changing the top-level agent.
	SystemPromptOverride string

	// ModelOverride, when non-empty, is passed as Request.Model in every provider
	// Stream call inside the agent loop. It selects the "strong" model for
	// reasoning turns when task-based routing is configured. An empty string keeps
	// the provider's own configured model, preserving all existing behaviour.
	ModelOverride string
}

// ConfirmFunc is called when a tier-gated tool call needs user approval.
type ConfirmFunc func(req protocol.ConfirmRequest) bool

// Agent orchestrates the tool-calling loop.
type Agent struct {
	provider  ai.Provider
	config    Config
	confirmFn ConfirmFunc
	registry  *tools.Registry

	// projectContext caches the SPAI.md lookup (loadProjectContext) for this
	// Agent instance so it is read from disk once per session rather than on
	// every turn. It is populated lazily and guarded by projectContextOnce,
	// which also makes the compute-once safe if loop() goroutines ever overlap.
	// The cache is per-Agent-instance: each delegated sub-agent (built with its
	// own Config.WorkingDir in runDelegate) gets a fresh, independently-scoped
	// cache. Tradeoff: a SPAI.md edited mid-session is only picked up after
	// restarting spai — acceptable for a simple per-process cache (no watching
	// or invalidation by design).
	projectContextOnce sync.Once
	projectContext     string
}

// loadProjectContextCached returns the SPAI.md project context for this Agent,
// computing it from disk at most once per instance and caching the result.
func (a *Agent) loadProjectContextCached() string {
	a.projectContextOnce.Do(func() {
		a.projectContext = loadProjectContext(a.config.WorkingDir)
	})
	return a.projectContext
}

// New creates an Agent with the default tool registry.
func New(provider ai.Provider, config Config, confirmFn ConfirmFunc) *Agent {
	return NewWithRegistry(provider, config, confirmFn, tools.DefaultRegistry())
}

// NewWithRegistry creates an Agent with an injected tool registry. Used in tests.
func NewWithRegistry(provider ai.Provider, config Config, confirmFn ConfirmFunc, registry *tools.Registry) *Agent {
	// Wire up the "delegate" (subagent) tool here rather than in
	// tools.DefaultRegistry(): it needs a closure over the provider, the real
	// confirmFn, and a child Config, none of which DefaultRegistry() can see.
	// The runner (runDelegate) builds the nested Agent with a plain
	// DefaultRegistry — which does NOT include this tool — so recursion depth
	// can never exceed 1. Registry.Add is a no-op if "delegate" is already
	// present, so this is safe to call unconditionally.
	registry.Add(tools.NewDelegate(func(ctx context.Context, task, profile string) (string, error) {
		return runDelegate(ctx, provider, confirmFn, childConfig(config), task, profile)
	}))
	return &Agent{provider: provider, config: config, confirmFn: confirmFn, registry: registry}
}

const systemPrompt = `You are spaiSH, an AI assistant embedded in the user's terminal. Accomplish the user's goal using the available tools.

Guidelines:
1. Briefly explain what you are about to do (1-2 sentences) before acting.
2. Use tools to inspect and change the system. Prefer the dedicated file tools (read_file, write_file, edit_file, glob, grep, list_dir) over bash for file work.
3. Never run interactive commands (vim, nano, top, less) — use non-interactive alternatives.
4. If a command fails, diagnose why from its output and try a fix.
5. When the goal is achieved, stop calling tools and give a short summary of what you did.`

// send writes resp to ch, returning false if ctx is cancelled first.
func send(ctx context.Context, ch chan<- protocol.Response, resp protocol.Response) bool {
	select {
	case ch <- resp:
		return true
	case <-ctx.Done():
		return false
	}
}

// Run starts the agent loop and returns a channel of Response chunks. The
// channel is closed when the loop ends; the last response has Type "done".
func (a *Agent) Run(ctx context.Context, req *protocol.AgentRequest, sess *session.Session) <-chan protocol.Response {
	ch := make(chan protocol.Response, 16)
	go func() {
		defer close(ch)
		a.loop(ctx, req, sess, ch)
	}()
	return ch
}

func (a *Agent) loop(ctx context.Context, req *protocol.AgentRequest, sess *session.Session, ch chan<- protocol.Response) {
	base := systemPrompt
	if a.config.SystemPromptOverride != "" {
		base = a.config.SystemPromptOverride
	}
	system := base + "\n\nWorking directory: " + a.config.WorkingDir
	if spaiCtx := a.loadProjectContextCached(); spaiCtx != "" {
		system += "\n\n## Project instructions (SPAI.md)\n" + spaiCtx
	}
	if a.config.GitBranch != "" {
		system += "\nGit branch: " + a.config.GitBranch
	}

	messages := append([]ai.Message(nil), sess.MessagesForPrompt()...)
	if a.config.Stdin != "" {
		messages = append(messages, ai.Message{Role: "user", Content: "[piped input]\n" + a.config.Stdin})
	}
	messages = append(messages, ai.Message{Role: "user", Content: req.Query})

	mode := a.config.Mode
	if mode == "" {
		if a.config.Autonomous || req.Autonomous {
			mode = ModeAuto
		} else {
			mode = ModeManual
		}
	}
	verbose := a.config.Verbose || req.Verbose
	maxIter := a.config.MaxIterations
	if maxIter <= 0 {
		maxIter = 25
	}
	toolSpecs := a.registry.Specs()

	// Thread a checkpointer through ctx so mutating tools snapshot files before
	// they write. The store is keyed by (project, session) and resolved from the
	// same working directory the REPL's /undo, /redo use, so they share history.
	if sess != nil {
		ctx = tools.WithCheckpointer(ctx, session.NewCheckpointStore(sess.ID(), a.config.WorkingDir))
	}

	// Accumulate real API-reported usage across all iterations of this Run()
	// call. Deferred so it runs on every exit path.
	var totalUsage ai.Usage
	defer func() {
		if totalUsage.InputTokens > 0 || totalUsage.OutputTokens > 0 {
			sess.AddActualUsage(totalUsage)
		}
	}()

	for iter := 0; iter < maxIter; iter++ {
		evCh, err := a.provider.Stream(ctx, ai.Request{System: system, Messages: messages, Tools: toolSpecs, Model: a.config.ModelOverride})
		if err != nil {
			send(ctx, ch, protocol.Response{Type: "error", Content: fmt.Sprintf("AI error: %v", err)})
			send(ctx, ch, protocol.Response{Type: "done"})
			return
		}

		var text strings.Builder
		var toolCalls []ai.ToolCall
		var iterUsage *ai.Usage
		for ev := range evCh {
			switch ev.Type {
			case "text":
				text.WriteString(ev.Text)
				if !send(ctx, ch, protocol.Response{Type: "text", Content: ev.Text}) {
					return
				}
			case "tool_call":
				toolCalls = append(toolCalls, *ev.ToolCall)
			case "error":
				send(ctx, ch, protocol.Response{Type: "error", Content: ev.Err})
				send(ctx, ch, protocol.Response{Type: "done"})
				return
			case "done":
				iterUsage = ev.Usage
			}
		}
		if iterUsage != nil {
			totalUsage.InputTokens += iterUsage.InputTokens
			totalUsage.OutputTokens += iterUsage.OutputTokens
			totalUsage.CacheCreationTokens += iterUsage.CacheCreationTokens
			totalUsage.CacheReadTokens += iterUsage.CacheReadTokens
		}

		messages = append(messages, ai.Message{Role: "assistant", Content: text.String(), ToolCalls: toolCalls})

		// No tool calls => the model is done.
		if len(toolCalls) == 0 {
			send(ctx, ch, protocol.Response{Type: "done"})
			return
		}

		// Plan mode: show the proposed tool calls and stop without executing.
		if mode == ModePlan {
			for _, tc := range toolCalls {
				tier, display := classify(tc)
				send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("▶ (plan) [%s] %s\n", tier.Display(), display)})
			}
			send(ctx, ch, protocol.Response{Type: "done"})
			return
		}

		results := make([]ai.ToolResult, 0, len(toolCalls))
		for _, tc := range toolCalls {
			tool, ok := a.registry.Get(tc.Name)
			if !ok {
				results = append(results, ai.ToolResult{ToolUseID: tc.ID, Content: "unknown tool: " + tc.Name, IsError: true})
				continue
			}

			tier, display := classify(tc)

			// Consult the configurable policy before the tier-based gate. A
			// bash command is passed so the allowlist can match on it.
			var bashCmd string
			if tc.Name == "bash" {
				bashCmd = tools.Command(tc.Input)
			}
			decision := a.config.Policy.Decide(tc.Name, bashCmd)
			if decision == permissions.DecisionDeny {
				// Blocked in every mode: never execute, report an error result,
				// and continue the loop so the model can adapt.
				content := "blocked by permission policy: " + tc.Name
				results = append(results, ai.ToolResult{ToolUseID: tc.ID, Content: content, IsError: true})
				send(ctx, ch, protocol.Response{Type: "output", Content: content + "\n"})
				continue
			}

			// allow bypasses confirmation entirely; confirm/default fall through
			// to the existing tier-based gate.
			needConfirm := decision != permissions.DecisionAllow &&
				mode == ModeManual && tier != permissions.TierPassthrough && tier != permissions.TierRead
			if needConfirm {
				creq := protocol.ConfirmRequest{
					Command:   display,
					Tier:      tier.String(),
					Display:   tier.Display(),
					Iteration: iter + 1,
				}
				// For file-editing tools, attach a preview of the change so the
				// confirmation UI can show a diff before the y/N prompt. The
				// content is computed without touching disk; the actual write
				// only happens later via tool.Run after approval.
				if path, oldC, newC, ok := tools.PreviewEdit(tc.Name, tc.Input); ok {
					creq.ShowDiff = true
					creq.Path = path
					creq.OldContent = oldC
					creq.NewContent = newC
				}
				approved := a.confirmFn(creq)
				if !approved {
					send(ctx, ch, protocol.Response{Type: "text", Content: "\nCancelled by user.\n"})
					send(ctx, ch, protocol.Response{Type: "done"})
					return
				}
			}

			if verbose {
				send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("▶ [%s] %s\n", tier.Display(), display)})
			} else {
				send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("▶ %s\n", display)})
			}

			// pre_tool hooks fire only AFTER the tier/confirm gate has approved this
			// call (a cancelled call returns above and never reaches here) and BEFORE
			// the tool runs. A block refuses the already-permitted tool — pure
			// defense-in-depth, never a bypass. Mirrors the Policy-deny path above:
			// error result + continue, and tool.Run is NOT called.
			preInv := hooks.Invocation{Tool: tc.Name, Input: tc.Input}
			if be := a.config.Hooks.RunPre(ctx, preInv); be != nil {
				content := be.Error()
				results = append(results, ai.ToolResult{ToolUseID: tc.ID, Content: content, IsError: true})
				send(ctx, ch, protocol.Response{Type: "output", Content: content + "\n"})
				continue
			}

			out, runErr := tool.Run(ctx, tc.Input)
			isErr := runErr != nil
			content := out
			if isErr {
				content = runErr.Error()
			}
			result := ai.ToolResult{ToolUseID: tc.ID, Content: content, IsError: isErr}
			// Tools that produce images (e.g. read_image) attach them to the
			// result so vision-capable providers can forward them to the model.
			if ip, ok := tool.(tools.ImageProducer); ok && !isErr {
				if imgs, imgErr := ip.Images(tc.Input); imgErr == nil {
					result.Images = imgs
				}
			}
			results = append(results, result)

			// post_tool hooks are observe-only and run only when the tool SUCCEEDED.
			// The result above is already recorded; a hook failure is surfaced but
			// never undoes the tool.
			if !isErr {
				postInv := hooks.Invocation{Tool: tc.Name, Input: tc.Input, Output: out}
				for _, hf := range a.config.Hooks.RunPost(ctx, postInv) {
					send(ctx, ch, protocol.Response{Type: "output",
						Content: fmt.Sprintf("post_tool hook failed (%s): %s\n", hf.Command, hf.Reason)})
				}
			}

			if tc.Name == "todo_write" && !isErr {
				send(ctx, ch, protocol.Response{Type: "todo", Content: out})
			} else if isErr || verbose {
				send(ctx, ch, protocol.Response{Type: "output", Content: content + "\n"})
			}
		}

		messages = append(messages, ai.Message{Role: "user", ToolResults: results})
	}

	send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("\nReached iteration limit (%d). Stopping.\n", maxIter)})
	send(ctx, ch, protocol.Response{Type: "done"})
}

// classify returns the permission tier and a human-readable display string for
// a tool call.
func classify(tc ai.ToolCall) (permissions.Tier, string) {
	switch tc.Name {
	case "bash":
		cmd := tools.Command(tc.Input)
		return permissions.Classify(cmd), cmd
	case "http_request":
		return permissions.TierElevated, "http_request " + tools.URLArg(tc.Input)
	case "web_search":
		// Read-only network search (like web_fetch): no confirmation gate.
		return permissions.TierRead, "web_search " + tools.QueryArg(tc.Input)
	case "write_file", "edit_file", "multi_edit":
		return permissions.TierWrite, tc.Name + " " + tools.PathArg(tc.Input)
	case "git":
		sub, args := tools.GitCall(tc.Input)
		display := strings.TrimSpace("git " + sub + " " + strings.Join(args, " "))
		return tools.GitTier(sub, args), display
	case "gh":
		sub, args := tools.GHCall(tc.Input)
		display := strings.TrimSpace("gh " + sub + " " + strings.Join(args, " "))
		return tools.GHTier(sub), display
	case "todo_write":
		return permissions.TierRead, "updating task list"
	case "code_exec":
		// Runs arbitrary Python/Node with the same OS privileges as bash and is
		// not sandboxed, so it gets bash's worst-case tier — no easier ride just
		// because it looks like a narrower tool.
		return permissions.TierDestructive, tc.Name
	case "read_image":
		// Reading an image is a read, not a mutation: no confirmation gate.
		return permissions.TierRead, tc.Name + " " + tools.PathArg(tc.Input)
	case "delegate":
		// Delegating a task spawns a whole nested agent loop — a meaningful,
		// non-obvious action worth a top-level confirmation in manual mode, even
		// though the nested loop independently re-gates each of its own
		// sub-actions through this same confirmFn. TierWrite (not a stronger tier)
		// because delegation itself performs no mutation: the sub-agent's actual
		// Write/Destructive calls are each confirmed on their own merits.
		task := tools.TaskArg(tc.Input)
		if p := tools.ProfileArg(tc.Input); p != "" {
			return permissions.TierWrite, fmt.Sprintf("delegate[%s]: %s", p, task)
		}
		return permissions.TierWrite, "delegate: " + task
	default:
		// MCP tools (mcp__<server>__<tool>) are external; gate them at Write
		// tier so they require confirmation in manual mode.
		if strings.HasPrefix(tc.Name, "mcp__") {
			return permissions.TierWrite, tc.Name
		}
		// read_file, glob, grep, list_dir
		return permissions.TierRead, tc.Name + " " + tools.PathArg(tc.Input)
	}
}
