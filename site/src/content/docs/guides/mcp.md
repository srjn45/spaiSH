---
title: MCP servers
description: Connect spai to external Model Context Protocol servers and expose their tools to the model.
---

`spai` can connect to external [MCP](https://modelcontextprotocol.io) servers
and expose their tools to the model. Add one `[[mcp.servers]]` block per server
to `~/.config/spaish/spaid.toml`:

```toml
[[mcp.servers]]
name = "filesystem"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/some/dir"]
# env = ["KEY=VALUE"]   # optional
```

Servers are spawned over stdio; their tools appear to the model as
`mcp__<name>__<tool>` and are gated at Write tier (confirmed in manual mode). A
server that fails to start or handshake is logged and skipped — `spai` still
runs without it.

Use `/mcp` in an interactive session to show MCP server status and their
discovered tools.

## Per-server policy

You can allow or deny an entire server's tools in the `[permissions]` section:

```toml
[permissions.mcp_servers]
filesystem = "allow"
git = "confirm"
```

An explicit `[permissions.tools]` entry for a specific `mcp__*` tool wins over
this default. See [Permissions & modes](/spaiSH/guides/permissions/).
