package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"spaios/internal/ai"
	"spaios/internal/parser"
	"spaios/internal/permissions"
	"spaios/internal/protocol"
	"spaios/internal/session"
)

// Config holds agent runtime settings, derived from spaid.toml [agent] section.
type Config struct {
	Autonomous    bool
	MaxIterations int
	Verbose       bool
	WorkingDir    string
	GitBranch     string
}

// ConfirmFunc is called when a tier-gated command needs user approval.
// Returns true if the user approved execution.
type ConfirmFunc func(req protocol.ConfirmRequest) bool

// ExecFunc runs a shell command and returns combined stdout+stderr and exit code.
// Injected for testing; production code uses defaultExec.
type ExecFunc func(ctx context.Context, cmd string) (output string, exitCode int)

// Agent orchestrates the agentic loop.
type Agent struct {
	provider  ai.Provider
	config    Config
	confirmFn ConfirmFunc
	execFn    ExecFunc
}

// New creates an Agent using the real shell executor.
func New(provider ai.Provider, config Config, confirmFn ConfirmFunc) *Agent {
	return &Agent{
		provider:  provider,
		config:    config,
		confirmFn: confirmFn,
		execFn:    defaultExec,
	}
}

// NewWithExec creates an Agent with an injected exec function. Used in tests.
func NewWithExec(provider ai.Provider, config Config, confirmFn ConfirmFunc, execFn ExecFunc) *Agent {
	return &Agent{
		provider:  provider,
		config:    config,
		confirmFn: confirmFn,
		execFn:    execFn,
	}
}

func defaultExec(ctx context.Context, cmd string) (string, int) {
	c := exec.CommandContext(ctx, "bash", "-c", cmd)
	var out strings.Builder
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return out.String(), exitErr.ExitCode()
		}
		return out.String(), 1
	}
	return out.String(), 0
}

const systemPrompt = `You are an autonomous system agent. Help the user accomplish their goal by running shell commands.

Rules:
1. Explain what you are doing in 1-2 sentences.
2. List exact shell commands in a single ` + "```bash" + ` code block — one per line, no comments.
3. If the goal is achieved or no commands are needed, omit the code block entirely.
4. If a previous command failed, diagnose why and propose a fix.
5. Never use interactive commands (vim, nano, top) — use non-interactive alternatives.`

// send writes resp to ch, returning false if ctx is cancelled first.
func send(ctx context.Context, ch chan<- protocol.Response, resp protocol.Response) bool {
	select {
	case ch <- resp:
		return true
	case <-ctx.Done():
		return false
	}
}

// Run starts the agent loop and returns a channel of Response chunks.
// The channel is closed when the loop ends. The last response always has Type "done".
// Session persistence is the caller's responsibility — the agent reads session history
// via sess.MessagesForPrompt() but does not call sess.AddExchange or sess.Save.
func (a *Agent) Run(ctx context.Context, req *protocol.AgentRequest, sess *session.Session) <-chan protocol.Response {
	ch := make(chan protocol.Response, 16)
	go func() {
		defer close(ch)
		a.loop(ctx, req, sess, ch)
	}()
	return ch
}

func (a *Agent) loop(ctx context.Context, req *protocol.AgentRequest, sess *session.Session, ch chan<- protocol.Response) {
	sysCtx := fmt.Sprintf("Working directory: %s", a.config.WorkingDir)
	if a.config.GitBranch != "" {
		sysCtx += fmt.Sprintf("\nGit branch: %s", a.config.GitBranch)
	}

	messages := []ai.Message{
		{Role: "system", Content: systemPrompt + "\n\nSystem context:\n" + sysCtx},
	}
	messages = append(messages, sess.MessagesForPrompt()...)
	messages = append(messages, ai.Message{Role: "user", Content: req.Query})

	autonomous := a.config.Autonomous || req.Autonomous
	verbose := a.config.Verbose || req.Verbose
	maxIter := a.config.MaxIterations
	if maxIter <= 0 {
		maxIter = 5
	}

	for iter := 0; iter < maxIter; iter++ {
		if verbose && iter > 0 {
			if !send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("\n── iteration %d/%d ──\n", iter+1, maxIter)}) {
				return
			}
		}

		// Call AI
		textCh, err := a.provider.Complete(ctx, messages)
		if err != nil {
			send(ctx, ch, protocol.Response{Type: "error", Content: fmt.Sprintf("AI error: %v", err)})
			send(ctx, ch, protocol.Response{Type: "done"})
			return
		}

		var fullText strings.Builder
		for chunk := range textCh {
			fullText.WriteString(chunk)
			if !send(ctx, ch, protocol.Response{Type: "text", Content: chunk}) {
				return
			}
		}

		aiText := fullText.String()
		commands := parser.ParseCommands(aiText)

		// No commands = goal achieved
		if len(commands) == 0 {
			send(ctx, ch, protocol.Response{Type: "done"})
			return
		}

		// Execute each command
		var allOutput strings.Builder
		failedAt := -1

		for cmdIdx, cmd := range commands {
			tier := permissions.Classify(cmd)
			needsConfirm := !autonomous &&
				tier != permissions.TierPassthrough &&
				tier != permissions.TierRead

			if needsConfirm {
				approved := a.confirmFn(protocol.ConfirmRequest{
					Command:   cmd,
					Tier:      tier.String(),
					Display:   tier.Display(),
					Iteration: iter + 1,
				})
				if !approved {
					send(ctx, ch, protocol.Response{Type: "text", Content: "\nCancelled by user.\n"})
					send(ctx, ch, protocol.Response{Type: "done"})
					return
				}
			}

			if verbose {
				if !send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("▶ [%s] %s\n", tier.Display(), cmd)}) {
					return
				}
			} else {
				if !send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("▶ %s\n", cmd)}) {
					return
				}
			}

			output, exitCode := a.execFn(ctx, cmd)
			allOutput.WriteString(fmt.Sprintf("$ %s\n%s\n", cmd, output))

			if exitCode != 0 || verbose {
				if !send(ctx, ch, protocol.Response{Type: "output", Content: output}) {
					return
				}
			}

			if exitCode != 0 {
				failedAt = cmdIdx
				break
			}
		}

		// Build feedback message for next AI call
		messages = append(messages, ai.Message{Role: "assistant", Content: aiText})

		if failedAt >= 0 {
			messages = append(messages, ai.Message{
				Role: "user",
				Content: fmt.Sprintf(
					"Command failed (exit non-zero):\n%s\nDiagnose and propose a fix. If unfixable, explain why and respond with no code block.",
					allOutput.String(),
				),
			})
		} else {
			messages = append(messages, ai.Message{
				Role: "user",
				Content: fmt.Sprintf(
					"Commands completed successfully. Output:\n%s\nIf the goal is achieved, respond with no code block. Otherwise continue.",
					allOutput.String(),
				),
			})
		}
	}

	// max_iterations reached
	send(ctx, ch, protocol.Response{Type: "text", Content: fmt.Sprintf("\nReached iteration limit (%d). Here is where things stand.\n", maxIter)})
	send(ctx, ch, protocol.Response{Type: "done"})
}

