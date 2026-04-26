# Architecture

## Overview

spaiSH consists of two binaries and a configuration file:

| Component | Purpose |
|-----------|---------|
| `spaid` | Background daemon — handles AI requests, manages context, enforces permissions |
| `spai` | CLI client — sends queries to `spaid` via Unix socket, streams responses |
| `spaid.toml` | User configuration — API endpoint, model, routing preferences |

---

## System Layout

```
~/.config/spaish/
└── spaid.toml              # User configuration

~/.local/share/spaish/
├── spaid.sock              # Unix socket (spaid listens here)
├── spaid.log               # Daemon logs
└── session.json            # Rolling session context

~/.local/bin/
├── spaid                   # Daemon binary
└── spai                    # CLI client binary
```

---

## The Daemon — `spaid`

`spaid` runs as a systemd user service. It starts on login and runs silently in the background. It is the single brain behind all spaiSH functionality.

```
                   ┌──────────────────┐
  spai CLI ────────▶                  │
  FUSE (Phase 2) ──▶     spaid        │◀──── Model Router
  PAM (Phase 3) ───▶                  │           │
  eBPF (Phase 3) ──▶  (Unix socket)   │   ┌───────┴───────┐
                   └──────────────────┘ Local          Cloud
                                       Model           API
```

### Responsibilities

- **Request handling** — receives queries from `spai`, processes them, streams responses back
- **Context management** — maintains session state: working directory, recent commands, git context, conversation history
- **Permission classification** — classifies every proposed command before execution
- **Model routing** — decides whether to use local or cloud model based on config and availability
- **Session memory** — persists context within a session; summarizes when context grows large

---

## Permission Tier Engine

Every command `spaid` proposes to run is classified before execution. Classification uses a static rule set — fast, deterministic, and offline. The AI is only involved in ambiguous edge cases.

| Tier | Examples | Behaviour |
|------|---------|-----------|
| **Read** | `ls`, `cat`, `ps`, `git log`, `df`, `journalctl` | Execute silently |
| **Write** | `mkdir`, file edits, `git commit`, `touch` | Show plan, single confirmation |
| **Elevated** | `sudo *`, `systemctl`, package installs | Explicit prompt, shows exact command |
| **Destructive** | `rm -rf`, `mkfs`, `dd`, `DROP TABLE`, `git reset --hard` | Hard confirm, full command displayed |

Safety decisions never depend on network availability. If the AI provider is unreachable, the permission engine still runs.

---

## Model Routing

```
Request arrives
    ↓
Simple passthrough? (cd, ls, pwd, clear, exit)
    → Yes: run directly, skip AI entirely
    ↓ No
SPAI_API_KEY set and network reachable?
    → Yes: route to configured cloud endpoint
    ↓ No
Ollama running locally?
    → Yes: route to local model
    ↓ No
Error: "No AI provider available."
```

---

## Configuration

`~/.config/spaish/spaid.toml`:

```toml
[provider]
endpoint = "https://api.example.com/v1"   # any OpenAI-compatible endpoint
api_key_env = "SPAI_API_KEY"              # reads from environment, never stored here
model = "your-model-name"                 # set to whichever model you use

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder"

[routing]
# Commands that bypass AI entirely
passthrough_commands = ["cd", "ls", "pwd", "clear", "exit", "history"]
# Set true to always use local model (privacy mode)
prefer_local = false

[permissions]
# Seconds before elevated permission expires and must be re-confirmed
sudo_session_timeout = 300
```

---

## Phase Roadmap

### Phase 1 — Core Daemon + Shell Integration (current)

- `spaid` daemon with Unix socket
- `spai` CLI client
- Permission tier engine
- Model routing (cloud + local)
- Session context management
- systemd user service
- Single-command installer

### Phase 2 — FUSE Filesystem

A FUSE mount at `/ai/*` makes the daemon accessible from any process without an SDK:

```bash
cat /ai/explain/var/log/syslog        # explains the log
cat /ai/fix/etc/nginx/nginx.conf      # returns corrected config
cat /ai/summarise/home/user/docs/     # summarises a directory
```

Any program that can read a file gets AI capabilities.

### Phase 3 — Deep System Integration

- **PAM module** — context-aware authentication decisions
- **eBPF probes** — real-time syscall observation for behavioral security monitoring
- **Systemd integration** — `spaid` as a process supervisor

### Phase 4 — Full Stack

- Wayland compositor integration
- GUI terminal with reasoning display
- Intent manifest format for AI-native applications
- Multi-provider abstraction layer

---

## Source Layout

```
spaish/
├── cmd/
│   ├── spai/               # CLI client
│   └── spaid/              # Daemon
├── internal/
│   ├── router/             # Model routing logic
│   ├── permissions/        # Permission tier classification
│   ├── context/            # Session context management
│   └── socket/             # Unix socket client/server
├── config/
│   └── spaid.toml          # Default config template
├── install.sh              # Installer script
├── docs/                   # Documentation
└── LICENSE
```
