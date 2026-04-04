package router

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"spaios/internal/ai"
	"spaios/internal/config"
	"spaios/internal/permissions"
	"spaios/internal/protocol"
	"spaios/internal/session"
)

var bashBlockRe = regexp.MustCompile("(?s)```(?:bash|sh|shell)?\n(.*?)```")

const systemPrompt = `You are a Linux system assistant integrated into the user's shell.
Help the user accomplish their request.

Rules:
1. Explain what you found or plan to do in 1-3 sentences.
2. List the exact shell commands to run in a single ` + "```bash" + ` code block.
3. Each command must be on its own line inside the code block.
4. If no commands are needed, omit the code block entirely.
5. Never propose interactive commands (vim, nano, top) — use non-interactive alternatives.
6. Do not include comments (#) inside the code block — commands only.`

// Router selects an AI provider and processes queries.
type Router struct {
	cfg   *config.Config
	cloud ai.Provider
	local ai.Provider
}

// New creates a Router with the given config and providers.
func New(cfg *config.Config, cloud, local ai.Provider) *Router {
	return &Router{cfg: cfg, cloud: cloud, local: local}
}

// Route processes a query request and returns a channel of Response chunks.
// Chunks: zero or more "text" chunks, one optional "plan" chunk, one "done" chunk.
func (r *Router) Route(ctx context.Context, req *protocol.Request, sess *session.Session) (<-chan protocol.Response, error) {
	provider, err := r.selectProvider(req.ForceLocal)
	if err != nil {
		return nil, err
	}

	messages := r.buildMessages(req, sess)
	textCh, err := provider.Complete(ctx, messages)
	if err != nil {
		return nil, err
	}

	respCh := make(chan protocol.Response)
	go func() {
		defer close(respCh)

		var fullText strings.Builder
		for chunk := range textCh {
			fullText.WriteString(chunk)
			respCh <- protocol.Response{Type: "text", Content: chunk}
		}

		commands := parseCommands(fullText.String())
		if len(commands) > 0 {
			plan := make([]protocol.CommandItem, len(commands))
			for i, cmd := range commands {
				tier := permissions.Classify(cmd)
				plan[i] = protocol.CommandItem{
					Command: cmd,
					Tier:    tier.String(),
					Display: tier.Display(),
				}
			}
			respCh <- protocol.Response{Type: "plan", Plan: plan}
		}

		respCh <- protocol.Response{Type: "done"}
	}()

	return respCh, nil
}

func (r *Router) selectProvider(forceLocal bool) (ai.Provider, error) {
	if forceLocal || r.cfg.Routing.PreferLocal {
		if r.local.Available() {
			return r.local, nil
		}
		return nil, fmt.Errorf("local model not available — is your local model runtime running?")
	}
	if r.cloud.Available() {
		return r.cloud, nil
	}
	if r.local.Available() {
		return r.local, nil
	}
	return nil, fmt.Errorf("no AI provider available — set %s or start a local model", r.cfg.Provider.APIKeyEnv)
}

func (r *Router) buildMessages(req *protocol.Request, sess *session.Session) []ai.Message {
	ctx := fmt.Sprintf("Working directory: %s", req.WorkingDir)
	if req.GitBranch != "" {
		ctx += fmt.Sprintf("\nGit branch: %s", req.GitBranch)
	}
	sysMsg := systemPrompt + "\n\nSystem context:\n" + ctx
	msgs := []ai.Message{{Role: "system", Content: sysMsg}}
	msgs = append(msgs, sess.MessagesForPrompt()...)
	msgs = append(msgs, ai.Message{Role: "user", Content: req.Query})
	return msgs
}

// parseCommands extracts shell commands from ```bash code blocks in the AI response.
func parseCommands(text string) []string {
	matches := bashBlockRe.FindAllStringSubmatch(text, -1)
	var commands []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(match[1]), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				commands = append(commands, line)
			}
		}
	}
	return commands
}
