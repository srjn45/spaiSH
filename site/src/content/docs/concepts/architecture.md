---
title: Architecture
description: How spai's agent loop and provider layer are put together.
---


## Overview

`spai` is a single Go binary — no daemon, no socket, no background service. The
CLI calls the agent engine in-process. The engine is decoupled from any
front-end: it consumes a provider stream and emits a typed response stream, and
the one-shot renderer and the interactive REPL are thin consumers of it.

```
cmd/spai ──▶ internal/app ──▶ internal/agent (tool-calling loop)
                 │                   │
                 │                   ├─▶ internal/ai        (providers: anthropic / openai / ollama)
                 │                   ├─▶ internal/tools     (bash, read/write/edit, glob, grep, list_dir)
                 │                   ├─▶ internal/permissions (parser-based command classifier)
                 │                   └─▶ internal/mcp       (external MCP servers over stdio)
                 ├─▶ internal/session  (file-backed history + auto-compaction)
                 └─▶ internal/llm      (Ollama model management)

internal/cli  ──▶ one-shot renderer + interactive REPL (render the response stream)
```

---

## The agent loop

`internal/agent` runs a native tool-calling loop:

1. Stream a request to the provider; forward text deltas to the front-end.
2. Collect the tool calls the model emitted this turn.
3. For each tool call, classify its risk and (in manual mode) ask the user.
4. Execute approved tools via the registry; append the results to the
   conversation.
5. Repeat until the model stops calling tools or `max_iterations` is reached.

The loop is provider-agnostic and front-end-agnostic — it emits a stream of
typed responses (text, tool activity, output, done, error).

### Execution modes

| Mode | Behaviour |
|---|---|
| **manual** (default) | Write / Elevated / Destructive tool calls require confirmation |
| **auto** | Execute everything without prompting (`--autonomous`) |
| **plan** | Show the proposed tool calls but never execute (`--dry-run`) |

---

## Providers — `internal/ai`

A neutral `Provider` interface (`Stream` for tool-calling, `Complete` for plain
text) with three implementations:

- **anthropic** — native Anthropic Messages API (streaming SSE, `tool_use` /
  `tool_result`, adaptive thinking). Default model `claude-opus-4-8`.
- **openai** — any OpenAI-compatible `/chat/completions` endpoint with streaming
  `tool_calls`.
- **ollama** — local models via `/api/chat` with tool support.

Selection is driven by config, the `--local` flag, and provider availability.

---

## Permission classifier — `internal/permissions`

Every `bash` command is classified before it runs. Classification **parses** the
command with `mvdan.cc/sh` and walks every simple command in pipelines, lists,
and command substitutions, taking the most dangerous tier found. This fixes the
gaps of substring matching: `rm  -rf`, `rm --recursive`, and `a && rm -rf b` are
all caught, while `echo "rm -rf"` is not.

| Tier | Examples | Behaviour |
|---|---|---|
| **Passthrough** | `ls`, `cd`, `pwd`, `echo` | trivial, no gate |
| **Read** | `cat`, `grep`, `git log`, `ps` | execute silently |
| **Write** | `mkdir`, `cp`, file edits, `git commit` | confirm in manual mode |
| **Elevated** | `sudo *`, `systemctl restart`, package installs | explicit prompt |
| **Destructive** | `rm -rf`, `mkfs`, `dd`, `git reset --hard` | hard confirm (type YES) |

Classification is static and offline — it runs even when no model is reachable.

---

## MCP tools — `internal/mcp`

`spai` can connect to external [Model Context Protocol](https://modelcontextprotocol.io)
servers and expose their tools to the model alongside the built-ins. Each
configured server is spawned as a subprocess and spoken to over the **stdio
transport** — newline-delimited JSON-RPC 2.0 on its stdin/stdout. The client is
hand-rolled and minimal: it performs the `initialize` handshake, calls
`tools/list`, and proxies `tools/call`.

Each discovered tool is wrapped as a regular `tools.Tool` and namespaced
`mcp__<server>__<tool>` (mirroring Claude Code) to avoid collisions. The bridged
tools are appended to the built-in registry via `Registry.Add`, so they flow
through the same tool-calling loop and confirmation flow. MCP tools are gated at
**Write** tier — they require confirmation in manual mode.

Loading is **resilient**: a server that fails to start, handshake, or list its
tools is logged and skipped, never aborting startup — `spai` runs the same with
zero or broken MCP servers. Servers are spawned once per session (lazily on the
first agent run) and shut down via `App.Close` when the CLI exits.

Configure servers with one `[[mcp.servers]]` block each:

```toml
[[mcp.servers]]
name = "filesystem"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/some/dir"]
# env = ["KEY=VALUE"]   # optional
```

---

## Sessions — `internal/session`

Sessions are file-backed under `~/.local/share/spaish/sessions/<id>/`. Each turn
appends to the conversation; when a session's estimated token footprint exceeds
the budget, older messages are summarised while recent turns are kept verbatim
(auto-compaction). `SPAI_SESSION_ID` (set per-terminal by the installer) gives
each terminal its own session; `spai resume` reopens the most recent one.

---

## Configuration

`~/.config/spaish/spaid.toml` (written by `spai init`):

```toml
[provider]
kind = "anthropic"                 # "anthropic" | "openai"
endpoint = ""                      # OpenAI-compatible endpoint (kind = "openai")
api_key_env = "ANTHROPIC_API_KEY"  # env var holding the key — never the key itself
model = "claude-opus-4-8"

[local]
ollama_endpoint = "http://localhost:11434"
local_model = "qwen2.5-coder:7b"

[routing]
prefer_local = false               # always use the local model

[agent]
autonomous = false                 # default to auto mode
max_iterations = 25
verbose = false

# External MCP servers (optional, repeatable). Tools are exposed as
# mcp__<name>__<tool> and gated at Write tier.
[[mcp.servers]]
name = "filesystem"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/some/dir"]
# env = ["KEY=VALUE"]
```

---

## Source layout

```
spaish/
├── cmd/spai/                 # entry point, subcommands, init wizard
├── internal/
│   ├── app/                  # in-process orchestration
│   ├── agent/                # tool-calling loop
│   ├── ai/                   # provider interface + anthropic/openai/ollama
│   ├── tools/                # tool registry and implementations
│   ├── mcp/                  # MCP stdio client + tool bridge
│   ├── permissions/          # parser-based command classifier
│   ├── cli/                  # one-shot renderer + REPL
│   ├── session/              # file-backed sessions + auto-compaction
│   ├── llm/                  # Ollama model management
│   └── protocol/             # request/response types
├── config/spaid.toml         # default config template
├── install.sh / uninstall.sh
├── docs/
└── LICENSE
```
