---
title: What is spai?
description: A Claude-Code-style AI agent for your terminal — ask in plain language, behind a permission gate.
---

**spai** is a Claude-Code-style AI agent for your terminal. Ask in plain
language; `spai` reads files, runs commands, and edits code on your behalf —
with a permission gate in front of anything that changes your system.

It works with **remote models** (Anthropic Claude, or any OpenAI-compatible
API) and **local models** (Ollama). One small Go binary, no daemon.

```bash
$ spai "nginx keeps returning 502, find and fix it"
  ▶ read_file /var/log/nginx/error.log
  ▶ read_file /etc/nginx/nginx.conf
  Found an upstream timeout: proxy_read_timeout is 30s but the backend needs ~90s.

  ▶ [Write] edit /etc/nginx/nginx.conf
    proxy_read_timeout 30s  →  proxy_read_timeout 90s
  Allow? [y/N]:
```

## Why spai

- **Plain language, real actions.** Describe the goal. spai plans, reads,
  runs, and edits to get there — it does not just chat.
- **Permission-gated by default.** Write, elevated, and destructive actions ask
  first. Command safety is decided by *parsing* each shell command, not
  substring matching.
- **Bring your own model.** Anthropic, any OpenAI-compatible endpoint, or a
  fully local model via Ollama — switchable mid-session.
- **One binary.** No daemon, no socket, no background service, no database.

## How it works

`spai` runs a native tool-calling loop: the model streams text and tool calls,
each tool call is classified for risk and gated, executed, and the result fed
back until the task is done. The built-in tools are `bash`, `read_file`,
`write_file`, `edit_file`, `glob`, `grep`, and `list_dir`.

Sessions are file-backed and auto-compact when they grow large.

## Next steps

- [Install & setup](/spaiSH/start/install/) — get the binary and configure a provider.
- [Quickstart](/spaiSH/start/quickstart/) — your first one-shot and interactive session.
- [Permissions & modes](/spaiSH/guides/permissions/) — how the gate and execution modes work.

## Status

**Experimental. Personal project. Use at your own risk.** You are responsible
for API costs and for any command you approve.
