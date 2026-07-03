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
- [x] Expanded tool surface — `git`, `multi_edit`, `code_exec`, vision input,
      `todo_write`, `http_request`
- [x] Test-coverage and permission-classifier hardening pass
- [x] Project context & subagents — `SPAI.md` project-instructions file,
      `/init` scaffold command, `delegate` (subagent) tool with a hard depth-1
      limit, and per-project `.spai/settings.toml` permission overrides

## Next

### Distribution & polish

- [x] Shell completions — hand-written `completions/spai.{bash,zsh,fish}`
      scripts covering `spai`'s subcommands/flags; `install.sh` installs them
      to the right per-shell location
- [x] `man` page — `docs/spai.1`, installed by `install.sh` to
      `~/.local/share/man/man1`
- [x] Homebrew formula — `Formula/spai.rb` pointing at the existing tagged
      release tarballs + checksums from `.github/workflows/release.yml`, with
      README instructions for `brew tap srjn45/spaish` / `brew install spai`
- [ ] `.deb`/`.rpm` packaging via `nfpm` in `release.yml`, bundling the binary
      with the shell completions and man page now that both exist

## Recently completed

- [x] First tagged release (`v0.1.0`) — cut a tag and publish prebuilt binaries
- [x] Per-tool / per-MCP-server permission policy and allowlists
- [x] Streaming MCP tool discovery + `/mcp` status slash command
- [x] Cost/token usage reporting per session
- [x] `@`-completion for files and richer slash-command help
- [x] `git` tool — structured status/diff/log/blame/branch, per-subcommand tiering
- [x] `multi_edit` — regex find & replace across a glob of files
- [x] `code_exec` — ephemeral Python/Node execution (explicitly not sandboxed)
- [x] Vision input — image files passed through to vision-capable providers
- [x] `todo_write` — in-session task list surfaced in the REPL
- [x] `http_request` — generic REST tool (method/headers/body)
- [x] `SPAI.md` — auto-discovered project-instructions file injected into the
      system prompt
- [x] `/init` — REPL slash command that scaffolds a starter `SPAI.md`
- [x] `delegate` — nested subagent tool, depth-limited to 1, sharing the
      parent's confirmation gate
- [x] Per-project `.spai/settings.toml` permission overrides, layered over
      global config

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
