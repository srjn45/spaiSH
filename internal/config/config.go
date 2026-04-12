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
	Agent       AgentConfig       `toml:"agent"`
	Spaish      SpaishConfig      `toml:"spaish"`
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

// APIKey reads the API key from the environment variable named in cfg.Provider.APIKeyEnv.
func (c *Config) APIKey() string {
	if c.Provider.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.Provider.APIKeyEnv)
}
