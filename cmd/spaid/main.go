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

	sess, err := session.LoadFrom(session.DefaultPath())
	if err != nil {
		log.Printf("session load warning: %v — starting fresh", err)
		sess, _ = session.LoadFrom(session.DefaultPath())
	}

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

		forceLocal := req.ForceLocal || cfg.Agent.Autonomous || req.Agent.Autonomous
		provider, err := rtr.SelectProvider(forceLocal)
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
		}
		if agentCfg.MaxIterations <= 0 {
			agentCfg.MaxIterations = 5
		}

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
		for resp := range a.Run(context.Background(), req.Agent, sess) {
			enc.Encode(resp)
			if resp.Type == "text" {
				fullText.WriteString(resp.Content)
			}
		}
		sess.AddExchange(req.Agent.Query, fullText.String())
		sess.Save()
	}

	if err := socket.Serve(sock, onQuery, onExec, onLLM, onAgent); err != nil {
		log.Fatalf("socket error: %v", err)
	}
}
