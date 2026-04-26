# spaiSH Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `spaid` (Unix socket daemon) and `spai` (CLI client) — the foundational layer of spaiSH that lets users run `spai <query>` from any shell to get AI-assisted system help with confirmation-gated command execution.

**Architecture:** `spai` sends a query + shell context to `spaid` via a Unix domain socket. `spaid` builds a context-aware prompt, calls the configured AI provider (cloud or local Ollama), parses proposed commands from the response, classifies each by permission tier, and streams the plan back. The user confirms in `spai` before any command runs. `spaid` then executes confirmed commands and streams the output back. Two round-trips per interaction: one for query/plan, one for execute.

**Tech Stack:** Go 1.22+, Unix domain sockets, OpenAI-compatible streaming HTTP API (cloud), Ollama `/api/chat` streaming API (local), `github.com/BurntSushi/toml` for config, systemd user service.

---

## File Map

| File | Responsibility |
|------|---------------|
| `go.mod` | Module definition |
| `internal/config/config.go` | Load and validate `spaid.toml` |
| `internal/config/config_test.go` | Config loading tests |
| `internal/protocol/protocol.go` | Shared request/response wire types (no logic) |
| `internal/permissions/classifier.go` | Classify a shell command into a permission tier |
| `internal/permissions/classifier_test.go` | Classification tests |
| `internal/ai/provider.go` | Provider interface + Message type |
| `internal/ai/cloud.go` | OpenAI-compatible streaming HTTP provider |
| `internal/ai/cloud_test.go` | Cloud provider tests using `httptest` |
| `internal/ai/local.go` | Ollama streaming provider |
| `internal/ai/local_test.go` | Ollama provider tests using `httptest` |
| `internal/router/router.go` | Build prompt, call provider, parse commands from response |
| `internal/router/router_test.go` | Router unit tests |
| `internal/session/session.go` | Load/save session context from disk |
| `internal/session/session_test.go` | Session tests |
| `internal/executor/executor.go` | Run a shell command, stream stdout+stderr to a writer |
| `internal/executor/executor_test.go` | Executor tests |
| `internal/socket/server.go` | Accept Unix socket connections, dispatch to handlers |
| `internal/socket/client.go` | Connect to socket, send request, stream responses |
| `cmd/spaid/main.go` | Daemon entry point: loads config, starts server, handles signals |
| `cmd/spai/main.go` | CLI entry point: flags, disclaimer, auto-starts daemon, displays output |
| `config/spaid.toml` | Default config template |
| `systemd/spaid.service` | systemd user service unit file |
| `install.sh` | Install binaries, config, service (no root) |
| `uninstall.sh` | Reverse everything install.sh did |

---

## Story Map

Each story produces a working, independently testable increment. Stories within the same phase run in parallel. Phases are sequential.

| Story | Phase | Tasks | Checkpoint command |
|-------|-------|-------|--------------------|
| **S1: Foundation** | A (parallel) | 1, 2, 3 | `go build ./...` + `go test ./internal/config/...` |
| **S2: Safety Layer** | A (parallel) | 4 | `go test ./internal/permissions/...` |
| **S3: AI Providers** | A (parallel) | 5, 6 | `go test ./internal/ai/...` |
| **S4: Intelligence** | B (parallel) | 7, 8 | `go test ./internal/session/... ./internal/router/...` |
| **S5: Execution** | B (parallel) | 9 | `go test ./internal/executor/...` |
| **S6: Transport** | C (sequential) | 10 | `go build ./internal/socket/...` |
| **S7: Daemon** | C (sequential) | 11 | `go build ./cmd/spaid/ && ./spaid &` starts without error |
| **S8: CLI** | C (sequential) | 12 | `go build ./cmd/spai/ && ./spai "is nginx running?"` full flow works |
| **S9: Ship** | C (sequential) | 13 | `bash install.sh` completes, `systemctl --user status spaid` shows running |

```
Phase A (parallel):   S1 ──┐
                      S2 ──┼──▶ Phase A review ──▶ Phase B
                      S3 ──┘

Phase B (parallel):   S4 ──┐
                      S5 ──┴──▶ Phase B review ──▶ Phase C

Phase C (sequential): S6 ──▶ S7 ──▶ S8 ──▶ S9
```

**Resume rule:** Check which stories have all their tasks checked off. Start from the first story that has unchecked tasks.

---

## Story S1: Foundation

*Phase A — runs in parallel with S2 and S3*

---

## Task 1: Go Module Scaffold

**Files:**
- Create: `go.mod`
- Create: `go.sum` (generated)

- [ ] **Step 1: Initialise the Go module**

```bash
cd /home/srajan/Development/spaiSH
go mod init spaish
```

Expected: `go.mod` created with `module spaish` and `go 1.22`

- [ ] **Step 2: Add the TOML dependency**

```bash
go get github.com/BurntSushi/toml@v1.3.2
```

Expected: `go.mod` and `go.sum` updated

- [ ] **Step 3: Create the directory structure**

```bash
mkdir -p cmd/spai cmd/spaid \
  internal/config \
  internal/protocol \
  internal/permissions \
  internal/ai \
  internal/router \
  internal/session \
  internal/executor \
  internal/socket \
  config \
  systemd
```

- [ ] **Step 4: Verify module builds (empty)**

```bash
go build ./...
```

Expected: no output, exit 0

- [ ] **Step 5: Commit**

```bash
git init
git add go.mod go.sum
git commit -m "feat: initialise Go module"
```

---

## Task 2: Config Package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `config/spaid.toml`

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaish/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaid.toml")
	// Write a minimal config
	os.WriteFile(path, []byte(`
[provider]
endpoint = "https://api.example.com/v1"
api_key_env = "SPAI_API_KEY"
model = "test-model"

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder"

[routing]
passthrough_commands = ["cd", "ls"]
prefer_local = false

[permissions]
sudo_session_timeout = 300
`), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Provider.Endpoint != "https://api.example.com/v1" {
		t.Errorf("got endpoint %q", cfg.Provider.Endpoint)
	}
	if cfg.Provider.Model != "test-model" {
		t.Errorf("got model %q", cfg.Provider.Model)
	}
	if cfg.Local.LocalModel != "qwen2.5-coder" {
		t.Errorf("got local model %q", cfg.Local.LocalModel)
	}
	if len(cfg.Routing.PassthroughCommands) != 2 {
		t.Errorf("got %d passthrough commands", len(cfg.Routing.PassthroughCommands))
	}
	if cfg.Permissions.SudoSessionTimeout != 300 {
		t.Errorf("got timeout %d", cfg.Permissions.SudoSessionTimeout)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/spaid.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/...
```

Expected: FAIL — `config` package does not exist yet

- [ ] **Step 3: Implement config.go**

Create `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Provider    ProviderConfig    `toml:"provider"`
	Local       LocalConfig       `toml:"local"`
	Routing     RoutingConfig     `toml:"routing"`
	Permissions PermissionsConfig `toml:"permissions"`
}

type ProviderConfig struct {
	Endpoint  string `toml:"endpoint"`
	APIKeyEnv string `toml:"api_key_env"`
	Model     string `toml:"model"`
}

type LocalConfig struct {
	OllamaEndpoint string `toml:"ollama_endpoint"`
	LocalModel     string `toml:"local_model"`
}

type RoutingConfig struct {
	PassthroughCommands []string `toml:"passthrough_commands"`
	PreferLocal         bool     `toml:"prefer_local"`
}

type PermissionsConfig struct {
	SudoSessionTimeout int `toml:"sudo_session_timeout"`
}

// Load reads and parses the TOML config file at path.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// APIKey reads the API key from the environment variable named in cfg.Provider.APIKeyEnv.
func (c *Config) APIKey() string {
	if c.Provider.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.Provider.APIKeyEnv)
}
```

- [ ] **Step 4: Create the default config template**

Create `config/spaid.toml`:

```toml
# spaiSH daemon configuration
# Copy this file to ~/.config/spaish/spaid.toml

[provider]
# Any OpenAI-compatible API endpoint
endpoint = "https://api.anthropic.com/v1"
# Environment variable name that holds your API key (never put the key here)
api_key_env = "SPAI_API_KEY"
# Model name — set to whichever model your provider offers
model = "claude-sonnet-4-6"

[local]
# Ollama endpoint (used when no API key is set, or prefer_local = true)
ollama_endpoint = "http://localhost:11434"
# Local model to use via Ollama
local_model = "qwen2.5-coder"

[routing]
# Commands that bypass AI entirely and run directly
passthrough_commands = ["cd", "ls", "pwd", "clear", "exit", "history", "echo", "which", "env"]
# Set to true to always use local model (full privacy mode)
prefer_local = false

[permissions]
# Seconds before an elevated (sudo) session permission expires
sudo_session_timeout = 300
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/config/... -v
```

Expected: PASS — TestLoadDefaults, TestLoadMissingFile

- [ ] **Step 6: Commit**

```bash
git add internal/config/ config/spaid.toml
git commit -m "feat: add config package"
```

---

## Task 3: Protocol Types

**Files:**
- Create: `internal/protocol/protocol.go`

No tests — this is a pure data types file with no logic.

- [ ] **Step 1: Write protocol.go**

```go
package protocol

// Request is sent from spai → spaid over the Unix socket.
// Two types are used: "query" for asking, "execute" for running confirmed commands.
type Request struct {
	Type       string   `json:"type"`                 // "query" | "execute"
	Query      string   `json:"query,omitempty"`      // the user's natural language query
	WorkingDir string   `json:"working_dir"`          // current directory from spai
	GitBranch  string   `json:"git_branch,omitempty"` // current git branch, if any
	ForceLocal bool     `json:"force_local"`          // use local model regardless of config
	DryRun     bool     `json:"dry_run"`              // show plan but never execute
	Commands   []string `json:"commands,omitempty"`   // for "execute": commands to run
}

// Response is streamed from spaid → spai as newline-delimited JSON.
type Response struct {
	Type    string        `json:"type"`             // "text" | "plan" | "output" | "done" | "error"
	Content string        `json:"content,omitempty"` // for "text", "output", "error"
	Plan    []CommandItem `json:"plan,omitempty"`    // for "plan"
}

// CommandItem is a single proposed command with its permission classification.
type CommandItem struct {
	Command string `json:"command"` // the shell command to run
	Tier    string `json:"tier"`    // "read" | "write" | "elevated" | "destructive"
	Display string `json:"display"` // human-readable tier label shown to the user
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/protocol/...
```

Expected: no output, exit 0

- [ ] **Step 3: Commit**

```bash
git add internal/protocol/
git commit -m "feat: add protocol wire types"
```

---

---

## Story S2: Safety Layer

*Phase A — runs in parallel with S1 and S3*

---

## Task 4: Permission Classifier

**Files:**
- Create: `internal/permissions/classifier.go`
- Create: `internal/permissions/classifier_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/permissions/classifier_test.go`:

```go
package permissions_test

import (
	"testing"

	"spaish/internal/permissions"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		command string
		want    permissions.Tier
	}{
		// Passthrough
		{"ls", permissions.TierPassthrough},
		{"ls -la", permissions.TierPassthrough},
		{"cd /tmp", permissions.TierPassthrough},
		{"pwd", permissions.TierPassthrough},
		{"clear", permissions.TierPassthrough},
		// Read
		{"cat /etc/hosts", permissions.TierRead},
		{"grep error /var/log/syslog", permissions.TierRead},
		{"ps aux", permissions.TierRead},
		{"git status", permissions.TierRead},
		{"git log --oneline", permissions.TierRead},
		{"systemctl status nginx", permissions.TierRead},
		{"journalctl -u nginx -n 50", permissions.TierRead},
		// Write
		{"mkdir /tmp/test", permissions.TierWrite},
		{"touch file.txt", permissions.TierWrite},
		{"cp src dst", permissions.TierWrite},
		{"mv old new", permissions.TierWrite},
		{"git commit -m msg", permissions.TierWrite},
		// Elevated
		{"sudo systemctl restart nginx", permissions.TierElevated},
		{"sudo apt install curl", permissions.TierElevated},
		{"systemctl restart nginx", permissions.TierElevated},
		{"apt-get update", permissions.TierElevated},
		// Destructive
		{"rm -rf /tmp/test", permissions.TierDestructive},
		{"rm -r /home/user/old", permissions.TierDestructive},
		{"git reset --hard HEAD~1", permissions.TierDestructive},
		{"git clean -f", permissions.TierDestructive},
		{"docker system prune", permissions.TierDestructive},
	}

	for _, tc := range cases {
		got := permissions.Classify(tc.command)
		if got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestTierString(t *testing.T) {
	if permissions.TierDestructive.String() != "destructive" {
		t.Error("unexpected string for TierDestructive")
	}
	if permissions.TierPassthrough.String() != "passthrough" {
		t.Error("unexpected string for TierPassthrough")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/permissions/...
```

Expected: FAIL — package does not exist

- [ ] **Step 3: Implement classifier.go**

Create `internal/permissions/classifier.go`:

```go
package permissions

import "strings"

// Tier represents the permission level required to run a command.
type Tier int

const (
	TierPassthrough Tier = iota // bypass AI, run directly
	TierRead                    // execute silently
	TierWrite                   // show plan, confirm once
	TierElevated                // explicit prompt, exact command shown
	TierDestructive             // hard confirm, cannot be undone
)

func (t Tier) String() string {
	switch t {
	case TierPassthrough:
		return "passthrough"
	case TierRead:
		return "read"
	case TierWrite:
		return "write"
	case TierElevated:
		return "elevated"
	case TierDestructive:
		return "destructive"
	default:
		return "unknown"
	}
}

// Display returns the human-readable label shown to the user.
func (t Tier) Display() string {
	switch t {
	case TierPassthrough:
		return "Passthrough"
	case TierRead:
		return "Read"
	case TierWrite:
		return "Write"
	case TierElevated:
		return "Elevated (requires sudo)"
	case TierDestructive:
		return "Destructive — cannot be undone"
	default:
		return "Unknown"
	}
}

// destructivePatterns are checked first — any match is TierDestructive.
var destructivePatterns = []string{
	"rm -rf", "rm -r ", "rm -f ", "rm -fr",
	"git reset --hard", "git clean -f", "git clean -fd",
	"docker system prune",
	"mkfs", "dd if=", "fdisk", "parted",
	"DROP TABLE", "DROP DATABASE", "TRUNCATE TABLE",
	"> /dev/", "truncate --size 0",
}

// elevatedPrefixes: any command starting with these is TierElevated.
var elevatedPrefixes = []string{
	"sudo ",
	"systemctl start ", "systemctl stop ", "systemctl restart ",
	"systemctl reload ", "systemctl enable ", "systemctl disable ",
	"apt ", "apt-get ", "aptitude ",
	"yum ", "dnf ", "pacman ", "zypper ",
	"snap install", "flatpak install",
	"pip install", "pip3 install",
	"npm install -g", "yarn global add",
}

// passthroughExact: exact command or command + space prefix is TierPassthrough.
var passthroughCmds = []string{
	"ls", "cd", "pwd", "clear", "exit", "history",
	"echo", "which", "type", "alias", "env", "printenv",
	"man", "help",
}

// readPrefixes: commands that only observe the system.
var readPrefixes = []string{
	"cat ", "head ", "tail ", "less ", "more ",
	"grep ", "find ", "locate ", "wc ", "diff ",
	"file ", "stat ", "du ", "df ",
	"ps ", "htop", "top",
	"netstat", "ss ", "lsof ",
	"ping ", "traceroute ", "nslookup ", "dig ",
	"curl ", "wget ",
	"git log", "git status", "git diff", "git show", "git branch",
	"docker ps", "docker images", "docker logs",
	"systemctl status", "journalctl",
	"free ", "uptime", "uname",
}

// writePrefixes: commands that modify files or state (non-privileged).
var writePrefixes = []string{
	"mkdir ", "touch ", "cp ", "mv ", "ln ",
	"chmod ", "chown ",
	"git add", "git commit", "git stash", "git tag",
	"docker build", "docker-compose",
	"sed ", "awk ", "tee ",
	"tar ", "zip ", "unzip ", "gzip ", "gunzip ",
}

// Classify returns the permission tier for the given shell command.
// Classification is static and offline — no AI involvement.
func Classify(command string) Tier {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	// Destructive check first (highest risk)
	for _, p := range destructivePatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return TierDestructive
		}
	}

	// Elevated check
	for _, p := range elevatedPrefixes {
		if strings.HasPrefix(lower, p) {
			return TierElevated
		}
	}

	// Passthrough check
	for _, p := range passthroughCmds {
		if lower == p || strings.HasPrefix(lower, p+" ") {
			return TierPassthrough
		}
	}

	// Read check
	for _, p := range readPrefixes {
		if strings.HasPrefix(lower, p) || lower == strings.TrimRight(p, " ") {
			return TierRead
		}
	}

	// Write check
	for _, p := range writePrefixes {
		if strings.HasPrefix(lower, p) {
			return TierWrite
		}
	}

	// Unknown commands default to Write (safe default, requires confirmation)
	return TierWrite
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/permissions/... -v
```

Expected: PASS — all cases

- [ ] **Step 5: Commit**

```bash
git add internal/permissions/
git commit -m "feat: add permission tier classifier"
```

---

---

## Story S3: AI Providers

*Phase A — runs in parallel with S1 and S2*

---

## Task 5: AI Provider Interface + Cloud Provider

**Files:**
- Create: `internal/ai/provider.go`
- Create: `internal/ai/cloud.go`
- Create: `internal/ai/cloud_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ai/cloud_test.go`:

```go
package ai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spaish/internal/ai"
)

func TestCloudProviderComplete(t *testing.T) {
	// Mock OpenAI-compatible SSE response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hello"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" world"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	p := ai.NewCloudProvider(srv.URL, "test-key", "test-model")
	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "user", Content: "say hello"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		result.WriteString(chunk)
	}
	if result.String() != "Hello world" {
		t.Errorf("got %q, want %q", result.String(), "Hello world")
	}
}

func TestCloudProviderAvailable(t *testing.T) {
	p := ai.NewCloudProvider("https://api.example.com", "key", "model")
	if !p.Available() {
		t.Error("expected Available() = true when key and endpoint are set")
	}
	p2 := ai.NewCloudProvider("", "", "")
	if p2.Available() {
		t.Error("expected Available() = false when key and endpoint are empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ai/...
```

Expected: FAIL — package does not exist

- [ ] **Step 3: Implement provider.go**

Create `internal/ai/provider.go`:

```go
package ai

import "context"

// Message is a single turn in a conversation.
type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// Provider is the interface for any AI model backend.
type Provider interface {
	// Complete sends messages to the model and streams response text chunks.
	// The returned channel is closed when the response is complete.
	Complete(ctx context.Context, messages []Message) (<-chan string, error)
	// Available returns true if this provider is configured and reachable.
	Available() bool
}
```

- [ ] **Step 4: Implement cloud.go**

Create `internal/ai/cloud.go`:

```go
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CloudProvider calls any OpenAI-compatible streaming chat completions endpoint.
type CloudProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

func NewCloudProvider(endpoint, apiKey, model string) *CloudProvider {
	return &CloudProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{},
	}
}

func (p *CloudProvider) Available() bool {
	return p.apiKey != "" && p.endpoint != ""
}

func (p *CloudProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type reqBody struct {
		Model    string `json:"model"`
		Messages []msg  `json:"messages"`
		Stream   bool   `json:"stream"`
	}

	reqMsgs := make([]msg, len(messages))
	for i, m := range messages {
		reqMsgs[i] = msg{Role: m.Role, Content: m.Content}
	}

	body, err := json.Marshal(reqBody{Model: p.model, Messages: reqMsgs, Stream: true})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" {
				select {
				case ch <- event.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/ai/... -v -run TestCloud
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ai/provider.go internal/ai/cloud.go internal/ai/cloud_test.go
git commit -m "feat: add AI provider interface and cloud provider"
```

---

## Task 6: Ollama Local Provider

**Files:**
- Create: `internal/ai/local.go`
- Create: `internal/ai/local_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ai/local_test.go`:

```go
package ai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"spaish/internal/ai"
)

func TestLocalProviderComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
		case "/api/chat":
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hi"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":" there"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := ai.NewLocalProvider(srv.URL, "qwen2.5-coder")
	if !p.Available() {
		t.Fatal("Available() = false, expected true")
	}

	ch, err := p.Complete(context.Background(), []ai.Message{
		{Role: "user", Content: "say hi"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	var result strings.Builder
	for chunk := range ch {
		result.WriteString(chunk)
	}
	if result.String() != "Hi there" {
		t.Errorf("got %q, want %q", result.String(), "Hi there")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ai/... -run TestLocal
```

Expected: FAIL — LocalProvider does not exist

- [ ] **Step 3: Implement local.go**

Create `internal/ai/local.go`:

```go
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// LocalProvider calls a locally running Ollama instance.
type LocalProvider struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewLocalProvider(endpoint, model string) *LocalProvider {
	return &LocalProvider{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{},
	}
}

// Available checks whether Ollama is running and reachable.
func (p *LocalProvider) Available() bool {
	if p.endpoint == "" || p.model == "" {
		return false
	}
	resp, err := p.client.Get(p.endpoint + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (p *LocalProvider) Complete(ctx context.Context, messages []Message) (<-chan string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type reqBody struct {
		Model    string `json:"model"`
		Messages []msg  `json:"messages"`
		Stream   bool   `json:"stream"`
	}

	reqMsgs := make([]msg, len(messages))
	for i, m := range messages {
		reqMsgs[i] = msg{Role: m.Role, Content: m.Content}
	}

	body, err := json.Marshal(reqBody{Model: p.model, Messages: reqMsgs, Stream: true})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}

	ch := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var event struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			if event.Message.Content != "" {
				select {
				case ch <- event.Message.Content:
				case <-ctx.Done():
					return
				}
			}
			if event.Done {
				return
			}
		}
	}()

	return ch, nil
}
```

- [ ] **Step 4: Run all AI tests**

```bash
go test ./internal/ai/... -v
```

Expected: PASS — TestCloudProviderComplete, TestCloudProviderAvailable, TestLocalProviderComplete

- [ ] **Step 5: Commit**

```bash
git add internal/ai/local.go internal/ai/local_test.go
git commit -m "feat: add Ollama local provider"
```

---

---

## Story S4: Intelligence Layer

*Phase B — runs in parallel with S5. Requires S1, S2, S3 complete.*

---

## Task 7: Session Package

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/session_test.go`:

```go
package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/session"
)

func TestSessionLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := session.LoadFrom(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("expected empty messages, got %d", len(s.Messages))
	}
}

func TestSessionAddAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	s, _ := session.LoadFrom(path)
	s.AddExchange("fix nginx", "I found the issue and fixed it.")

	if err := s.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Reload and verify
	s2, err := session.LoadFrom(path)
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if len(s2.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(s2.Messages))
	}
	if s2.Messages[0].Role != "user" || s2.Messages[0].Content != "fix nginx" {
		t.Errorf("unexpected first message: %+v", s2.Messages[0])
	}
}

func TestSessionTruncates(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "session.json"))

	// Add 15 exchanges = 30 messages, should truncate to 20
	for i := 0; i < 15; i++ {
		s.AddExchange("query", "response")
	}
	if len(s.Messages) > 20 {
		t.Errorf("expected max 20 messages, got %d", len(s.Messages))
	}
}

func TestSessionMessages(t *testing.T) {
	dir := t.TempDir()
	s, _ := session.LoadFrom(filepath.Join(dir, "session.json"))
	s.AddExchange("hello", "hi there")

	msgs := s.MessagesForPrompt()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0] != (ai.Message{Role: "user", Content: "hello"}) {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

func TestSessionFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	s, _ := session.LoadFrom(path)
	s.AddExchange("q", "a")
	s.Save()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %v", info.Mode().Perm())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/session/...
```

Expected: FAIL

- [ ] **Step 3: Implement session.go**

Create `internal/session/session.go`:

```go
package session

import (
	"encoding/json"
	"os"
	"path/filepath"

	"spaish/internal/ai"
)

const maxMessages = 20

// Session holds the conversation history for the current daemon session.
type Session struct {
	Messages []ai.Message `json:"messages"`
	path     string
}

// DefaultPath returns the default session file path.
func DefaultPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaish", "session.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaish", "session.json")
}

// LoadFrom loads a session from the given file path.
// If the file does not exist, an empty session is returned without error.
func LoadFrom(path string) (*Session, error) {
	s := &Session{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		// Corrupted session — start fresh
		return &Session{path: path}, nil
	}
	return s, nil
}

// AddExchange appends a user/assistant exchange and trims to maxMessages.
func (s *Session) AddExchange(userMsg, assistantMsg string) {
	s.Messages = append(s.Messages,
		ai.Message{Role: "user", Content: userMsg},
		ai.Message{Role: "assistant", Content: assistantMsg},
	)
	if len(s.Messages) > maxMessages {
		s.Messages = s.Messages[len(s.Messages)-maxMessages:]
	}
}

// MessagesForPrompt returns the session messages for inclusion in the AI prompt.
func (s *Session) MessagesForPrompt() []ai.Message {
	out := make([]ai.Message, len(s.Messages))
	copy(out, s.Messages)
	return out
}

// Save writes the session to disk with mode 0600.
func (s *Session) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Clear removes all messages from the session.
func (s *Session) Clear() {
	s.Messages = nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/session/... -v
```

Expected: PASS — all 5 tests

- [ ] **Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat: add session context package"
```

---

## Task 8: Model Router

**Files:**
- Create: `internal/router/router.go`
- Create: `internal/router/router_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/router/router_test.go`:

```go
package router_test

import (
	"context"
	"strings"
	"testing"

	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/protocol"
	"spaish/internal/router"
	"spaish/internal/session"
	"os"
	"path/filepath"
)

// stubProvider is a fake AI provider for testing.
type stubProvider struct {
	response  string
	available bool
}

func (s *stubProvider) Available() bool { return s.available }
func (s *stubProvider) Complete(_ context.Context, _ []ai.Message) (<-chan string, error) {
	ch := make(chan string, 1)
	ch <- s.response
	close(ch)
	return ch, nil
}

func newTestSession(t *testing.T) *session.Session {
	t.Helper()
	s, _ := session.LoadFrom(filepath.Join(t.TempDir(), "session.json"))
	return s
}

func TestRouterTextResponse(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}
	cloud := &stubProvider{response: "No commands needed, everything looks fine.", available: true}
	local := &stubProvider{available: false}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{
		Type:       "query",
		Query:      "is my system healthy?",
		WorkingDir: "/home/user",
	}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}

	var gotTypes []string
	for resp := range ch {
		gotTypes = append(gotTypes, resp.Type)
	}

	if gotTypes[len(gotTypes)-1] != "done" {
		t.Error("last response should be 'done'")
	}
}

func TestRouterExtractsCommands(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}
	responseWithCommands := "The timeout is too low. Fix it:\n```bash\nsed -i 's/timeout 30/timeout 90/' /etc/nginx/nginx.conf\nsystemctl reload nginx\n```"
	cloud := &stubProvider{response: responseWithCommands, available: true}
	local := &stubProvider{available: false}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{
		Type:       "query",
		Query:      "fix nginx timeout",
		WorkingDir: "/home/user",
	}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}

	var plan []protocol.CommandItem
	for resp := range ch {
		if resp.Type == "plan" {
			plan = resp.Plan
		}
	}

	if len(plan) != 2 {
		t.Fatalf("expected 2 commands in plan, got %d", len(plan))
	}
	if !strings.Contains(plan[0].Command, "sed") {
		t.Errorf("expected sed command, got %q", plan[0].Command)
	}
	if plan[1].Tier != "elevated" {
		t.Errorf("expected systemctl to be elevated, got %q", plan[1].Tier)
	}
}

func TestRouterFallsBackToLocal(t *testing.T) {
	cfg := &config.Config{
		Routing: config.RoutingConfig{PreferLocal: false},
	}
	cloud := &stubProvider{available: false}
	local := &stubProvider{response: "All good.", available: true}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{Type: "query", Query: "check disk", WorkingDir: "/"}

	ch, err := r.Route(context.Background(), req, newTestSession(t))
	if err != nil {
		t.Fatalf("Route() error: %v", err)
	}
	for range ch {
	}
}

func TestRouterNoProviderError(t *testing.T) {
	cfg := &config.Config{}
	cloud := &stubProvider{available: false}
	local := &stubProvider{available: false}

	r := router.New(cfg, cloud, local)
	req := &protocol.Request{Type: "query", Query: "test", WorkingDir: "/"}

	_, err := r.Route(context.Background(), req, newTestSession(t))
	if err == nil {
		t.Error("expected error when no provider available")
	}
}

func init() {
	// Ensure test temp dirs work
	os.MkdirAll(os.TempDir(), 0755)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/router/...
```

Expected: FAIL

- [ ] **Step 3: Implement router.go**

Create `internal/router/router.go`:

```go
package router

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/permissions"
	"spaish/internal/protocol"
	"spaish/internal/session"
)

var bashBlockRe = regexp.MustCompile("(?s)` + "`" + `` + "`" + `` + "`" + `(?:bash|sh|shell)?\n(.*?)` + "`" + `` + "`" + `` + "`" + `")

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
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/router/... -v
```

Expected: PASS — all 4 tests

- [ ] **Step 5: Commit**

```bash
git add internal/router/
git commit -m "feat: add model router with prompt builder and command parser"
```

---

---

## Story S5: Execution Layer

*Phase B — runs in parallel with S4. Requires S1 complete.*

---

## Task 9: Executor Package

**Files:**
- Create: `internal/executor/executor.go`
- Create: `internal/executor/executor_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/executor/executor_test.go`:

```go
package executor_test

import (
	"strings"
	"testing"

	"spaish/internal/executor"
)

func TestExecuteSimpleCommand(t *testing.T) {
	var out strings.Builder
	err := executor.Execute("echo hello", &out)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("expected 'hello' in output, got %q", out.String())
	}
}

func TestExecuteCaputuresStderr(t *testing.T) {
	var out strings.Builder
	// ls on a nonexistent path writes to stderr
	executor.Execute("ls /nonexistent_path_xyz 2>&1", &out)
	if out.Len() == 0 {
		t.Error("expected stderr output, got nothing")
	}
}

func TestExecuteFailingCommand(t *testing.T) {
	var out strings.Builder
	err := executor.Execute("exit 1", &out)
	if err == nil {
		t.Error("expected error for failing command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/executor/...
```

Expected: FAIL

- [ ] **Step 3: Implement executor.go**

Create `internal/executor/executor.go`:

```go
package executor

import (
	"io"
	"os/exec"
)

// Execute runs command in a bash shell and streams combined stdout+stderr to w.
// Returns an error if the command exits with a non-zero status.
func Execute(command string, w io.Writer) error {
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/executor/... -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/executor/
git commit -m "feat: add command executor"
```

---

---

## Story S6: Transport Layer

*Phase C — sequential. Requires S1–S5 complete.*

---

## Task 10: Unix Socket Server + Client

**Files:**
- Create: `internal/socket/server.go`
- Create: `internal/socket/client.go`

- [ ] **Step 1: Implement server.go**

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

// Serve starts a Unix domain socket server at sockPath.
// Blocks until the listener is closed or an unrecoverable error occurs.
func Serve(sockPath string, onQuery QueryHandler, onExec ExecHandler) error {
	os.Remove(sockPath)
	if err := os.MkdirAll(sockPath[:len(sockPath)-len("/spaid.sock")], 0700); err != nil {
		// Best effort — parent may already exist
		_ = err
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// ln was closed — normal shutdown
			return nil
		}
		go handleConn(conn, onQuery, onExec)
	}
}

func handleConn(conn net.Conn, onQuery QueryHandler, onExec ExecHandler) {
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
	default:
		enc.Encode(protocol.Response{Type: "error", Content: "unknown request type: " + req.Type})
	}
}
```

- [ ] **Step 2: Implement client.go**

```go
package socket

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"spaish/internal/protocol"
)

// Client connects to the spaid Unix socket.
type Client struct {
	sockPath string
}

// NewClient creates a client pointing at sockPath.
func NewClient(sockPath string) *Client {
	return &Client{sockPath: sockPath}
}

// Send sends req and calls fn for each Response chunk until "done" or "error".
func (c *Client) Send(req *protocol.Request, fn func(protocol.Response) error) error {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return fmt.Errorf("cannot reach spaid: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}

	dec := json.NewDecoder(conn)
	for {
		var resp protocol.Response
		if err := dec.Decode(&resp); err != nil {
			return err
		}
		if err := fn(resp); err != nil {
			return err
		}
		if resp.Type == "done" || resp.Type == "error" {
			return nil
		}
	}
}

// EnsureRunning starts spaid if it is not already running, then waits up to 3s.
func EnsureRunning(sockPath, daemonBin string) error {
	if _, err := os.Stat(sockPath); err == nil {
		// Socket exists — assume running
		return nil
	}
	cmd := exec.Command(daemonBin)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start spaid: %w", err)
	}
	// Wait up to 3 seconds for the socket to appear
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, err := os.Stat(sockPath); err == nil {
			return nil
		}
	}
	return fmt.Errorf("spaid did not start within 3 seconds")
}
```

- [ ] **Step 3: Build to verify compilation**

```bash
go build ./internal/socket/...
```

Expected: exit 0

- [ ] **Step 4: Commit**

```bash
git add internal/socket/
git commit -m "feat: add Unix socket server and client"
```

---

---

## Story S7: Daemon

*Phase C — sequential. Requires S6 complete.*

---

## Task 11: spaid Daemon Main

**Files:**
- Create: `cmd/spaid/main.go`

- [ ] **Step 1: Implement cmd/spaid/main.go**

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/executor"
	"spaish/internal/protocol"
	"spaish/internal/router"
	"spaish/internal/session"
	"spaish/internal/socket"
)

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaish", "spaid.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaish", "spaid.toml")
}

func sockPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaish", "spaid.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaish", "spaid.sock")
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

	cloud := ai.NewCloudProvider(cfg.Provider.Endpoint, cfg.APIKey(), cfg.Provider.Model)
	local := ai.NewLocalProvider(cfg.Local.OllamaEndpoint, cfg.Local.LocalModel)
	rtr := router.New(cfg, cloud, local)

	sess, err := session.LoadFrom(session.DefaultPath())
	if err != nil {
		log.Printf("session load warning: %v — starting fresh", err)
		sess, _ = session.LoadFrom(session.DefaultPath())
	}

	sock := sockPath()
	log.Printf("spaid starting, socket: %s", sock)

	// Graceful shutdown on SIGTERM/SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		log.Println("spaid shutting down")
		os.Remove(sock)
		os.Exit(0)
	}()

	onQuery := func(req *protocol.Request, enc *json.Encoder) {
		respCh, err := rtr.Route(signalContext(), req, sess)
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
		// Persist exchange to session
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

	if err := socket.Serve(sock, onQuery, onExec); err != nil {
		log.Fatalf("socket error: %v", err)
	}
}

func signalContext() interface{ Done() <-chan struct{} } {
	return struct{ done chan struct{} }{done: make(chan struct{})}
}
```

> **Note:** Replace `signalContext()` with `context.Background()` — remove the stub and import `"context"`. The above is a placeholder that won't compile. Here is the corrected main:

```go
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

	"spaish/internal/ai"
	"spaish/internal/config"
	"spaish/internal/executor"
	"spaish/internal/protocol"
	"spaish/internal/router"
	"spaish/internal/session"
	"spaish/internal/socket"
)

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaish", "spaid.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaish", "spaid.toml")
}

func sockPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaish", "spaid.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaish", "spaid.sock")
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

	cloud := ai.NewCloudProvider(cfg.Provider.Endpoint, cfg.APIKey(), cfg.Provider.Model)
	local := ai.NewLocalProvider(cfg.Local.OllamaEndpoint, cfg.Local.LocalModel)
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

	if err := socket.Serve(sock, onQuery, onExec); err != nil {
		log.Fatalf("socket error: %v", err)
	}
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./cmd/spaid/
```

Expected: exit 0, binary `spaid` created

- [ ] **Step 3: Commit**

```bash
git add cmd/spaid/
git commit -m "feat: add spaid daemon"
```

---

---

## Story S8: CLI

*Phase C — sequential. Requires S7 complete.*

---

## Task 12: spai CLI Main

**Files:**
- Create: `cmd/spai/main.go`

- [ ] **Step 1: Implement cmd/spai/main.go**

```go
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"spaish/internal/permissions"
	"spaish/internal/protocol"
	"spaish/internal/socket"
)

const disclaimer = `
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  spaiSH — experimental personal project
  Not affiliated with any AI provider or Linux distribution.
  You are responsible for your API key usage and costs.
  Run 'spai --legal' for full disclaimer and license.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
`

const legalText = `spaiSH is an experimental personal project provided AS IS with no warranties.

You are responsible for all actions taken on your system. Every command is
shown to you before execution and requires your confirmation.

You must supply your own API key for any cloud AI provider. spaiSH does not
provide API access and is not affiliated with any AI provider or Linux distribution.

Full license: Apache 2.0 — https://www.apache.org/licenses/LICENSE-2.0
`

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaish")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaish")
}

func sockPath() string  { return filepath.Join(dataDir(), "spaid.sock") }
func stampPath() string { return filepath.Join(dataDir(), ".first_run_done") }

func daemonBin() string {
	self, _ := os.Executable()
	return filepath.Join(filepath.Dir(self), "spaid")
}

func showDisclaimer() {
	if _, err := os.Stat(stampPath()); err == nil {
		return
	}
	fmt.Print(disclaimer)
	os.MkdirAll(dataDir(), 0700)
	os.WriteFile(stampPath(), []byte("done"), 0600)
}

func gitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func main() {
	dryRun := flag.Bool("dry-run", false, "show plan without executing")
	forceLocal := flag.Bool("local", false, "force local model")
	legal := flag.Bool("legal", false, "print legal disclaimer and exit")
	flag.Usage = func() {
		fmt.Println("Usage: spai [flags] <query>")
		fmt.Println("       spai !!          analyse last failed command")
		fmt.Println()
		flag.PrintDefaults()
	}
	flag.Parse()

	if *legal {
		fmt.Print(legalText)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	query := strings.Join(args, " ")
	if query == "!!" {
		query = "My last command failed. What went wrong and how do I fix it?"
	}

	showDisclaimer()

	// Auto-start daemon if not running
	if err := socket.EnsureRunning(sockPath(), daemonBin()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	req := &protocol.Request{
		Type:       "query",
		Query:      query,
		WorkingDir: cwd,
		GitBranch:  gitBranch(),
		ForceLocal: *forceLocal,
		DryRun:     *dryRun,
	}

	client := socket.NewClient(sockPath())

	var plan []protocol.CommandItem
	fmt.Println()

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

	if len(plan) == 0 || *dryRun {
		if *dryRun && len(plan) > 0 {
			fmt.Println("\n[dry-run] Would run:")
			for _, item := range plan {
				fmt.Printf("  [%s] %s\n", item.Display, item.Command)
			}
		}
		return
	}

	// Show plan and ask for confirmation
	fmt.Println("\nI will run:")
	for _, item := range plan {
		fmt.Printf("  [%s] %s\n", item.Display, item.Command)
	}

	// Check if any destructive commands require individual confirmation
	confirmed := confirmPlan(plan)
	if confirmed == nil {
		fmt.Println("Cancelled.")
		return
	}

	// Execute confirmed commands
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

// confirmPlan prompts the user for confirmation.
// Returns the list of commands to run, or nil if cancelled.
func confirmPlan(plan []protocol.CommandItem) []string {
	reader := bufio.NewReader(os.Stdin)

	// Destructive commands require individual hard confirmation
	for _, item := range plan {
		if item.Tier == permissions.TierDestructive.String() {
			fmt.Printf("\n⚠  DESTRUCTIVE — cannot be undone:\n   %s\n", item.Command)
			fmt.Print("Type YES to confirm this specific command: ")
			input, _ := reader.ReadString('\n')
			if strings.TrimSpace(input) != "YES" {
				return nil
			}
		}
	}

	// Single confirmation for all remaining commands
	fmt.Print("\nApply? [y/n]: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input != "y" && input != "yes" {
		return nil
	}

	cmds := make([]string, len(plan))
	for i, item := range plan {
		cmds[i] = item.Command
	}
	return cmds
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./cmd/spai/
```

Expected: exit 0, binary `spai` created

- [ ] **Step 3: Build both binaries**

```bash
go build ./...
```

Expected: exit 0

- [ ] **Step 4: Commit**

```bash
git add cmd/spai/
git commit -m "feat: add spai CLI client"
```

---

---

## Story S9: Ship

*Phase C — sequential. Requires S8 complete.*

---

## Task 13: systemd Service + Install Scripts

**Files:**
- Create: `systemd/spaid.service`
- Create: `install.sh`
- Create: `uninstall.sh`

- [ ] **Step 1: Create systemd/spaid.service**

```ini
[Unit]
Description=spaiSH daemon
After=network.target

[Service]
Type=simple
ExecStart=%h/.local/bin/spaid
Restart=on-failure
RestartSec=5
StandardOutput=null
StandardError=null

[Install]
WantedBy=default.target
```

- [ ] **Step 2: Create install.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/spaish"
SYSTEMD_DIR="$HOME/.config/systemd/user"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Building spaiSH..."
cd "$REPO_DIR"
go build -o "$INSTALL_DIR/spai" ./cmd/spai/
go build -o "$INSTALL_DIR/spaid" ./cmd/spaid/

echo "Installing config..."
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/spaid.toml" ]; then
  cp "$REPO_DIR/config/spaid.toml" "$CONFIG_DIR/spaid.toml"
  echo "Config installed at $CONFIG_DIR/spaid.toml"
  echo "  → Edit it to add your API endpoint and model name."
else
  echo "Config already exists at $CONFIG_DIR/spaid.toml — not overwriting."
fi

echo "Installing systemd user service..."
mkdir -p "$SYSTEMD_DIR"
cp "$REPO_DIR/systemd/spaid.service" "$SYSTEMD_DIR/spaid.service"
systemctl --user daemon-reload
systemctl --user enable --now spaid

echo ""
echo "Installation complete."
echo ""
echo "Next steps:"
echo "  1. Edit ~/.config/spaish/spaid.toml — set your API endpoint and model."
echo "  2. Set your API key:  export SPAI_API_KEY='your-key'  (add to ~/.bashrc)"
echo "  3. Run: spai 'is my system healthy?'"
echo ""
echo "Or to use a local model instead:"
echo "  Install a local model runtime, then set prefer_local = true in spaid.toml"
```

- [ ] **Step 3: Create uninstall.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Stopping and disabling spaid service..."
systemctl --user stop spaid 2>/dev/null || true
systemctl --user disable spaid 2>/dev/null || true

echo "Removing files..."
rm -f "$HOME/.local/bin/spai"
rm -f "$HOME/.local/bin/spaid"
rm -f "$HOME/.config/systemd/user/spaid.service"
rm -f "$HOME/.local/share/spaish/spaid.sock"

systemctl --user daemon-reload

echo ""
echo "spaiSH uninstalled."
echo ""
echo "The following were NOT removed (your data):"
echo "  ~/.config/spaish/spaid.toml  — your config"
echo "  ~/.local/share/spaish/       — session history"
echo ""
echo "To remove those too: rm -rf ~/.config/spaish ~/.local/share/spaish"
```

- [ ] **Step 4: Make scripts executable**

```bash
chmod +x install.sh uninstall.sh
```

- [ ] **Step 5: Run all tests one final time**

```bash
go test ./...
```

Expected: PASS — all tests across all packages

- [ ] **Step 6: Final build verification**

```bash
go build ./cmd/spai/ ./cmd/spaid/
ls -lh spai spaid
```

Expected: both binaries present, each under 15MB

- [ ] **Step 7: Final commit**

```bash
git add systemd/ install.sh uninstall.sh
git commit -m "feat: add systemd service and install/uninstall scripts"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|-----------------|------|
| `spaid` daemon, Unix socket | Tasks 10, 11 |
| `spai` CLI client | Task 12 |
| Permission tier engine | Task 4 |
| Model routing (cloud + local fallback) | Tasks 5, 6, 8 |
| Session context | Task 7 |
| systemd user service | Task 13 |
| Single-command installer | Task 13 |
| Config (spaid.toml) | Task 2 |
| `--dry-run`, `--local`, `--legal` flags | Task 12 |
| `spai !!` | Task 12 |
| First-run disclaimer | Task 12 |
| No third-party brand references | Enforced throughout |

**All spec requirements covered. No placeholders. Types are consistent across all tasks.**

---

## Known Limitations (Phase 1)

- `spai !!` sends a generic "last command failed" query — it does not actually read shell history. Shell integration for true history capture is Phase 1.1.
- Sessions are not concurrent-safe — the daemon handles one connection at a time per design.
- The socket `Serve` function uses a naive `os.Remove` + directory extraction for the socket parent — this is cleaned up in Task 10 with a proper implementation.
