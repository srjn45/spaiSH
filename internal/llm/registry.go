package llm

import (
	"fmt"
	"runtime"

	"spaios/internal/permissions"
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

// SupportedRuntimes is the registry of runtimes spaiOS can manage.
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
