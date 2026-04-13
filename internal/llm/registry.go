package llm

import (
	"fmt"
	"runtime"

	"spaios/internal/permissions"
)

// Runtime describes a supported local LLM runtime.
type Runtime struct {
	Name              string
	Description       string
	Endpoint          string
	DetectCmd         string           // check if runtime is on PATH
	VersionCmd        string           // get installed version
	StartCmd          string           // start the runtime service
	InstallCmds       []string         // ordered shell commands to install
	InstallStepChecks []string         // one per InstallCmd; shell cmd that exits 0 if the step is already done
	UninstallCmds     []string         // ordered shell commands to remove the runtime
	InstallTier       permissions.Tier // permission tier for install commands
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
		InstallCmds:       ollamaInstallCmds(),
		InstallStepChecks: ollamaInstallChecks(),
		UninstallCmds:     ollamaUninstallCmds(),
		InstallTier:       permissions.TierElevated,
	},
	"bitnet": {
		Name:        "bitnet",
		Description: "Microsoft BitNet — 1-bit quantized LLMs, extreme CPU efficiency, no GPU required",
		Endpoint:    "http://localhost:8080",
		DetectCmd:   "test -d ~/.local/share/spaios/bitnet",
		VersionCmd:  "cd ~/.local/share/spaios/bitnet && git rev-parse --short HEAD",
		StartCmd:    "~/.local/share/spaios/bitnet/build/bin/llama-server -m ~/.local/share/spaios/bitnet/models/BitNet-b1.58-2B-4T/ggml-model-i2_s.gguf --port 8080 --host 127.0.0.1",
		InstallCmds:       bitnetInstallCmds(),
		InstallStepChecks: bitnetInstallChecks(),
		UninstallCmds:     bitnetUninstallCmds(),
		InstallTier:       permissions.TierElevated,
	},
}

// bitnetInstallCmds returns the platform-appropriate install commands for BitNet.
// Steps:
//  1. git clone the repo
//  2. install system build tools (cmake, g++, ninja) needed by setup_env.py
//  3. create a venv and pip-install Python deps (avoids PEP 668 on Debian/Ubuntu 22.04+)
//  4. run setup_env.py — builds the project with cmake and downloads the model
func bitnetInstallCmds() []string {
	dir := "~/.local/share/spaios/bitnet"
	venv := "~/.local/share/spaios/bitnet-venv"
	switch runtime.GOOS {
	case "linux":
		return []string{
			fmt.Sprintf("git clone --recursive https://github.com/microsoft/BitNet.git %s", dir),
			"sudo apt-get install -y cmake build-essential ninja-build clang",
			fmt.Sprintf("python3 -m venv %s && %s/bin/pip install -r %s/requirements.txt", venv, venv, dir),
			fmt.Sprintf("cd %s && %s/bin/python setup_env.py -md models/BitNet-b1.58-2B-4T -q i2_s", dir, venv),
		}
	case "darwin":
		return []string{
			fmt.Sprintf("git clone --recursive https://github.com/microsoft/BitNet.git %s", dir),
			"brew install cmake ninja llvm",
			fmt.Sprintf("python3 -m venv %s && %s/bin/pip install -r %s/requirements.txt", venv, venv, dir),
			fmt.Sprintf("cd %s && %s/bin/python setup_env.py -md models/BitNet-b1.58-2B-4T -q i2_s", dir, venv),
		}
	default:
		return []string{fmt.Sprintf(
			"echo 'Automatic install not supported on %s. Visit https://github.com/microsoft/BitNet to install.'",
			runtime.GOOS,
		)}
	}
}

// bitnetInstallChecks returns per-step detection commands for BitNet.
// Each entry corresponds to the same index in bitnetInstallCmds.
// A check exits 0 if the step is already complete.
func bitnetInstallChecks() []string {
	dir := "~/.local/share/spaios/bitnet"
	venv := "~/.local/share/spaios/bitnet-venv"
	switch runtime.GOOS {
	case "linux", "darwin":
		return []string{
			// Step 1: git clone — .git dir exists?
			fmt.Sprintf("test -d %s/.git", dir),
			// Step 2: system build tools — cmake and clang both on PATH?
			"which cmake >/dev/null 2>&1 && which clang >/dev/null 2>&1",
			// Step 3: venv + pip install — venv pip binary exists?
			fmt.Sprintf("test -f %s/bin/pip", venv),
			// Step 4: setup_env.py — model gguf and llama-server binary both present?
			fmt.Sprintf("test -f %s/build/bin/llama-server && test -f %s/models/BitNet-b1.58-2B-4T/ggml-model-i2_s.gguf", dir, dir),
		}
	default:
		return nil
	}
}

// bitnetUninstallCmds returns commands to fully remove the BitNet install directory.
func bitnetUninstallCmds() []string {
	return []string{"rm -rf ~/.local/share/spaios/bitnet"}
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

// ollamaInstallChecks returns per-step detection commands for Ollama.
func ollamaInstallChecks() []string {
	return []string{"which ollama >/dev/null 2>&1"}
}

// ollamaUninstallCmds returns commands to remove Ollama from the system.
func ollamaUninstallCmds() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{
			"sudo systemctl stop ollama 2>/dev/null || true",
			"sudo systemctl disable ollama 2>/dev/null || true",
			"sudo rm -f /usr/local/bin/ollama",
			"sudo rm -rf /usr/local/lib/ollama",
		}
	case "darwin":
		return []string{"brew uninstall ollama"}
	default:
		return nil
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
