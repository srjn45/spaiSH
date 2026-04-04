package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"spaios/internal/permissions"
	"spaios/internal/protocol"
)

// recommendedModels is the curated list shown to new users by `spai llm list`.
var recommendedModels = []struct {
	Name        string
	Description string
}{
	{"qwen2.5-coder:7b", "Best for coding tasks, fits in 8 GB RAM"},
	{"llama3.2:3b", "Fast general assistant, fits in 4 GB RAM"},
	{"phi4-mini", "Compact model, great on low-end hardware"},
	{"mistral:7b", "Strong general-purpose model, 8 GB RAM"},
}

// Manager orchestrates LLM runtime and model management for spaid.
type Manager struct {
	state     *State
	client    *http.Client
	ollamaURL string // overridable for tests; defaults to registry endpoint
}

// NewManager creates a Manager using the Ollama endpoint from the registry.
func NewManager(state *State) *Manager {
	rt, _ := Get("ollama")
	return &Manager{
		state:     state,
		client:    &http.Client{Timeout: 5 * time.Second},
		ollamaURL: rt.Endpoint,
	}
}

// NewManagerWithClient creates a Manager with an injected HTTP client and base URL.
// Used in tests to point at an httptest server.
func NewManagerWithClient(state *State, client *http.Client, ollamaURL string) *Manager {
	return &Manager{state: state, client: client, ollamaURL: ollamaURL}
}

// Handle dispatches an LLMRequest and returns a channel of Response chunks.
// The channel is always closed with a final "done" response.
func (m *Manager) Handle(req *protocol.LLMRequest) <-chan protocol.Response {
	ch := make(chan protocol.Response, 8)
	go func() {
		defer close(ch)
		switch req.Command {
		case "status":
			m.handleStatus(ch)
		case "install":
			m.handleInstall(ch)
		case "list":
			m.handleList(ch)
		case "pull":
			m.handlePull(ch, req.Args)
		case "use":
			m.handleUse(ch, req.Args)
		default:
			ch <- protocol.Response{Type: "error", Content: fmt.Sprintf("unknown llm command %q — try: status, install, list, pull, use", req.Command)}
			ch <- protocol.Response{Type: "done"}
		}
	}()
	return ch
}

// isOllamaRunning returns true if the Ollama HTTP API responds at ollamaURL.
func (m *Manager) isOllamaRunning() bool {
	resp, err := m.client.Get(m.ollamaURL + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ollamaModels queries the Ollama API and returns the names of installed models.
func (m *Manager) ollamaModels() ([]string, error) {
	resp, err := m.client.Get(m.ollamaURL + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	names := make([]string, len(body.Models))
	for i, mdl := range body.Models {
		names[i] = mdl.Name
	}
	return names, nil
}

func (m *Manager) handleStatus(ch chan<- protocol.Response) {
	running := m.isOllamaRunning()
	status := "stopped"
	if running {
		status = "running"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Runtime:       ollama (%s)\n", status))
	sb.WriteString(fmt.Sprintf("Endpoint:      %s\n", m.ollamaURL))
	sb.WriteString(fmt.Sprintf("Active model:  %s\n", m.state.ActiveModel))

	if running {
		models, err := m.ollamaModels()
		if err == nil && len(models) > 0 {
			sb.WriteString(fmt.Sprintf("Installed models (%d):\n", len(models)))
			for _, model := range models {
				marker := "  "
				if model == m.state.ActiveModel || strings.HasPrefix(model, m.state.ActiveModel+":") {
					marker = "* "
				}
				sb.WriteString(fmt.Sprintf("  %s%s\n", marker, model))
			}
		} else if err == nil {
			sb.WriteString("Installed models: none — run 'spai llm pull <model>' to download one\n")
		}
	} else {
		sb.WriteString("\nOllama is not running. To start it: ollama serve\n")
		sb.WriteString("To install Ollama:                  spai llm install\n")
	}

	ch <- protocol.Response{Type: "text", Content: sb.String()}
	ch <- protocol.Response{Type: "done"}
}

func (m *Manager) handleInstall(ch chan<- protocol.Response) {
	if m.isOllamaRunning() {
		ch <- protocol.Response{Type: "text", Content: "Ollama is already installed and running.\nRun 'spai llm list' to see available models.\n"}
		ch <- protocol.Response{Type: "done"}
		return
	}

	rt, err := Get("ollama")
	if err != nil {
		ch <- protocol.Response{Type: "error", Content: err.Error()}
		ch <- protocol.Response{Type: "done"}
		return
	}

	ch <- protocol.Response{Type: "text", Content: "The following command will install Ollama:\n\n"}

	plan := make([]protocol.CommandItem, len(rt.InstallCmds))
	for i, cmd := range rt.InstallCmds {
		plan[i] = protocol.CommandItem{
			Command: cmd,
			Tier:    rt.InstallTier.String(),
			Display: rt.InstallTier.Display(),
		}
	}
	ch <- protocol.Response{Type: "plan", Plan: plan}
	ch <- protocol.Response{Type: "done"}
}

func (m *Manager) handleList(ch chan<- protocol.Response) {
	var sb strings.Builder

	if m.isOllamaRunning() {
		models, err := m.ollamaModels()
		if err == nil && len(models) > 0 {
			sb.WriteString("Installed models:\n")
			for _, model := range models {
				marker := "  "
				if model == m.state.ActiveModel || strings.HasPrefix(model, m.state.ActiveModel+":") {
					marker = "* "
				}
				sb.WriteString(fmt.Sprintf("  %s%s\n", marker, model))
			}
			sb.WriteString("\n")
		} else if err == nil {
			sb.WriteString("No models installed yet.\n\n")
		}
	}

	sb.WriteString("Recommended models (run 'spai llm pull <name>' to install):\n")
	for _, r := range recommendedModels {
		sb.WriteString(fmt.Sprintf("  %-30s %s\n", r.Name, r.Description))
	}

	ch <- protocol.Response{Type: "text", Content: sb.String()}
	ch <- protocol.Response{Type: "done"}
}

func (m *Manager) handlePull(ch chan<- protocol.Response, args []string) {
	if len(args) == 0 {
		ch <- protocol.Response{Type: "error", Content: "usage: spai llm pull <model>\nexample: spai llm pull qwen2.5-coder:7b"}
		ch <- protocol.Response{Type: "done"}
		return
	}
	model := args[0]
	cmd := fmt.Sprintf("ollama pull %s", model)
	tier := permissions.Classify(cmd)
	ch <- protocol.Response{Type: "text", Content: fmt.Sprintf("Downloading model %q from Ollama registry...\n", model)}
	ch <- protocol.Response{
		Type: "plan",
		Plan: []protocol.CommandItem{{
			Command: cmd,
			Tier:    tier.String(),
			Display: tier.Display(),
		}},
	}
	ch <- protocol.Response{Type: "done"}
}

func (m *Manager) handleUse(ch chan<- protocol.Response, args []string) {
	if len(args) == 0 {
		ch <- protocol.Response{Type: "error", Content: "usage: spai llm use <model>\nexample: spai llm use llama3.2:3b"}
		ch <- protocol.Response{Type: "done"}
		return
	}
	model := args[0]
	m.state.SetActiveModel(model)
	if err := m.state.Save(); err != nil {
		ch <- protocol.Response{Type: "error", Content: fmt.Sprintf("failed to save state: %v", err)}
		ch <- protocol.Response{Type: "done"}
		return
	}
	ch <- protocol.Response{Type: "text", Content: fmt.Sprintf("Active model set to: %s\nRestart spaid for the change to take effect: systemctl --user restart spaid\n", model)}
	ch <- protocol.Response{Type: "done"}
}
