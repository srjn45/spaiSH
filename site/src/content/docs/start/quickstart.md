---
title: Quickstart
description: Your first one-shot command and interactive session with spai.
---

Once you've [installed spai](/spaiSH/start/install/) and run `spai init`, you're
ready to go.

## One-shot

```bash
spai "summarise what changed in the last 5 commits"   # one-shot
spai                                                  # interactive session
spai resume                                           # reopen the last session
git log | spai "anything worrying here?"              # pipe stdin in
spai !!                                                # explain the last failed command
```

## Interactive session

Run `spai` with no arguments for a multi-turn session. Reference a file with
`@path` to include its contents — press `Tab` after `@` to complete file and
directory names. `Shift-Tab` cycles the execution mode. `Ctrl+C` (or `Esc`)
cancels the current turn; `Ctrl+D` exits.

See [Interactive session](/spaiSH/guides/interactive-session/) for the full list
of slash commands.

## Modes

- **manual** (default) — write/elevated/destructive actions ask first.
- **auto** — run everything without prompting (`--autonomous` does the same one-shot).
- **plan** — propose the tool calls but never execute (`--dry-run`).

More in [Permissions & modes](/spaiSH/guides/permissions/).

## Next

- [Permissions & modes](/spaiSH/guides/permissions/)
- [MCP servers](/spaiSH/guides/mcp/)
- [Local models](/spaiSH/guides/local-models/)
