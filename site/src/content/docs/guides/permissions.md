---
title: Permissions & modes
description: The tiered permission gate, command parsing, execution modes, and the configurable policy layer.
---

`spai` puts a permission gate in front of anything that changes your system. It
combines a **tier-based gate**, a **configurable policy layer**, and three
**execution modes**.

## Execution modes

- **manual** (default) — write/elevated/destructive actions ask first.
- **auto** — run everything without prompting (`--autonomous` does the same one-shot).
- **plan** — propose the tool calls but never execute (`--dry-run`).

Set the mode with `/mode` in a session, cycle it with `Shift-Tab`, or pass
`--autonomous` / `--dry-run` for one-shot runs.

## Tier-based gate

Each tool call is classified by risk:

- **read** — `read_file`, `glob`, `grep`, `list_dir` run freely.
- **write** — `write_file`, `edit_file` are confirmed in manual mode.
- **elevated / destructive** — `sudo` and destructive commands always ask, and
  approval is session-scoped (see `sudo_session_timeout`).

## Command parsing, not string matching

Command safety is decided by **parsing** each shell command, so `rm -rf`,
`rm --recursive`, and `a && rm -rf b` are all caught, while `echo "rm -rf"` is
not.

## Configurable policy layer

You can layer a policy on top of the tier gate in the `[permissions]` section of
`spaid.toml` — allow / confirm / deny per tool or per MCP server, plus a bash
`allow_commands` prefix allowlist (e.g. `git status`) that bypasses
confirmation.

Resolution precedence, highest first:

1. per-tool entry (`[permissions.tools]`)
2. per-MCP-server entry (`[permissions.mcp_servers]`)
3. bash `allow_commands` prefix match
4. default tier-based behavior

See [Configuration](/spaiSH/reference/configuration/) for the full set of keys.
