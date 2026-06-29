package agent

import (
	"context"
	"fmt"
	"strings"

	"spaish/internal/ai"
	"spaish/internal/permissions"
	"spaish/internal/protocol"
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
}

// ConfirmFunc is called when a tier-gated tool call needs user approval.
type ConfirmFunc func(req protocol.ConfirmRequest) bool

// Agent orchestrates the tool-calling loop.
type Agent struct {
	provider  ai.Provider
	config    Config
	confirmFn ConfirmFunc
	registry  *tools.Registry
}

// New creates an Agent with the default tool registry.
func New(provider ai.Provider, config Config, confirmFn ConfirmFunc) *Agent {
	return NewWithRegistry(provider, config, confirmFn, tools.DefaultRegistry())
}

// NewWithRegistry creates an Agent with an injected tool registry. Used in tests.
func NewWithRegistry(provider ai.Provider, config Config, confirmFn ConfirmFunc, registry *tools.Registry) *Agent {
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
	system := systemPrompt + "\n\nWorking directory: " + a.config.WorkingDir
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

	for iter := 0; iter < maxIter; iter++ {
		evCh, err := a.provider.Stream(ctx, ai.Request{System: system, Messages: messages, Tools: toolSpecs})
		if err != nil {
			send(ctx, ch, protocol.Response{Type: "error", Content: fmt.Sprintf("AI error: %v", err)})
			send(ctx, ch, protocol.Response{Type: "done"})
			return
		}

		var text strings.Builder
		var toolCalls []ai.ToolCall
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
			}
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
			if mode == ModeManual && tier != permissions.TierPassthrough && tier != permissions.TierRead {
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

			out, runErr := tool.Run(ctx, tc.Input)
			isErr := runErr != nil
			content := out
			if isErr {
				content = runErr.Error()
			}
			results = append(results, ai.ToolResult{ToolUseID: tc.ID, Content: content, IsError: isErr})

			if isErr || verbose {
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
	case "write_file", "edit_file":
		return permissions.TierWrite, tc.Name + " " + tools.PathArg(tc.Input)
	default: // read_file, glob, grep, list_dir
		return permissions.TierRead, tc.Name + " " + tools.PathArg(tc.Input)
	}
}
