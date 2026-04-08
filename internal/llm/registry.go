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
	"bitnet": {
		Name:        "bitnet",
		Description: "Microsoft BitNet — 1-bit quantized LLMs, extreme CPU efficiency, no GPU required",
		Endpoint:    "http://localhost:8080",
		DetectCmd:   "test -d ~/.local/share/spaios/bitnet",
		VersionCmd:  "cd ~/.local/share/spaios/bitnet && git rev-parse --short HEAD",
		StartCmd:    "cd ~/.local/share/spaios/bitnet && python run_inference.py -m models/BitNet-b1.58-2B-4T/ggml-model-i2_s.gguf --server --port 8080",
		InstallCmds: bitnetInstallCmds(),
		InstallTier: permissions.TierElevated,
	},
}

// bitnetInstallCmds returns the platform-appropriate install commands for BitNet.
func bitnetInstallCmds() []string {
	dir := "~/.local/share/spaios/bitnet"
	switch runtime.GOOS {
	case "linux", "darwin":
		return []string{
			fmt.Sprintf("git clone --recursive https://github.com/microsoft/BitNet.git %s", dir),
			fmt.Sprintf("pip install -r %s/requirements.txt", dir),
			fmt.Sprintf("cd %s && python setup_env.py -md models/BitNet-b1.58-2B-4T -q i2_s", dir),
		}
	default:
		return []string{fmt.Sprintf(
			"echo 'Automatic install not supported on %s. Visit https://github.com/microsoft/BitNet to install.'",
			runtime.GOOS,
		)}
	}
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
		return Runtime{}, fmt.Errorf("unknown runtime %q — supported: ollama, bitnet", name)
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
