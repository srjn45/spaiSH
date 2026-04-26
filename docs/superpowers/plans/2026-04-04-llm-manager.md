# LLM Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `spai llm status|install|list|pull|use` subcommands so novice users can install Ollama and manage local models without leaving the terminal.

**Architecture:** A new `internal/llm` package (registry + state + manager) is wired into `spaid` as a third socket handler alongside the existing query and execute handlers. The CLI parses `spai llm <cmd>` before the existing flag path and sends an `"llm"` typed request over the socket. Every shell command the manager proposes (Ollama install, model pull) goes through the existing permission tier engine and confirmation flow — no special-casing.

**Tech Stack:** Go 1.22+, existing `spaish` module, `net/http/httptest` for manager tests. No new dependencies.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/protocol/protocol.go` | Modify | Add `LLMRequest` struct; add `LLM` field to `Request` |
| `internal/llm/state.go` | Create | Read/write `~/.config/spaish/llm-state.json`; active runtime + model |
| `internal/llm/state_test.go` | Create | State persistence tests |
| `internal/llm/registry.go` | Create | Ollama runtime definition, platform-aware install commands |
| `internal/llm/registry_test.go` | Create | Registry unit tests |
| `internal/llm/manager.go` | Create | Orchestrate status/install/list/pull/use — returns `<-chan protocol.Response` |
| `internal/llm/manager_test.go` | Create | Manager tests using `httptest` to mock Ollama API |
| `internal/socket/server.go` | Modify | Add `LLMHandler` type; add `"llm"` case to `handleConn`; extend `Serve` signature |
| `cmd/spaid/main.go` | Modify | Load LLM state; create manager; pass `onLLM` to `socket.Serve` |
| `cmd/spai/main.go` | Modify | Parse `spai llm <cmd> [args]` before flag parsing; send `"llm"` request |

---

## Story Map

| Story | Phase | Tasks | Checkpoint |
|-------|-------|-------|-----------|
| **S1: Protocol** | A (solo) | 1 | `go build ./internal/protocol/...` |
| **S2: State** | B (parallel) | 2 | `go test ./internal/llm/... -run State` |
| **S3: Registry** | B (parallel) | 3 | `go test ./internal/llm/... -run Registry` |
| **S4: Manager** | C (solo) | 4 | `go test ./internal/llm/...` |
| **S5: Socket + Daemon** | D (parallel) | 5 | `go build ./...` |
| **S6: CLI** | D (parallel) | 6 | `go build ./cmd/spai/ && spai llm status` works end-to-end |

**Resume rule:** check which tasks have all steps checked. Start from the first with an unchecked step.

---

## Task 1: Protocol Extension

**Files:**
- Modify: `internal/protocol/protocol.go`

- [ ] **Step 1: Add `LLMRequest` and extend `Request`**

Edit `internal/protocol/protocol.go`. Add after the existing `CommandItem` struct:

```go
// LLMRequest is the payload for "llm" request type.
// Used by the spai CLI to send LLM management commands to spaid.
type LLMRequest struct {
	Command string   `json:"command"` // "status" | "install" | "list" | "pull" | "use"
	Args    []string `json:"args,omitempty"`
}
```

And add one field to `Request` (after `Commands`):

```go
	LLM *LLMRequest `json:"llm,omitempty"` // for "llm" request type
```

The complete updated `Request` struct becomes:

```go
type Request struct {
	Type       string      `json:"type"`
	Query      string      `json:"query,omitempty"`
	WorkingDir string      `json:"working_dir"`
	GitBranch  string      `json:"git_branch,omitempty"`
	ForceLocal bool        `json:"force_local"`
	DryRun     bool        `json:"dry_run"`
	Commands   []string    `json:"commands,omitempty"`
	LLM        *LLMRequest `json:"llm,omitempty"`
}
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./internal/protocol/...
```

Expected: exit 0, no output.

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/protocol.go
git commit -m "feat(protocol): add LLMRequest type and llm field to Request"
```

---

## Task 2: LLM State

**Files:**
- Create: `internal/llm/state_test.go`
- Create: `internal/llm/state.go`

- [ ] **Step 1: Write failing tests**

Create `internal/llm/state_test.go`:

```go
package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaish/internal/llm"
)

func TestLoadStateEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")
	s, err := llm.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if s.ActiveRuntime != "ollama" {
		t.Errorf("expected default active_runtime 'ollama', got %q", s.ActiveRuntime)
	}
	if s.ActiveModel == "" {
		t.Error("expected non-empty default active_model")
	}
}

func TestLoadStatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")

	s, _ := llm.LoadState(path)
	s.SetRuntime("ollama", "0.6.1", "http://localhost:11434")
	s.SetActiveModel("llama3.2:3b")
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	s2, err := llm.LoadState(path)
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if s2.ActiveModel != "llama3.2:3b" {
		t.Errorf("got active model %q, want %q", s2.ActiveModel, "llama3.2:3b")
	}
	rt, ok := s2.Runtimes["ollama"]
	if !ok {
		t.Fatal("expected ollama runtime in state")
	}
	if rt.Version != "0.6.1" {
		t.Errorf("got version %q, want %q", rt.Version, "0.6.1")
	}
}

func TestLoadStateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")
	s, _ := llm.LoadState(path)
	s.SetActiveModel("test-model")
	s.Save()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected mode 0600, got %v", info.Mode().Perm())
	}
}

func TestLoadStateCorrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-state.json")
	os.WriteFile(path, []byte("not valid json {{{{"), 0600)

	s, err := llm.LoadState(path)
	if err != nil {
		t.Fatalf("expected no error for corrupted file, got: %v", err)
	}
	// Should return a fresh default state
	if s.ActiveRuntime != "ollama" {
		t.Errorf("expected default state after corruption")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/llm/... -run State
```

Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Implement state.go**

Create `internal/llm/state.go`:

```go
package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultModel = "qwen2.5-coder:7b"

// RuntimeInfo records what spaiSH knows about an installed runtime.
type RuntimeInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Endpoint  string `json:"endpoint"`
}

// State holds LLM manager state, persisted to ~/.config/spaish/llm-state.json.
type State struct {
	ActiveRuntime string                 `json:"active_runtime"`
	ActiveModel   string                 `json:"active_model"`
	Runtimes      map[string]RuntimeInfo `json:"runtimes"`
	LastUpdated   time.Time              `json:"last_updated"`

	path string
	mu   sync.Mutex
}

// DefaultStatePath returns the canonical state file location.
func DefaultStatePath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaish", "llm-state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaish", "llm-state.json")
}

// LoadState reads state from path. Returns a default state if the file does not exist.
func LoadState(path string) (*State, error) {
	s := &State{
		path:          path,
		ActiveRuntime: "ollama",
		ActiveModel:   defaultModel,
		Runtimes:      make(map[string]RuntimeInfo),
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		// Corrupted — return a fresh default state without error
		s.Runtimes = make(map[string]RuntimeInfo)
		return s, nil
	}
	if s.Runtimes == nil {
		s.Runtimes = make(map[string]RuntimeInfo)
	}
	return s, nil
}

// Save writes state to disk with mode 0600.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdated = time.Now()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// SetRuntime records a runtime as installed.
func (s *State) SetRuntime(name, version, endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Runtimes[name] = RuntimeInfo{Installed: true, Version: version, Endpoint: endpoint}
	s.ActiveRuntime = name
}

// SetActiveModel sets the active model name.
func (s *State) SetActiveModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ActiveModel = model
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/llm/... -run State -v
```

Expected: PASS — TestLoadStateEmpty, TestLoadStatePersists, TestLoadStateFilePermissions, TestLoadStateCorrupted.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/
git commit -m "feat(llm): add state persistence for active runtime and model"
```

---

## Task 3: Runtime Registry

**Files:**
- Create: `internal/llm/registry_test.go`
- Create: `internal/llm/registry.go`

- [ ] **Step 1: Write failing tests**

Create `internal/llm/registry_test.go`:

```go
package llm_test

import (
	"testing"

	"spaish/internal/llm"
)

func TestRegistryGetOllama(t *testing.T) {
	rt, err := llm.Get("ollama")
	if err != nil {
		t.Fatalf("Get(\"ollama\") error: %v", err)
	}
	if rt.Name != "ollama" {
		t.Errorf("got name %q, want %q", rt.Name, "ollama")
	}
	if rt.Endpoint == "" {
		t.Error("expected non-empty endpoint")
	}
	if rt.DetectCmd == "" {
		t.Error("expected non-empty DetectCmd")
	}
	if len(rt.InstallCmds) == 0 {
		t.Error("expected at least one install command")
	}
	for _, cmd := range rt.InstallCmds {
		if cmd == "" {
			t.Error("install command must not be empty")
		}
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	_, err := llm.Get("nonexistent")
	if err == nil {
		t.Error("expected error for unknown runtime")
	}
}

func TestRegistryList(t *testing.T) {
	names := llm.List()
	if len(names) == 0 {
		t.Error("expected at least one runtime in list")
	}
	found := false
	for _, n := range names {
		if n == "ollama" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'ollama' in runtime list")
	}
}

func TestRegistryInstallTierNotEmpty(t *testing.T) {
	rt, _ := llm.Get("ollama")
	if rt.InstallTier == 0 {
		t.Error("expected non-zero InstallTier for ollama")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/llm/... -run Registry
```

Expected: FAIL — `Get`, `List`, `Runtime` not defined.

- [ ] **Step 3: Implement registry.go**

Create `internal/llm/registry.go`:

```go
package llm

import (
	"fmt"
	"runtime"

	"spaish/internal/permissions"
)

// Runtime describes a supported local LLM runtime.
type Runtime struct {
	Name        string
	Description string
	Endpoint    string
	DetectCmd   string           // check if runtime is on PATH
	VersionCmd  string           // get installed version
	StartCmd    string           // start the runtime service
	InstallCmds []string         // ordered shell commands to install
	InstallTier permissions.Tier // permission tier for install commands
}

// SupportedRuntimes is the registry of runtimes spaiSH can manage.
// Add new runtimes here as they are supported.
var SupportedRuntimes = map[string]Runtime{
	"ollama": {
		Name:        "ollama",
		Description: "Popular local LLM runtime — single binary, easy install, wide model support",
		Endpoint:    "http://localhost:11434",
		DetectCmd:   "which ollama",
		VersionCmd:  "ollama --version",
		StartCmd:    "ollama serve",
		InstallCmds: ollamaInstallCmds(),
		InstallTier: permissions.TierElevated,
	},
}

// ollamaInstallCmds returns the platform-appropriate install commands for Ollama.
func ollamaInstallCmds() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"curl -fsSL https://ollama.com/install.sh | sh"}
	case "darwin":
		return []string{"brew install ollama"}
	default:
		return []string{fmt.Sprintf(
			"echo 'Automatic install not supported on %s. Visit https://ollama.com to install.'",
			runtime.GOOS,
		)}
	}
}

// Get returns the Runtime with the given name, or an error if unknown.
func Get(name string) (Runtime, error) {
	r, ok := SupportedRuntimes[name]
	if !ok {
		return Runtime{}, fmt.Errorf("unknown runtime %q — supported: ollama", name)
	}
	return r, nil
}

// List returns all supported runtime names.
func List() []string {
	names := make([]string, 0, len(SupportedRuntimes))
	for k := range SupportedRuntimes {
		names = append(names, k)
	}
	return names
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/llm/... -run Registry -v
```

Expected: PASS — all 4 registry tests.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/registry.go internal/llm/registry_test.go
git commit -m "feat(llm): add runtime registry with Ollama definition"
```

---

## Task 4: LLM Manager

**Files:**
- Create: `internal/llm/manager_test.go`
- Create: `internal/llm/manager.go`

- [ ] **Step 1: Write failing tests**

Create `internal/llm/manager_test.go`:

```go
package llm_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"spaish/internal/llm"
	"spaish/internal/protocol"
)

// newTestState returns an in-memory state pointing to a temp file.
func newTestState(t *testing.T) *llm.State {
	t.Helper()
	s, _ := llm.LoadState(filepath.Join(t.TempDir(), "llm-state.json"))
	return s
}

// collectResponses drains the response channel and returns all items.
func collectResponses(ch <-chan protocol.Response) []protocol.Response {
	var out []protocol.Response
	for r := range ch {
		out = append(out, r)
	}
	return out
}

func TestManagerStatusOllamaRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"models":[{"name":"qwen2.5-coder:7b"},{"name":"llama3.2:3b"}]}`)
		}
	}))
	defer srv.Close()

	state := newTestState(t)
	state.SetRuntime("ollama", "0.6.1", srv.URL)
	state.SetActiveModel("qwen2.5-coder:7b")

	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL)
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "status"}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected last response type 'done', got %q", last.Type)
	}

	var fullText string
	for _, r := range resps {
		if r.Type == "text" {
			fullText += r.Content
		}
	}
	if fullText == "" {
		t.Error("expected non-empty text response for status")
	}
}

func TestManagerStatusOllamaNotRunning(t *testing.T) {
	state := newTestState(t)
	// Point at a port nothing is listening on
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "status"}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
}

func TestManagerInstallReturnsPlan(t *testing.T) {
	state := newTestState(t)
	// Point at port nothing is listening on — ollama not running
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "install"}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) == 0 {
		t.Error("expected at least one command in install plan")
	}
	if plan[0].Tier == "" {
		t.Error("expected non-empty tier on install command")
	}
}

func TestManagerInstallAlreadyInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprintln(w, `{"models":[]}`)
		}
	}))
	defer srv.Close()

	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL)
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "install"}))

	var hadPlan bool
	var hadText bool
	for _, r := range resps {
		if r.Type == "plan" {
			hadPlan = true
		}
		if r.Type == "text" {
			hadText = true
		}
	}
	if hadPlan {
		t.Error("expected no plan when Ollama already running")
	}
	if !hadText {
		t.Error("expected text response saying already installed")
	}
}

func TestManagerListShowsRecommended(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprintln(w, `{"models":[{"name":"qwen2.5-coder:7b"}]}`)
		}
	}))
	defer srv.Close()

	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, srv.Client(), srv.URL)
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "list"}))

	var fullText string
	for _, r := range resps {
		if r.Type == "text" {
			fullText += r.Content
		}
	}
	if fullText == "" {
		t.Error("expected text output from list command")
	}
}

func TestManagerPullReturnsPlan(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "pull",
		Args:    []string{"llama3.2:3b"},
	}))

	var plan []protocol.CommandItem
	for _, r := range resps {
		if r.Type == "plan" {
			plan = r.Plan
		}
	}
	if len(plan) != 1 {
		t.Fatalf("expected 1 command in pull plan, got %d", len(plan))
	}
	if plan[0].Command != "ollama pull llama3.2:3b" {
		t.Errorf("unexpected pull command: %q", plan[0].Command)
	}
}

func TestManagerPullMissingArg(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "pull"}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
	var hasError bool
	for _, r := range resps {
		if r.Type == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error response when pull called without args")
	}
}

func TestManagerUseUpdatesState(t *testing.T) {
	dir := t.TempDir()
	state, _ := llm.LoadState(filepath.Join(dir, "llm-state.json"))
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "http://127.0.0.1:19999")

	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{
		Command: "use",
		Args:    []string{"mistral:7b"},
	}))

	last := resps[len(resps)-1]
	if last.Type != "done" {
		t.Errorf("expected 'done', got %q", last.Type)
	}
	if state.ActiveModel != "mistral:7b" {
		t.Errorf("expected active model 'mistral:7b', got %q", state.ActiveModel)
	}
}

func TestManagerUnknownCommand(t *testing.T) {
	state := newTestState(t)
	mgr := llm.NewManagerWithClient(state, &http.Client{}, "")
	resps := collectResponses(mgr.Handle(&protocol.LLMRequest{Command: "bogus"}))

	var hasError bool
	for _, r := range resps {
		if r.Type == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error response for unknown command")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/llm/...
```

Expected: FAIL — `Manager`, `NewManagerWithClient` not defined.

- [ ] **Step 3: Implement manager.go**

Create `internal/llm/manager.go`:

```go
package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"spaish/internal/permissions"
	"spaish/internal/protocol"
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
	state       *State
	client      *http.Client
	ollamaURL   string // overridable for tests; defaults to registry endpoint
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
```

- [ ] **Step 4: Run all llm tests**

```bash
go test ./internal/llm/... -v
```

Expected: PASS — all state, registry, and manager tests.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/manager.go internal/llm/manager_test.go
git commit -m "feat(llm): add manager orchestrating status/install/list/pull/use"
```

---

## Task 5: Wire into spaid

**Files:**
- Modify: `internal/socket/server.go`
- Modify: `cmd/spaid/main.go`

- [ ] **Step 1: Add LLMHandler to socket server**

In `internal/socket/server.go`, add the new handler type and extend `Serve`:

Current `Serve` signature:
```go
func Serve(sockPath string, onQuery QueryHandler, onExec ExecHandler) error
```

Replace the entire file with:

```go
package socket

import (
	"encoding/json"
	"net"
	"os"

	"spaish/internal/protocol"
)

// QueryHandler processes a query request and writes Response chunks to enc.
type QueryHandler func(req *protocol.Request, enc *json.Encoder)

// ExecHandler processes an execute request and writes Response chunks to enc.
type ExecHandler func(req *protocol.Request, enc *json.Encoder)

// LLMHandler processes an llm management request and writes Response chunks to enc.
type LLMHandler func(req *protocol.Request, enc *json.Encoder)

// Serve starts a Unix domain socket server at sockPath.
// Blocks until the listener is closed or an unrecoverable error occurs.
func Serve(sockPath string, onQuery QueryHandler, onExec ExecHandler, onLLM LLMHandler) error {
	os.Remove(sockPath)
	os.MkdirAll(sockPath[:len(sockPath)-len("/spaid.sock")], 0700)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil
		}
		go handleConn(conn, onQuery, onExec, onLLM)
	}
}

func handleConn(conn net.Conn, onQuery QueryHandler, onExec ExecHandler, onLLM LLMHandler) {
	defer conn.Close()

	var req protocol.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	enc := json.NewEncoder(conn)
	switch req.Type {
	case "query":
		onQuery(&req, enc)
	case "execute":
		onExec(&req, enc)
	case "llm":
		onLLM(&req, enc)
	default:
		enc.Encode(protocol.Response{Type: "error", Content: "unknown request type: " + req.Type})
	}
}
```

- [ ] **Step 2: Wire onLLM into spaid**

In `cmd/spaid/main.go`, add the following imports:

```go
"spaish/internal/llm"
```

After the `sess` initialization block (after `session.LoadFrom`), add:

```go
llmState, err := llm.LoadState(llm.DefaultStatePath())
if err != nil {
    log.Printf("llm state load warning: %v — using defaults", err)
    llmState, _ = llm.LoadState(llm.DefaultStatePath())
}
llmMgr := llm.NewManager(llmState)
```

Add the `onLLM` handler closure before the `socket.Serve` call:

```go
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
```

Update the `socket.Serve` call to pass `onLLM` as the fourth argument:

```go
if err := socket.Serve(sock, onQuery, onExec, onLLM); err != nil {
    log.Fatalf("socket error: %v", err)
}
```

- [ ] **Step 3: Build to verify**

```bash
go build ./...
```

Expected: exit 0, no errors.

- [ ] **Step 4: Run all tests**

```bash
go test ./...
```

Expected: all existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/socket/server.go cmd/spaid/main.go
git commit -m "feat(spaid): wire LLM manager handler into socket server"
```

---

## Task 6: spai CLI Subcommand

**Files:**
- Modify: `cmd/spai/main.go`

- [ ] **Step 1: Add handleLLMCommand function**

In `cmd/spai/main.go`, add these imports (merge with existing):

```go
"os"
"strings"
```

(Both are already imported — no change needed.)

Add the `handleLLMCommand` function before `main()`:

```go
// handleLLMCommand handles `spai llm <cmd> [args...]`.
// It sends an "llm" typed request to spaid and streams the response.
func handleLLMCommand(args []string) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("Usage: spai llm <command> [args]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  status          show runtime and model status")
		fmt.Println("  install         install Ollama on this machine")
		fmt.Println("  list            list installed and recommended models")
		fmt.Println("  pull <model>    download a model (e.g. qwen2.5-coder:7b)")
		fmt.Println("  use <model>     set the active model for local inference")
		os.Exit(0)
	}

	showDisclaimer()

	if err := socket.EnsureRunning(sockPath(), daemonBin()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	req := &protocol.Request{
		Type: "llm",
		LLM: &protocol.LLMRequest{
			Command: args[0],
			Args:    args[1:],
		},
	}

	client := socket.NewClient(sockPath())
	fmt.Println()

	var plan []protocol.CommandItem
	err := client.Send(req, func(resp protocol.Response) error {
		switch resp.Type {
		case "text":
			fmt.Print(resp.Content)
		case "plan":
			plan = resp.Plan
		case "error":
			fmt.Fprintf(os.Stderr, "\nerror: %s\n", resp.Content)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()

	if len(plan) == 0 {
		return
	}

	// Reuse existing confirmation flow for install/pull commands
	fmt.Println("I will run:")
	for _, item := range plan {
		fmt.Printf("  [%s] %s\n", item.Display, item.Command)
	}

	confirmed := confirmPlan(plan)
	if confirmed == nil {
		fmt.Println("Cancelled.")
		return
	}

	execReq := &protocol.Request{
		Type:     "execute",
		Commands: confirmed,
	}
	fmt.Println()
	client.Send(execReq, func(resp protocol.Response) error {
		switch resp.Type {
		case "output":
			fmt.Print(resp.Content)
		case "error":
			fmt.Fprintf(os.Stderr, "error: %s\n", resp.Content)
		}
		return nil
	})
}
```

- [ ] **Step 2: Intercept `llm` subcommand in main()**

At the very top of `main()`, before `flag.Parse()`, add:

```go
// Handle `spai llm <cmd>` before flag parsing so flags don't interfere.
if len(os.Args) >= 2 && os.Args[1] == "llm" {
    handleLLMCommand(os.Args[2:])
    return
}
```

The complete beginning of `main()` becomes:

```go
func main() {
	// Handle `spai llm <cmd>` before flag parsing so flags don't interfere.
	if len(os.Args) >= 2 && os.Args[1] == "llm" {
		handleLLMCommand(os.Args[2:])
		return
	}

	dryRun := flag.Bool("dry-run", false, "show plan without executing")
	// ... rest of existing main unchanged
```

- [ ] **Step 3: Build and verify CLI**

```bash
go build ./cmd/spai/ && ./spai llm help
```

Expected: prints the llm usage message and exits 0.

```bash
go build ./...
```

Expected: exit 0, both binaries build.

- [ ] **Step 4: Run full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/spai/main.go
git commit -m "feat(spai): add 'spai llm' subcommand for runtime and model management"
```

---

## Task 7: Feature Documentation

**Files:**
- `docs/llm-manager.md` — already created, commit it

- [ ] **Step 1: Commit the feature doc**

```bash
git add docs/llm-manager.md
git commit -m "docs: add llm-manager feature documentation"
```

---

## Self-Review

**Spec coverage:**

| Requirement | Task |
|-------------|------|
| `spai llm status` | Tasks 4, 6 |
| `spai llm install` | Tasks 3, 4, 6 |
| `spai llm list` | Tasks 4, 6 |
| `spai llm pull <model>` | Tasks 4, 6 |
| `spai llm use <model>` | Tasks 2, 4, 6 |
| State persisted to llm-state.json | Task 2 |
| Ollama-only, phase-1 scope | Task 3 |
| Install goes through permission tier engine | Tasks 3, 4 |
| Auto-detect existing Ollama install | Task 4 (`handleInstall` checks `isOllamaRunning`) |
| Backward compat with manual Ollama installs | Task 4 (status/list work without manager install) |
| No new dependencies | enforced throughout |
| Feature doc in docs/ | Task 7 |

**All requirements covered.**
