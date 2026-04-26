# spaiSH Phase 1 Design Spec

**Date:** 2026-04-04
**Scope:** Core daemon (`spaid`) + shell integration (`spai`) — Phase 1B

---

## Summary

Build the foundational spaiSH layer: a Go daemon (`spaid`) that listens on a Unix socket, and a CLI client (`spai`) that sends queries to it. The daemon handles model routing, session context, and permission classification. The shell is unchanged — AI is opt-in via the `spai` command.

---

## Naming

| Thing | Name |
|-------|------|
| Project / OS | spaiSH |
| CLI command | `spai` |
| Daemon | `spaid` |
| Config | `spaid.toml` |
| Socket | `spaid.sock` |
| systemd service | `spaid.service` |

No references to third-party brands in any distributed artifact.

---

## Architecture

**Option chosen:** Unix socket daemon + thin CLI client (Option A from brainstorm)

- `spaid` runs as a systemd user service, listens on `~/.local/share/spaish/spaid.sock`
- `spai` is a thin client that connects to the socket, streams the response, exits
- If daemon is not running when `spai` is called, `spai` starts it automatically

```
spai CLI ──── Unix socket ──── spaid ──── Model Router
                                              ├── Cloud API (if SPAI_API_KEY set)
                                              └── Ollama (if running locally)
```

---

## Source Layout

```
spaish/
├── cmd/
│   ├── spai/               # CLI client
│   └── spaid/              # Daemon
├── internal/
│   ├── router/             # Model routing
│   ├── permissions/        # Permission tier classification
│   ├── context/            # Session context management
│   └── socket/             # Unix socket client/server
├── config/
│   └── spaid.toml          # Default config template
├── install.sh
├── uninstall.sh
├── docs/
└── LICENSE
```

---

## Permission Tier Engine

Classification is static and offline. No AI involvement for classification decisions.

| Tier | Examples | Behaviour |
|------|---------|-----------|
| Read | `ls`, `cat`, `ps`, `git log` | Execute silently |
| Write | `mkdir`, file edits, `git commit` | Show plan, confirm once |
| Elevated | `sudo *`, `systemctl`, package installs | Explicit prompt, exact command shown |
| Destructive | `rm -rf`, `mkfs`, `dd` | Hard confirm, full command displayed |

Safety decisions never depend on API availability.

---

## Model Routing Logic

```
Is it a passthrough command? (cd, ls, pwd, clear, exit)
  → Yes: run directly, skip AI
SPAI_API_KEY set and network reachable?
  → Yes: use configured cloud endpoint
Ollama running on localhost?
  → Yes: use local model
  → No: return error with setup instructions
```

---

## Configuration (`spaid.toml`)

```toml
[provider]
endpoint = ""              # any OpenAI-compatible endpoint
api_key_env = "SPAI_API_KEY"
model = ""

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder"

[routing]
passthrough_commands = ["cd", "ls", "pwd", "clear", "exit", "history"]
prefer_local = false

[permissions]
sudo_session_timeout = 300
```

---

## CLI Flags

```
spai <query>              natural language query
spai --dry-run <query>    show plan, never execute
spai --local <query>      force local model
spai --legal              print disclaimer and exit
spai --help               usage
spai !!                   analyse last failed command
```

---

## UX — First Run

One-time disclaimer on first invocation, never shown again:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  spaiSH — experimental personal project
  Not affiliated with any AI provider or Linux distribution.
  You are responsible for your API key usage and costs.
  Run 'spai --legal' for full disclaimer and license.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## Install

```bash
curl -fsSL https://get.spaish.dev/install.sh | bash
```

- No root required
- Installs to `~/.local/bin/`
- Config at `~/.config/spaish/spaid.toml`
- Registers systemd user service

---

## Phase Scope

**Phase 1B (this spec):** daemon + shell + permission tiers
**Phase 1C (next iteration):** FUSE filesystem (`/ai/*` paths)

---

## Success Criteria

A developer can install spaiSH on a fresh Linux machine and use `spai` to diagnose and fix a real system problem within 5 minutes, with no commands executing without their confirmation.

---

## Out of Scope for Phase 1

- PAM module
- eBPF integration
- Wayland compositor
- FUSE filesystem (Phase 1C)
- Intent manifest format
- GUI
- Bootable ISO
