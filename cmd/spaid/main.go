package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"spaios/internal/agent"
	"spaios/internal/ai"
	"spaios/internal/config"
	"spaios/internal/executor"
	"spaios/internal/llm"
	"spaios/internal/protocol"
	"spaios/internal/router"
	"spaios/internal/session"
	"spaios/internal/socket"
)

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaios", "spaid.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaios", "spaid.toml")
}

func sockPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaios", "spaid.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaios", "spaid.sock")
}

// loadSession returns the session for the given ID, falling back to "default".
func loadSession(id string) *session.Session {
	if id == "" {
		id = "default"
	}
	sess, err := session.LoadByID(id)
	if err != nil {
		log.Printf("session load warning (id=%s): %v — starting fresh", id, err)
		sess, _ = session.LoadByID(id)
	}
	return sess
}

func main() {
	logPath := filepath.Join(filepath.Dir(sockPath()), "spaid.log")
	os.MkdirAll(filepath.Dir(logPath), 0700)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	cfg, err := config.Load(configPath())
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	llmState, err := llm.LoadState(llm.DefaultStatePath())
	if err != nil {
		log.Printf("llm state load warning: %v — using defaults", err)
		llmState, _ = llm.LoadState(llm.DefaultStatePath())
	}
	llmMgr := llm.NewManager(llmState)

	// Prefer the active model from llm-state over the config value.
	// This lets `spai llm use <model>` take effect after a daemon restart.
	localModel := cfg.Local.LocalModel
	if llmState.ActiveModel != "" {
		localModel = llmState.ActiveModel
	}

	cloud := ai.NewCloudProvider(cfg.Provider.Endpoint, cfg.APIKey(), cfg.Provider.Model)
	local := ai.NewLocalProvider(cfg.Local.OllamaEndpoint, localModel)
	rtr := router.New(cfg, cloud, local)

	sock := sockPath()
	log.Printf("spaid starting, socket: %s", sock)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		log.Println("spaid shutting down")
		os.Remove(sock)
		os.Exit(0)
	}()

	onQuery := func(req *protocol.Request, enc *json.Encoder) {
		sess := loadSession(req.SessionID)
		respCh, err := rtr.Route(context.Background(), req, sess)
		if err != nil {
			enc.Encode(protocol.Response{Type: "error", Content: err.Error()})
			return
		}
		var fullText strings.Builder
		for resp := range respCh {
			enc.Encode(resp)
			if resp.Type == "text" {
				fullText.WriteString(resp.Content)
			}
		}
		sess.AddExchange(req.Query, fullText.String())
		sess.Save()
	}

	onExec := func(req *protocol.Request, enc *json.Encoder) {
		for _, cmd := range req.Commands {
			enc.Encode(protocol.Response{Type: "output", Content: fmt.Sprintf("$ %s\n", cmd)})
			var out strings.Builder
			if err := executor.Execute(cmd, &out); err != nil {
				enc.Encode(protocol.Response{Type: "output", Content: out.String()})
				enc.Encode(protocol.Response{Type: "error", Content: fmt.Sprintf("command failed: %v", err)})
				return
			}
			enc.Encode(protocol.Response{Type: "output", Content: out.String()})
		}
		enc.Encode(protocol.Response{Type: "done"})
	}

	onLLM := func(req *protocol.Request, enc *json.Encoder) {
		if req.LLM == nil {
			enc.Encode(protocol.Response{Type: "error", Content: "missing llm payload"})
			enc.Encode(protocol.Response{Type: "done"})
			return
		}
		for resp := range llmMgr.Handle(req.LLM) {
			enc.Encode(resp)
		}
	}

	onAgent := func(req *protocol.Request, enc *json.Encoder, dec *json.Decoder) {
		if req.Agent == nil {
			enc.Encode(protocol.Response{Type: "error", Content: "missing agent payload"})
			enc.Encode(protocol.Response{Type: "done"})
			return
		}

		sess := loadSession(req.SessionID)
		provider, err := rtr.SelectProvider(req.ForceLocal)
		if err != nil {
			enc.Encode(protocol.Response{Type: "error", Content: err.Error()})
			enc.Encode(protocol.Response{Type: "done"})
			return
		}

		agentCfg := agent.Config{
			Autonomous:    cfg.Agent.Autonomous || req.Agent.Autonomous,
			MaxIterations: cfg.Agent.MaxIterations,
			Verbose:       cfg.Agent.Verbose || req.Agent.Verbose,
			WorkingDir:    req.WorkingDir,
			GitBranch:     req.GitBranch,
			Stdin:         req.Stdin,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		confirmFn := func(confirmReq protocol.ConfirmRequest) bool {
			data, err := json.Marshal(confirmReq)
			if err != nil {
				return false
			}
			enc.Encode(protocol.Response{Type: "confirm_request", Content: string(data)})
			var reply protocol.Request
			if err := dec.Decode(&reply); err != nil || reply.ConfirmResponse == nil {
				return false
			}
			return reply.ConfirmResponse.Approved
		}

		a := agent.New(provider, agentCfg, confirmFn)

		var fullText strings.Builder
		for resp := range a.Run(ctx, req.Agent, sess) {
			enc.Encode(resp)
			if resp.Type == "text" {
				fullText.WriteString(resp.Content)
			}
		}
		sess.AddExchange(req.Agent.Query, fullText.String())
		sess.Save()
	}

	onSession := func(req *protocol.Request, enc *json.Encoder) {
		if req.Session == nil {
			enc.Encode(protocol.Response{Type: "error", Content: "missing session payload"})
			enc.Encode(protocol.Response{Type: "done"})
			return
		}

		sess := loadSession(req.SessionID)

		switch req.Session.Command {
		case "clear":
			if req.Session.Lines == 0 {
				sess.Clear()
				enc.Encode(protocol.Response{Type: "text", Content: "Session cleared.\n"})
			} else {
				sess.Trim(req.Session.Lines)
				enc.Encode(protocol.Response{Type: "text", Content: fmt.Sprintf("Session trimmed to %d messages.\n", req.Session.Lines)})
			}
			if err := sess.Save(); err != nil {
				log.Printf("session save error: %v", err)
			}
			enc.Encode(protocol.Response{Type: "done"})

		case "compact":
			if len(sess.Messages) == 0 {
				enc.Encode(protocol.Response{Type: "text", Content: "Nothing to compact — session is empty.\n"})
				enc.Encode(protocol.Response{Type: "done"})
				return
			}

			provider, err := rtr.SelectProvider(req.ForceLocal)
			if err != nil {
				enc.Encode(protocol.Response{Type: "error", Content: err.Error()})
				enc.Encode(protocol.Response{Type: "done"})
				return
			}

			compactMsgs := []ai.Message{
				{Role: "system", Content: "Summarise the following conversation concisely. Focus on what was worked on and what was achieved. One short paragraph."},
			}
			compactMsgs = append(compactMsgs, sess.Messages...)

			textCh, err := provider.Complete(context.Background(), compactMsgs)
			if err != nil {
				enc.Encode(protocol.Response{Type: "error", Content: err.Error()})
				enc.Encode(protocol.Response{Type: "done"})
				return
			}

			var summary strings.Builder
			for chunk := range textCh {
				summary.WriteString(chunk)
				enc.Encode(protocol.Response{Type: "text", Content: chunk})
			}

			sess.Compact(summary.String())
			if err := sess.Save(); err != nil {
				log.Printf("session save error: %v", err)
			}
			enc.Encode(protocol.Response{Type: "done"})

		default:
			enc.Encode(protocol.Response{Type: "error", Content: "unknown session command: " + req.Session.Command})
			enc.Encode(protocol.Response{Type: "done"})
		}
	}

	if err := socket.Serve(sock, onQuery, onExec, onLLM, onAgent, onSession); err != nil {
		log.Fatalf("socket error: %v", err)
	}
}
