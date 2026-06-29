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
	Kind      string `toml:"kind"` // "anthropic" (default) | "openai"
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
