package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Provider    ProviderConfig    `toml:"provider"`
	Local       LocalConfig       `toml:"local"`
	Routing     RoutingConfig     `toml:"routing"`
	Permissions PermissionsConfig `toml:"permissions"`
	Agent       AgentConfig       `toml:"agent"`
	Spaish      SpaishConfig      `toml:"spaish"`
	MCP         MCPConfig         `toml:"mcp"`
	Sandbox     SandboxConfig     `toml:"sandbox"`
	Retry       RetryConfig       `toml:"retry"`
}

// SandboxConfig is the opt-in, default-OFF execution sandbox for the bash and
// code_exec tools. The zero value is disabled, so an absent [sandbox] section
// reproduces exactly the pre-sandbox behavior. It is enforced on Linux (via
// bubblewrap or native Landlock+seccomp) and is a no-op elsewhere. The sandbox
// is defense-in-depth layered under the permission gate; it never replaces
// confirmation.
type SandboxConfig struct {
	// Enabled is the master opt-in. Default false.
	Enabled bool `toml:"enabled"`
	// AllowNetwork keeps network access open when true. Default false (deny).
	AllowNetwork bool `toml:"allow_network"`
	// AllowPaths lists extra writable directories; the working directory and the
	// code_exec throwaway temp dir are always writable in addition to these.
	AllowPaths []string `toml:"allow_paths"`
	// Backend selects the mechanism: "auto" (default) | "bwrap" | "landlock" | "off".
	Backend string `toml:"backend"`
	// TrustAllowlistedCommands, when true, exempts bash commands matched by
	// [permissions].allow_commands from the sandbox. Default false (sandbox all).
	TrustAllowlistedCommands bool `toml:"trust_allowlisted_commands"`
}

// RetryConfig mirrors ai.RetryConfig on the wire, expressing durations as
// integer milliseconds for TOML friendliness. It is a top-level section because
// the policy spans the Anthropic/OpenAI providers under [provider] and Ollama
// under [local]. Zero/absent values resolve to the ai package defaults, so the
// section is optional.
type RetryConfig struct {
	MaxAttempts int `toml:"max_attempts"`  // default 4
	BaseDelayMS int `toml:"base_delay_ms"` // default 500
	MaxDelayMS  int `toml:"max_delay_ms"`  // default 30000
}

// MCPConfig declares external Model Context Protocol servers to connect to. Each
// server is spawned over stdio; its tools are exposed to the model namespaced as
// mcp__<name>__<tool>.
type MCPConfig struct {
	Servers []MCPServerConfig `toml:"servers"`
}

// MCPServerConfig describes a single MCP server launched as a subprocess.
type MCPServerConfig struct {
	Name    string   `toml:"name"`
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
	Env     []string `toml:"env"` // optional "KEY=VALUE" entries
}

type ProviderConfig struct {
	Kind            string `toml:"kind"` // "anthropic" (default) | "openai"
	Endpoint        string `toml:"endpoint"`
	APIKeyEnv       string `toml:"api_key_env"`
	Model           string `toml:"model"`
	ReasoningEffort string `toml:"reasoning_effort"` // "low"|"medium"|"high" or "" (omit field)
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

	// Tools maps a tool name to a policy: "allow" | "confirm" | "deny".
	// "allow" runs it without confirmation; "deny" blocks it in every mode;
	// "confirm" (or an absent entry) keeps the default tier-based behavior.
	Tools map[string]string `toml:"tools"`

	// MCPServers maps an MCP server name (the <server> in mcp__<server>__<tool>)
	// to a policy applied to all of that server's tools. An explicit Tools entry
	// for a specific mcp__* tool wins over the server default.
	MCPServers map[string]string `toml:"mcp_servers"`

	// AllowCommands lists bash command prefixes (e.g. "git status", "go test")
	// that bypass confirmation when the classified bash command matches.
	AllowCommands []string `toml:"allow_commands"`
}

type AgentConfig struct {
	Autonomous    bool `toml:"autonomous"`
	MaxIterations int  `toml:"max_iterations"`
	Verbose       bool `toml:"verbose"`
}

// SpaishConfig holds configuration for the spaiSH shell wrapper.
type SpaishConfig struct {
	Shell           string `toml:"shell"`
	ErrorThreshold  int    `toml:"error_threshold"`
	PatternMinCount int    `toml:"pattern_min_count"`
	ContextWindow   int    `toml:"context_window"`
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

// Save writes cfg to path as TOML, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// APIKey reads the API key from the environment variable named in cfg.Provider.APIKeyEnv.
func (c *Config) APIKey() string {
	if c.Provider.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.Provider.APIKeyEnv)
}
