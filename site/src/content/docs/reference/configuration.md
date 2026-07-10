---
title: Configuration
description: The spaid.toml configuration file — provider, local, routing, permissions, agent, and MCP sections.
---

`spai init` writes your configuration to `~/.config/spaish/spaid.toml`. You can
also edit it by hand. An empty `[permissions]` section reproduces the default
behavior exactly.

## `[provider]` — remote model

```toml
[provider]
# Remote provider kind: "anthropic" (native) or "openai" (OpenAI-compatible).
kind = "anthropic"
# Endpoint — only needed for kind = "openai" (include /v1).
endpoint = ""
# Environment variable name that holds your API key (never put the key here).
api_key_env = "ANTHROPIC_API_KEY"
# Model name.
model = "claude-opus-4-8"
```

## `[local]` — Ollama

```toml
[local]
# Ollama endpoint (used with prefer_local = true or `spai --local`).
ollama_endpoint = "http://localhost:11434"
# Local model to use via Ollama.
local_model = "qwen2.5-coder:7b"
```

## `[routing]`

```toml
[routing]
# Set to true to always use the local model (full privacy mode).
prefer_local = false
```

## `[permissions]`

The configurable policy layer sits in front of the built-in tier gate. Each
policy value is one of:

- `allow` — run without confirmation, even in manual mode
- `confirm` — keep the default tier-based behavior
- `deny` — never execute, in any mode (auto included)

```toml
[permissions]
# Seconds before an elevated (sudo) approval expires and must be re-confirmed.
sudo_session_timeout = 300

# Bash command prefixes that bypass confirmation when the classified command
# matches on a word boundary (e.g. "git status" matches "git status -s").
# allow_commands = ["git status", "git diff", "go test", "go build"]

# Per-tool policy: tool name -> allow | confirm | deny.
# [permissions.tools]
# read_file = "allow"
# write_file = "confirm"
# bash = "confirm"

# Per-MCP-server policy: server name -> allow | confirm | deny.
# [permissions.mcp_servers]
# filesystem = "allow"
# git = "confirm"
```

Resolution precedence, highest first: per-tool entry → per-MCP-server entry →
bash `allow_commands` prefix → default tier-based behavior. See
[Permissions & modes](/spaiSH/guides/permissions/).

## `[agent]`

```toml
[agent]
# Default to auto mode (run tool calls without confirmation).
autonomous = false
# Maximum tool-calling iterations per request.
max_iterations = 25
# Verbose tool output.
verbose = false
```

## `[[mcp.servers]]`

Declare one block per external MCP server. Tools are exposed as
`mcp__<name>__<tool>` and treated as Write-tier.

```toml
[[mcp.servers]]
name = "filesystem"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/some/dir"]
# env = ["KEY=VALUE"]   # optional extra environment variables

[[mcp.servers]]
name = "git"
command = "uvx"
args = ["mcp-server-git", "--repository", "/path/to/repo"]
```

See [MCP servers](/spaiSH/guides/mcp/) for details.
