package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"spaios/internal/permissions"
	"spaios/internal/protocol"
)

// recommendedModels is the curated list shown to new users by `spai llm list`.
var recommendedModels = []struct {
	Name        string
	Runtime     string
	Description string
}{
	{"qwen2.5-coder:7b", "ollama", "Best for coding tasks, fits in 8 GB RAM"},
	{"llama3.2:3b", "ollama", "Fast general assistant, fits in 4 GB RAM"},
	{"phi4-mini", "ollama", "Compact model, great on low-end hardware"},
	{"mistral:7b", "ollama", "Strong general-purpose model, 8 GB RAM"},
	{"BitNet-b1.58-2B-4T", "bitnet", "Microsoft 1-bit model, 2B params, extreme CPU efficiency"},
	{"BitNet-b1.58-3B", "bitnet", "Microsoft 1-bit model, 3B params, stronger reasoning"},
	{"Llama3-8B-1.58-100B-tokens", "bitnet", "Llama 3 8B in 1-bit, best BitNet quality"},
}

// Manager orchestrates LLM runtime and model management for spaid.
type Manager struct {
	state       *State
	client      *http.Client
	ollamaURL   string               // overridable for tests; defaults to registry endpoint
	stepCheckFn func(string) bool    // nil = run real shell check; overridable for tests
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

// WithStepChecker returns a copy of the Manager with a custom step-check function.
// Used in tests to simulate partial or complete installs without real disk state.
func (m *Manager) WithStepChecker(fn func(cmd string) bool) *Manager {
	copy := *m
	copy.stepCheckFn = fn
	return &copy
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
			m.handleInstall(ch, req.Args)
		case "list":
			m.handleList(ch)
		case "pull":
			m.handlePull(ch, req.Args)
		case "use":
			m.handleUse(ch, req.Args)
		case "remove":
			m.handleRemove(ch, req.Args)
		case "uninstall":
			m.handleUninstall(ch, req.Args)
		case "use-runtime":
			m.handleUseRuntime(ch, req.Args)
		default:
			ch <- protocol.Response{Type: "error", Content: fmt.Sprintf("unknown llm command %q — try: status, install, uninstall, list, pull, use", req.Command)}
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
	activeRuntime := m.state.ActiveRuntime
	if activeRuntime == "" {
		activeRuntime = "ollama"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Runtime:       %s\n", activeRuntime))
	sb.WriteString(fmt.Sprintf("Active model:  %s\n", m.state.ActiveModel))

	switch activeRuntime {
	case "bitnet":
		rt, _ := Get("bitnet")
		sb.WriteString(fmt.Sprintf("Endpoint:      %s\n", rt.Endpoint))
		sb.WriteString("\nBitNet runs inference on-demand (no persistent daemon).\n")
		sb.WriteString(fmt.Sprintf("To start server: %s\n", rt.StartCmd))
	default: // ollama
		running := m.isOllamaRunning()
		status := "stopped"
		if running {
			status = "running"
		}
		sb.WriteString(fmt.Sprintf("Endpoint:      %s\n", m.ollamaURL))
		sb.WriteString(fmt.Sprintf("Status:        %s\n", status))
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
	}

	ch <- protocol.Response{Type: "text", Content: sb.String()}
	ch <- protocol.Response{Type: "done"}
}

func (m *Manager) handleInstall(ch chan<- protocol.Response, args []string) {
	runtimeName := m.state.ActiveRuntime
	if len(args) > 0 {
		runtimeName = args[0]
	}
	if runtimeName == "" {
		runtimeName = "ollama"
	}

	rt, err := Get(runtimeName)
	if err != nil {
		ch <- protocol.Response{Type: "error", Content: err.Error()}
		ch <- protocol.Response{Type: "done"}
		return
	}

	// Determine which install steps are still pending by running per-step checks.
	pendingCmds, doneCount := m.pendingInstallSteps(rt)

	if len(pendingCmds) == 0 {
		msg := fmt.Sprintf("%s is already installed.\n", rt.Name)
		if runtimeName == "ollama" && m.isOllamaRunning() {
			msg = "Ollama is already installed and running.\n"
		}
		msg += "Run 'spai llm list' to see available models.\n"
		ch <- protocol.Response{Type: "text", Content: msg}
		ch <- protocol.Response{Type: "done"}
		return
	}

	if doneCount > 0 {
		total := len(rt.InstallCmds)
		ch <- protocol.Response{Type: "text", Content: fmt.Sprintf(
			"Resuming %s install — %d/%d steps already done, running remaining %d:\n\n",
			rt.Name, doneCount, total, len(pendingCmds),
		)}
	} else {
		ch <- protocol.Response{Type: "text", Content: fmt.Sprintf("The following commands will install %s:\n\n", rt.Name)}
	}

	plan := make([]protocol.CommandItem, len(pendingCmds))
	for i, cmd := range pendingCmds {
		plan[i] = protocol.CommandItem{
			Command: cmd,
			Tier:    rt.InstallTier.String(),
			Display: rt.InstallTier.Display(),
		}
	}
	ch <- protocol.Response{Type: "plan", Plan: plan}

	if runtimeName != m.state.ActiveRuntime {
		ch <- protocol.Response{Type: "text", Content: fmt.Sprintf("\nAfter install, activate with: spai llm use-runtime %s\n", runtimeName)}
	}
	ch <- protocol.Response{Type: "done"}
}

// pendingInstallSteps returns the subset of rt.InstallCmds whose step-check
// indicates the step has not yet completed, plus the count of already-done steps.
func (m *Manager) pendingInstallSteps(rt Runtime) (pending []string, doneCount int) {
	checkDone := m.stepCheckFn
	if checkDone == nil {
		checkDone = func(cmd string) bool {
			return exec.Command("sh", "-c", cmd).Run() == nil
		}
	}
	for i, cmd := range rt.InstallCmds {
		if i < len(rt.InstallStepChecks) && rt.InstallStepChecks[i] != "" {
			if checkDone(rt.InstallStepChecks[i]) {
				doneCount++
				continue
			}
		}
		pending = append(pending, cmd)
	}
	return
}

func (m *Manager) handleUninstall(ch chan<- protocol.Response, args []string) {
	runtimeName := m.state.ActiveRuntime
	if len(args) > 0 {
		runtimeName = args[0]
	}
	if runtimeName == "" {
		runtimeName = "ollama"
	}

	rt, err := Get(runtimeName)
	if err != nil {
		ch <- protocol.Response{Type: "error", Content: err.Error()}
		ch <- protocol.Response{Type: "done"}
		return
	}

	if len(rt.UninstallCmds) == 0 {
		ch <- protocol.Response{Type: "error", Content: fmt.Sprintf("automatic uninstall is not supported for runtime %q on this platform", runtimeName)}
		ch <- protocol.Response{Type: "done"}
		return
	}

	ch <- protocol.Response{Type: "text", Content: fmt.Sprintf("The following commands will uninstall %s:\n\n", rt.Name)}
	plan := make([]protocol.CommandItem, len(rt.UninstallCmds))
	for i, cmd := range rt.UninstallCmds {
		tier := permissions.Classify(cmd)
		plan[i] = protocol.CommandItem{
			Command: cmd,
			Tier:    tier.String(),
			Display: tier.Display(),
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

	activeRuntime := m.state.ActiveRuntime
	if activeRuntime == "" {
		activeRuntime = "ollama"
	}
	sb.WriteString(fmt.Sprintf("Recommended models for %s (run 'spai llm pull <name>' to install):\n", activeRuntime))
	for _, r := range recommendedModels {
		if r.Runtime == activeRuntime {
			sb.WriteString(fmt.Sprintf("  %-40s %s\n", r.Name, r.Description))
		}
	}
	sb.WriteString("\nTo switch runtimes: spai llm install <runtime>  (ollama, bitnet)\n")

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

	activeRuntime := m.state.ActiveRuntime
	if activeRuntime == "" {
		activeRuntime = "ollama"
	}

	var cmd, msg string
	switch activeRuntime {
	case "bitnet":
		dir := "~/.local/share/spaios/bitnet"
		venv := "~/.local/share/spaios/bitnet-venv"
		cmd = fmt.Sprintf("cd %s && %s/bin/python setup_env.py -md models/%s -q i2_s", dir, venv, model)
		msg = fmt.Sprintf("Downloading and quantizing BitNet model %q...\n", model)
	default:
		cmd = fmt.Sprintf("ollama pull %s", model)
		msg = fmt.Sprintf("Downloading model %q from Ollama registry...\n", model)
	}

	tier := permissions.Classify(cmd)
	ch <- protocol.Response{Type: "text", Content: msg}
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

func (m *Manager) handleRemove(ch chan<- protocol.Response, args []string) {
	if len(args) == 0 {
		ch <- protocol.Response{Type: "error", Content: "usage: spai llm remove <model>\nexample: spai llm remove llama3.2:3b"}
		ch <- protocol.Response{Type: "done"}
		return
	}
	model := args[0]

	activeRuntime := m.state.ActiveRuntime
	if activeRuntime == "" {
		activeRuntime = "ollama"
	}

	var cmd string
	switch activeRuntime {
	case "bitnet":
		cmd = fmt.Sprintf("rm -rf ~/.local/share/spaios/bitnet/models/%s", model)
	default:
		cmd = fmt.Sprintf("ollama rm %s", model)
	}

	tier := permissions.Classify(cmd)
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

func (m *Manager) handleUseRuntime(ch chan<- protocol.Response, args []string) {
	if len(args) == 0 {
		ch <- protocol.Response{Type: "error", Content: "usage: spai llm use-runtime <runtime>\nexample: spai llm use-runtime bitnet"}
		ch <- protocol.Response{Type: "done"}
		return
	}
	name := args[0]
	if _, err := Get(name); err != nil {
		ch <- protocol.Response{Type: "error", Content: err.Error()}
		ch <- protocol.Response{Type: "done"}
		return
	}
	m.state.mu.Lock()
	m.state.ActiveRuntime = name
	m.state.mu.Unlock()
	if err := m.state.Save(); err != nil {
		ch <- protocol.Response{Type: "error", Content: fmt.Sprintf("failed to save state: %v", err)}
		ch <- protocol.Response{Type: "done"}
		return
	}
	ch <- protocol.Response{Type: "text", Content: fmt.Sprintf("Active runtime set to: %s\nRestart spaid for the change to take effect: systemctl --user restart spaid\n", name)}
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
