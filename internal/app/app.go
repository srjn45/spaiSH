// Package app wires together configuration, AI providers, the agent loop, local
// model management, and session persistence. It is the in-process replacement
// for the former spaid daemon: the spai CLI calls these methods directly instead
// of round-tripping over a Unix socket.
package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"spaish/internal/agent"
	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/llm"
	"spaish/internal/protocol"
	"spaish/internal/session"
)

// App holds the shared runtime: config, providers, and the local model manager.
type App struct {
	cfg    *config.Config
	cloud  ai.Provider
	local  ai.Provider
	llmMgr *llm.Manager
}

// Handler receives streamed responses from a running request.
type Handler func(protocol.Response)

// ConfigPath returns the path to the user's spaid.toml configuration file.
func ConfigPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaish", "spaid.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaish", "spaid.toml")
}

// New constructs an App from the user's configuration and local model state.
// A missing config file is non-fatal: built-in defaults are used.
func New() *App {
	cfg, err := config.Load(ConfigPath())
	if err != nil {
		cfg = &config.Config{}
	}

	llmState, err := llm.LoadState(llm.DefaultStatePath())
	if err != nil {
		log.Printf("llm state load warning: %v — using defaults", err)
		llmState, _ = llm.LoadState(llm.DefaultStatePath())
	}
	llmMgr := llm.NewManager(llmState)

	// Prefer the active model from llm-state over the config value, so that
	// `spai llm use <model>` takes effect without editing the config.
	localModel := cfg.Local.LocalModel
	if llmState.ActiveModel != "" {
		localModel = llmState.ActiveModel
	}

	cloud := buildCloudProvider(cfg)
	var local ai.Provider
	switch llmState.ActiveRuntime {
	case "bitnet":
		rt, _ := llm.Get("bitnet")
		local = ai.NewOpenAIProvider(rt.Endpoint+"/v1", "", localModel)
	default: // "ollama" or unset
		local = ai.NewLocalProvider(cfg.Local.OllamaEndpoint, localModel)
	}

	return &App{cfg: cfg, cloud: cloud, local: local, llmMgr: llmMgr}
}

// buildCloudProvider constructs the remote provider from config. It defaults to
// the native Anthropic provider; set [provider].kind = "openai" for an
// OpenAI-compatible endpoint.
func buildCloudProvider(cfg *config.Config) ai.Provider {
	switch cfg.Provider.Kind {
	case "openai":
		return ai.NewOpenAIProvider(cfg.Provider.Endpoint, cfg.APIKey(), cfg.Provider.Model)
	default: // "anthropic" or unset
		key := cfg.APIKey()
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		return ai.NewAnthropicProvider(key, cfg.Provider.Model)
	}
}

// ProviderInfo returns a short description of the configured remote provider and
// model, for display in the REPL.
func (a *App) ProviderInfo() string {
	model := a.cfg.Provider.Model
	if model == "" && a.cfg.Provider.Kind != "openai" {
		model = ai.DefaultAnthropicModel
	}
	name := a.cloud.Name()
	avail := "not configured"
	if a.cloud.Available() {
		avail = "ready"
	}
	if model == "" {
		return fmt.Sprintf("%s (%s)", name, avail)
	}
	return fmt.Sprintf("%s / %s (%s)", name, model, avail)
}

func (a *App) providers() ai.ProviderSet {
	return ai.ProviderSet{
		Cloud:       a.cloud,
		Local:       a.local,
		PreferLocal: a.cfg.Routing.PreferLocal,
		APIKeyEnv:   a.cfg.Provider.APIKeyEnv,
	}
}

// loadSession returns the session for id, falling back to a fresh session.
func loadSession(id string) *session.Session {
	if id == "" {
		id = "default"
	}
	sess, err := session.LoadByID(id)
	if err != nil {
		log.Printf("session load warning (id=%s): %v — starting fresh", id, err)
		return new(session.Session)
	}
	return sess
}

// RunAgent runs the agentic loop in-process, streaming responses to handle and
// persisting the exchange to the session afterwards. confirmFn is invoked when a
// tier-gated command needs user approval.
func (a *App) RunAgent(ctx context.Context, req *protocol.Request, confirmFn agent.ConfirmFunc, handle Handler) error {
	if req.Agent == nil {
		return fmt.Errorf("missing agent payload")
	}

	sess := loadSession(req.SessionID)
	provider, err := a.providers().Select(req.ForceLocal)
	if err != nil {
		return err
	}

	mode := req.Agent.Mode
	if mode == "" {
		mode = agent.ModeManual
		switch {
		case req.DryRun:
			mode = agent.ModePlan
		case a.cfg.Agent.Autonomous || req.Agent.Autonomous:
			mode = agent.ModeAuto
		}
	}

	agentCfg := agent.Config{
		Mode:          mode,
		MaxIterations: a.cfg.Agent.MaxIterations,
		Verbose:       a.cfg.Agent.Verbose || req.Agent.Verbose,
		WorkingDir:    req.WorkingDir,
		GitBranch:     req.GitBranch,
		Stdin:         req.Stdin,
	}

	ag := agent.New(provider, agentCfg, confirmFn)

	var fullText, outputText strings.Builder
	for resp := range ag.Run(ctx, req.Agent, sess) {
		handle(resp)
		switch resp.Type {
		case "text":
			fullText.WriteString(resp.Content)
		case "output":
			outputText.WriteString(resp.Content)
		}
	}

	sess.AddExchange(req.Agent.Query, fullText.String())
	a.maybeAutoCompact(ctx, sess, provider)
	if err := sess.SaveCache(); err != nil {
		log.Printf("session save error: %v", err)
	}
	sess.AppendHistory(time.Now().UTC(), req.Agent.Query, fullText.String(), outputText.String())
	return nil
}

// autoCompactTokens is the approximate prompt-context budget above which the
// session is summarised to keep future requests bounded.
const autoCompactTokens = 100_000

// maybeAutoCompact summarises older messages when the session's estimated token
// footprint exceeds the budget, keeping the most recent few turns verbatim. It
// is best-effort: on any error the session is left unchanged.
func (a *App) maybeAutoCompact(ctx context.Context, sess *session.Session, provider ai.Provider) {
	const keep = 4
	if sess.ApproxTokens() <= autoCompactTokens || len(sess.Messages) <= keep {
		return
	}
	older := sess.Messages[:len(sess.Messages)-keep]
	msgs := make([]ai.Message, 0, len(older)+2)
	if sess.Summary != "" {
		msgs = append(msgs, ai.Message{Role: "user", Content: "Earlier summary:\n" + sess.Summary})
	}
	msgs = append(msgs, older...)
	msgs = append(msgs, ai.Message{Role: "user", Content: "Summarise the conversation so far in one concise paragraph, preserving facts and decisions needed to continue."})

	summary, err := ai.CompleteText(ctx, provider, "You compress conversation history into a concise summary for context continuity.", msgs)
	if err != nil || summary == "" {
		log.Printf("auto-compact skipped: %v", err)
		return
	}
	sess.CompactOlder(summary, keep)
	log.Printf("auto-compacted session to ~%d tokens", sess.ApproxTokens())
}

// RunLLM dispatches a local-model management command (status/install/list/...).
func (a *App) RunLLM(req *protocol.LLMRequest, handle Handler) {
	for resp := range a.llmMgr.Handle(req) {
		handle(resp)
	}
}

// RunSession handles session maintenance commands: clear, compact, rebuild-context.
func (a *App) RunSession(ctx context.Context, req *protocol.Request, handle Handler) {
	if req.Session == nil {
		handle(protocol.Response{Type: "error", Content: "missing session payload"})
		return
	}
	sess := loadSession(req.SessionID)

	switch req.Session.Command {
	case "clear":
		if req.Session.Lines == 0 {
			if err := sess.Clear(); err != nil {
				log.Printf("session clear error: %v", err)
			}
			handle(protocol.Response{Type: "text", Content: "Session cleared.\n"})
		} else {
			sess.Trim(req.Session.Lines)
			if err := sess.SaveCache(); err != nil {
				log.Printf("session save error: %v", err)
			}
			handle(protocol.Response{Type: "text", Content: fmt.Sprintf("Session trimmed to %d messages.\n", req.Session.Lines)})
		}

	case "compact":
		if len(sess.Messages) == 0 && sess.Summary == "" {
			handle(protocol.Response{Type: "text", Content: "Nothing to compact — session is empty.\n"})
			return
		}
		provider, err := a.providers().Select(req.ForceLocal)
		if err != nil {
			handle(protocol.Response{Type: "error", Content: err.Error()})
			return
		}
		msgs := []ai.Message{{Role: "system", Content: "Summarise the following conversation concisely. Focus on what was worked on and what was achieved. One short paragraph."}}
		msgs = append(msgs, sess.Messages...)
		summary, err := streamText(ctx, provider, msgs, handle)
		if err != nil {
			handle(protocol.Response{Type: "error", Content: err.Error()})
			return
		}
		sess.Compact(summary)
		if err := sess.SaveCache(); err != nil {
			log.Printf("session save error: %v", err)
		}

	case "rebuild-context":
		history, err := sess.ReadAllHistory()
		if err != nil || history == "" {
			handle(protocol.Response{Type: "text", Content: "No history to rebuild from.\n"})
			return
		}
		provider, err := a.providers().Select(req.ForceLocal)
		if err != nil {
			handle(protocol.Response{Type: "error", Content: err.Error()})
			return
		}
		msgs := []ai.Message{
			{Role: "system", Content: "Summarise this conversation history concisely in one paragraph."},
			{Role: "user", Content: history},
		}
		summary, err := streamText(ctx, provider, msgs, handle)
		if err != nil {
			handle(protocol.Response{Type: "error", Content: err.Error()})
			return
		}
		sess.Messages = session.ParseHistoryMessages(history)
		sess.SetSummary(summary)
		if err := sess.SaveCache(); err != nil {
			log.Printf("session save error: %v", err)
		}

	default:
		handle(protocol.Response{Type: "error", Content: "unknown session command: " + req.Session.Command})
	}
}

// streamText streams a completion to handle and returns the full text.
func streamText(ctx context.Context, provider ai.Provider, msgs []ai.Message, handle Handler) (string, error) {
	textCh, err := provider.Complete(ctx, msgs)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for chunk := range textCh {
		sb.WriteString(chunk)
		handle(protocol.Response{Type: "text", Content: chunk})
	}
	return sb.String(), nil
}
