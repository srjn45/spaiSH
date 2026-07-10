---
title: Interactive session
description: The multi-turn spai REPL — slash commands, file references, and session controls.
---

Run `spai` with no arguments for a multi-turn interactive session.

## Slash commands

| Command | What it does |
|---|---|
| `/mode manual\|auto\|plan` | set how tool calls are gated |
| `/model [sel]` | show providers/models, or switch (`/model ollama`, `/model openai:gpt-4o`) |
| `/tools` | list available tools |
| `/mcp` | show MCP server status and their discovered tools |
| `/cost` | show estimated token usage and cost for the session |
| `/clear` | wipe the conversation context |
| `/compact` | summarise and compact the session |
| `/history` | print the session history |
| `/sessions` | list past sessions |
| `/help`, `/quit` | help (`/help <command>` for detail), exit |

## File references

Reference a file with `@path` to include its contents — press `Tab` after `@`
to complete file and directory names.

## Keys

- `Shift-Tab` — cycle the execution mode.
- `Ctrl+C` (or `Esc`) — cancel the current turn.
- `Ctrl+D` — exit.

## Sessions

Sessions are file-backed and auto-compact when they grow large. Reopen the most
recent one with:

```bash
spai resume
```
