# Roadmap

`spai` is a Claude-Code-style CLI agent. The roadmap below reflects that focus.
The earlier "AI-native OS" ambitions (FUSE, eBPF, PAM, a Wayland compositor, a
bootable ISO) are **parked** — see [Parked ideas](#parked-ideas) — in favour of
being an excellent terminal AI agent first.

## Shipped

The core agent is built and working:

- [x] Single in-process `spai` binary (no daemon, socket, or systemd)
- [x] Native tool-calling agent loop
- [x] Multi-provider support — Anthropic (native), OpenAI-compatible, Ollama
- [x] Tools — `bash`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `list_dir`
- [x] Parser-based command-safety classification (mvdan.cc/sh) with permission tiers
- [x] Execution modes — manual / auto / plan
- [x] Polished one-shot renderer — spinner, tool display, tier confirmations
- [x] Rich inline REPL — history, slash commands, `@file`, Ctrl+C interrupt
- [x] `spai init` onboarding wizard with live connection test
- [x] File-backed sessions with token-aware auto-compaction; `spai resume`
- [x] `spai !!`, stdin piping, `--local` / `--autonomous` / `--dry-run`
- [x] Streamed markdown rendering (glamour) in the renderer
- [x] Shift-Tab mode cycling and Esc-to-interrupt key handling
- [x] Prebuilt release binaries — tagged cross-compiled releases + CI
- [x] Live `/model` provider/model switching inside the REPL
- [x] Diff preview for `edit_file`/`write_file` before confirmation
- [x] MCP tool integration — connect external MCP servers, bridge their tools
- [x] More tools — `web_fetch`, `apply_patch` (structured patch/apply)

## Next

### Expand tool surface

- [ ] `git` tool — structured `status`/`diff`/`log`/`blame`/`branch` subcommands,
      permission-tiered independently of raw `bash` (read-only git ops auto-run,
      mutating ones like `commit`/`push` stay gated)
- [ ] `multi_edit` — regex-based find & replace across a glob of files in one
      call, with the existing diff-preview confirmation before writing
- [ ] `code_exec` — sandboxed, ephemeral code execution (Python/Node) as a tool
      distinct from `bash`, with its own timeout/resource limits and tier
- [ ] Vision input — let the agent read image files (screenshots, diagrams) and
      pass them through to vision-capable providers (Anthropic native first)
- [ ] `todo` tool — an agent-visible task list for self-tracking progress on
      multi-step work, surfaced in the REPL renderer
- [ ] `http_request` — a generic REST tool (method/headers/body/auth) to
      complement the read-only `web_fetch`

## Recently completed

- [x] First tagged release (`v0.1.0`) — cut a tag and publish prebuilt binaries
- [x] Per-tool / per-MCP-server permission policy and allowlists
- [x] Streaming MCP tool discovery + `/mcp` status slash command
- [x] Cost/token usage reporting per session
- [x] `@`-completion for files and richer slash-command help

## Parked ideas

Genuinely interesting, but out of scope for now. The most promising — ambient
post-error help and repeated-pattern → alias suggestions — can return later as an
optional shell hook that shells out to `spai`, without fragile keystroke
sniffing.

- FUSE filesystem (`cat /ai/explain/<path>`)
- eBPF syscall/network observation and anomaly detection
- PAM module for context-aware authentication
- Wayland compositor integration / GUI terminal
- Bootable ISO with `spai` pre-installed

## Versioning

Semantic versioning: `MAJOR.MINOR.PATCH`. The first tagged release of the
refactored agent will be `v0.1.0`.
