---
title: Roadmap
description: What's built and what's next for spai.
---


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
- [x] Distribution & polish — shell completions (`completions/spai.{bash,zsh,fish}`),
      a `man` page (`docs/spai.1`), a Homebrew formula (`Formula/spai.rb`), and
      `.deb`/`.rpm` packaging via `nfpm` in `release.yml`
- [x] Prompt caching & performance — Anthropic `cache_control: ephemeral`
      breakpoints (system prompt, tools, conversation history), real
      input/output/cache-read/cache-creation token counts surfaced in `/cost`
      (replacing the chars/4 estimate), and a per-`Agent`-instance cache for
      the `SPAI.md` project-context disk lookup
- [x] Reliability & test coverage — `handleSlash` dispatch and the REPL print
      helpers in `internal/cli/slash.go` (46.7% → 59.3% package coverage),
      and provider streaming edge cases + shared helpers in `internal/ai`
      (74.6% → 89.4% package coverage)
- [x] REPL/UX polish — a `/sessions` REPL command listing recent sessions
      (pinned/current/shell markers, message count, relative age) without
      leaving the interactive session, and a `capOutputLines` cap (40 lines)
      on long `output`-type tool responses in the TTY renderer

## Next

The seeded backlog is shipped; these are the next directions, grouped
by leverage. Tier 1 is high-leverage, on-theme, and mostly
self-contained; Tier 2 adds meaningful features; Tier 3 is polish.

### Tier 1 — high leverage, mostly self-contained

_All Tier 1 items shipped — see Recently completed._

### Tier 2 — meaningful features

- [ ] Background / long-running `bash` — a `run_in_background` option, a
      `/jobs` view, and streamed output. Files: `internal/tools/bash.go`.
- [ ] Named specialized subagents — today `delegate` is a single
      depth-1 generic; add named agent profiles (e.g. `reviewer`,
      `tester`) with their own system prompts and tool allowlists.
      Files: `internal/agent/subagent.go`.

### Tier 3 — polish

- [ ] Pre/post-tool-use hooks — e.g. auto-`gofmt` after edits, or
      block configured command patterns.
- [ ] Model routing — a cheap model for classification/safety, an
      expensive one for reasoning.
- [ ] REPL polish — multiline input, fuzzy `@file` completion, and
      syntax-highlighted diffs.
- [ ] Session memory — auto-learned facts persisted across sessions,
      beyond the static `SPAI.md`.

## Recently completed

- [x] `gh` / PR integration tool — a first-class `gh` tool wrapping the GitHub
      CLI for PR lifecycle (create, view, list, merge, close, comment, checkout)
      and convenience branch/push wrappers, with per-subcommand permission
      tiering: read-only queries (`pr-view`, `pr-list`, `pr-status`) at
      TierRead; local mutations (`pr-checkout`, `create-branch`) at TierWrite;
      outward-facing operations (`push`, `pr-create`, `pr-comment`, `pr-merge`,
      `pr-close`) at TierElevated. Bash classifier extended with a `classifyGH`
      branch covering `gh pr`, `gh run`, `gh repo`, `gh issue`, and `gh release`.
- [x] OpenAI `reasoning_effort` passthrough — a new `reasoning_effort` config
      key in `[provider]` (spaid.toml) passes the effort level ("low",
      "medium", "high") to OpenAI-compatible reasoning models (o1, o3,
      o4-mini, …). An empty value (the default) omits the field entirely,
      keeping all existing non-reasoning-model behaviour unchanged.
- [x] Optional execution sandbox — an opt-in, default-OFF Linux sandbox
      (native Landlock + seccomp, or `bwrap` when present) restricting
      filesystem and network access for `code_exec` and untrusted `bash`,
      behind build tags with a compiling no-op fallback on non-Linux.
      Layered under the permission gate as defense-in-depth — it never
      replaces a confirmation. Tunable via the optional `[sandbox]`
      config section.
- [x] Checkpoint & `/undo` for file edits — snapshot each file before
      `write_file`/`edit_file`/`apply_patch`/`multi_edit` mutates it
      (originals under `.spai/checkpoints/<session>/`), with `/undo`
      and `/redo` slash commands; create/edit/delete round-trip
- [x] Custom slash commands / prompt templates — discover
      `.spai/commands/*.md` and expand them into prompts with
      `$ARGUMENTS` / `$1` substitution, composing with `SPAI.md` discovery
- [x] Provider retry / backoff / rate-limit handling — a shared
      `http.RoundTripper` wrapper retries 429 / transient 5xx with
      exponential backoff and jitter, honouring `Retry-After`
      (delta-seconds and HTTP-date), bounded attempts, and
      context-cancellation, across all three providers. Tunable via the
      optional `[retry]` config section.
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
- [x] Shell completions — `completions/spai.{bash,zsh,fish}`, installed by
      `install.sh`
- [x] `man` page — `docs/spai.1`, installed by `install.sh`
- [x] Homebrew formula — `Formula/spai.rb` + `brew tap`/`install` instructions
- [x] `.deb`/`.rpm` packaging via `nfpm`, wired into `release.yml`
- [x] Anthropic prompt caching — `cache_control: ephemeral` breakpoints on the
      system prompt, tool definitions, and conversation history
- [x] Real token usage & cache-hit reporting in `/cost`, replacing the
      chars/4 estimate
- [x] Per-session `SPAI.md` project-context caching on the `Agent`, avoiding
      a disk re-read on every turn
- [x] REPL slash-command test coverage — `handleSlash` dispatch and print
      helpers in `internal/cli/slash.go`
- [x] Provider streaming edge cases and shared-helper test coverage in
      `internal/ai` (Anthropic/OpenAI/Ollama)
- [x] `/sessions` REPL command — list recent sessions without leaving the
      interactive session, reusing `session.ListSessions()`/`ReadPinned()`
- [x] Terminal-friendly long tool-output display — `capOutputLines` line cap
      in the REPL renderer, separate from the tool layer's byte-level cap

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
