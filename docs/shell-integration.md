# Shell Integration

## The `spai` Command

`spai` is the primary interface to spaiSH. It works from any shell — bash, zsh, fish, or any POSIX-compatible shell.

Your existing shell and workflow are completely unchanged. `spai` is an additional command, not a replacement. Regular shell commands never pass through AI.

```bash
$ ls -la          # normal shell, no AI involved
$ git status      # same
$ spai explain why my build failed   # only this goes through spaid
```

---

## Usage

```bash
spai <natural language query>
```

### Examples

```bash
# System administration
spai nginx keeps returning 502, find out why
spai show me what's using the most disk space
spai which process is listening on port 3000

# File operations
spai find all log files modified in the last 24 hours
spai compress everything in ~/Downloads older than 30 days

# Development
spai why did my last git push fail
spai explain what this Makefile does
spai set up a python virtual environment for this project

# Diagnostics
spai my wifi keeps dropping, check what's wrong
spai why is this machine running slow right now
```

### Flags

```bash
spai --dry-run "clean up old docker images"   # show plan, never execute
spai --local "explain this log file"          # force local model
spai --legal                                  # print disclaimer and exit
spai --help                                   # usage information
spai !!                                       # analyse the last failed command
```

---

## How It Works

1. You run `spai <query>`
2. `spai` connects to the `spaid` daemon via Unix socket
3. `spaid` adds system context (current directory, recent history, git state)
4. `spaid` routes the request to your configured AI provider
5. The AI proposes a plan — one or more commands
6. `spaid` classifies each command by permission tier
7. Commands that require confirmation are shown to you before running
8. You confirm, and the commands execute
9. Output streams back to your terminal
10. You're back at your normal prompt

---

## First Run

On first run, spaiSH shows a one-time disclaimer:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  spaiSH — experimental personal project
  Not affiliated with any AI provider or Linux distribution.
  You are responsible for your API key usage and costs.
  Run 'spai --legal' for full disclaimer and license.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

This is shown once and never again.

---

## Installation

### Requirements

- Linux (any modern distribution)
- systemd (for the user service)
- An AI provider API key **or** Ollama running locally

### Install

```bash
curl -fsSL https://get.spaish.dev/install.sh | bash
```

No root required. The installer:
1. Downloads `spai` and `spaid` binaries to `~/.local/bin/`
2. Creates default config at `~/.config/spaish/spaid.toml`
3. Registers `spaid` as a systemd user service
4. Prints next steps

### Configure your AI provider

Set your API key as an environment variable. Add to `~/.bashrc` or `~/.zshrc`:

```bash
export SPAI_API_KEY="your-key-here"
```

Then edit `~/.config/spaish/spaid.toml` to point at your provider's endpoint and set your model name.

### Using a local model instead

Install Ollama and pull a model:

```bash
# Install Ollama (see ollama.com for instructions)
ollama pull qwen2.5-coder   # recommended for system tasks
```

spaiSH will automatically use Ollama if no API key is set and Ollama is running.

### Uninstall

```bash
curl -fsSL https://get.spaish.dev/uninstall.sh | bash
```

Removes all binaries, config, and the systemd service. Your shell rc file is cleaned up.

---

## Managing the Daemon

```bash
systemctl --user status spaid     # check if running
systemctl --user restart spaid    # restart
systemctl --user stop spaid       # stop
journalctl --user -u spaid -f     # live logs
```
