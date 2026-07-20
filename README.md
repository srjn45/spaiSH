# spai

A Claude-Code-style AI agent for your terminal. Ask in plain language; `spai`
reads files, runs commands, and edits code on your behalf — with a permission
gate in front of anything that changes your system.

Works with **remote models** (Anthropic Claude, or any OpenAI-compatible API)
and **local models** (Ollama). One small Go binary, no daemon.

```bash
$ spai "nginx keeps returning 502, find and fix it"
  ▶ read_file /var/log/nginx/error.log
  ▶ read_file /etc/nginx/nginx.conf
  Found an upstream timeout: proxy_read_timeout is 30s but the backend needs ~90s.

  ▶ [Write] edit /etc/nginx/nginx.conf
    proxy_read_timeout 30s  →  proxy_read_timeout 90s
  Allow? [y/N]:
```

---

## Install

### Homebrew (macOS)

```bash
brew tap srjn45/spaish https://github.com/srjn45/spaiSH
brew install spai
```

### Linux packages (.deb / .rpm)

Download the `.deb` or `.rpm` for your architecture from the latest
[GitHub Release](https://github.com/srjn45/spaiSH/releases), then install:

```bash
# Debian / Ubuntu
sudo dpkg -i spai_*.deb

# Fedora / RHEL / openSUSE
sudo rpm -i spai_*.rpm
```

Packages ship the `spai` binary at `/usr/bin/spai` along with shell completions
and the man page.

### Script

```bash
git clone https://github.com/srjn45/spaiSH && cd spaiSH
./install.sh
```

`install.sh` downloads a **prebuilt binary** for your OS/arch from the latest
[GitHub Release](https://github.com/srjn45/spaiSH/releases) when one is
available, and otherwise **builds from source** (requires Go 1.25+). Force a
source build with `SPAI_FROM_SOURCE=1 ./install.sh`. Override the install
location with `INSTALL_DIR=/path ./install.sh`.

Prebuilt binaries cover linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64.
Each release also ships a `*_checksums.txt` (sha256) for verification.

Installs a single `spai` binary to `~/.local/bin`. No root, no systemd, no
background service.

Then configure a provider:

```bash
spai init
```

The wizard lets you pick Anthropic, an OpenAI-compatible endpoint, or Ollama,
sets the model, and runs a live connection test.

For an API provider you reference an environment variable rather than storing the
key in a file:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

---

## Usage

```bash
spai "summarise what changed in the last 5 commits"   # one-shot
spai                                                  # interactive session
spai resume                                           # reopen the last session
git log | spai "anything worrying here?"              # pipe stdin in
spai !!                                                # explain the last failed command
```

### Interactive session

Run `spai` with no arguments for a multi-turn session. Slash commands:

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
| `/undo`, `/redo` | revert / re-apply the agent's last file mutation (create, edit, or delete) |
| `/jobs [id]` | list background bash jobs (id, status, command); `/jobs <id>` to inspect output |
| `/help`, `/quit` | help (`/help <command>` for detail), exit |

Drop a Markdown file in `.spai/commands/` to add your own slash command: `.spai/commands/review.md` becomes `/review`. The file body is a prompt template — `$ARGUMENTS` expands to everything after the command and `$1`, `$2`, … to individual arguments — that runs as a normal agent turn (inheriting `SPAI.md` context and the usual permission gating).

Reference a file with `@path` to include its contents — press `Tab` after `@`
to complete file and directory names. `Shift-Tab` cycles the execution mode.
`Ctrl+C` (or `Esc`) cancels the current turn; `Ctrl+D` exits.

### Modes

- **manual** (default) — write/elevated/destructive actions ask first.
- **auto** — run everything without prompting (`--autonomous` does the same one-shot).
- **plan** — propose the tool calls but never execute (`--dry-run`).

---

## How it works

`spai` runs a native tool-calling loop: the model streams text and tool calls,
each tool call is classified for risk and gated, executed, and the result fed
back until the task is done.

### Tools

| Tool | What it does | Tier |
|---|---|---|
| `bash` | Run shell commands | Read → Destructive |
| `read_file` | Read a file | Read |
| `write_file` | Write a file | Write |
| `edit_file` | Edit a file (find & replace) | Write |
| `multi_edit` | Regex find & replace across a glob | Write |
| `apply_patch` | Apply a structured patch | Write |
| `glob` | Find files by pattern | Read |
| `grep` | Search file contents by regex | Read |
| `list_dir` | List directory contents | Read |
| `web_fetch` | Fetch a URL and return its text | Read |
| `web_search` | Keyless DuckDuckGo search | Read |
| `http_request` | Generic REST call (method/headers/body) | Write |
| `git` | Structured git operations with per-subcommand tiering | Read → Destructive |
| `gh` | GitHub CLI — PR lifecycle (create/view/list/merge/close), branch, push | Read → Elevated |
| `code_exec` | Ephemeral Python/Node/Ruby/Go execution | Write |
| `read_image` | Read an image file for vision models | Read |
| `todo_write` | Manage an in-session task list | Write |
| `delegate` | Spawn a named nested subagent (depth-limited to 1) | Write |

Command safety is decided by **parsing** each shell command (not substring
matching), so `rm -rf`, `rm --recursive`, and `a && rm -rf b` are all caught,
while `echo "rm -rf"` is not.

You can layer a **configurable policy** on top of the tier gate in the
`[permissions]` section of `spaid.toml` — allow/confirm/deny per tool or per MCP
server, plus a bash `allow_commands` prefix allowlist (e.g. `git status`) that
bypasses confirmation. See the annotated `config/spaid.toml` for the keys.

The `bash` tool accepts an optional `run_in_background` boolean. When `true`,
the command is started without blocking the current turn and returns a job id
immediately. Output is streamed into an in-memory buffer and the `/jobs` REPL
command shows the status and captured output. The same permission-classification
gate applies to background commands as to foreground ones.

OpenAI-compatible reasoning models (o1, o3, o4-mini, …) support a
`reasoning_effort` knob ("low", "medium", "high") that controls thinking
budget per request. Set it in `[provider]` of `spaid.toml`
(`reasoning_effort = "medium"`); leave it empty (the default) to omit the
field, which is safe for all models including non-reasoning ones.

Transient provider failures (HTTP 429 or 5xx) are retried automatically with
exponential backoff and jitter, honouring a server `Retry-After` header, across
all providers. Tune it in the optional `[retry]` section of `spaid.toml`
(`max_attempts`, `base_delay_ms`, `max_delay_ms`).

As **defense-in-depth under** the permission gate (never a replacement for it),
`code_exec` and untrusted `bash` commands can be run inside an opt-in execution
sandbox that restricts filesystem and network access. It is **off by default**
and enforced on Linux (native Landlock + seccomp, or `bwrap` when present); on
other platforms it is a no-op. Enable and tune it in the optional `[sandbox]`
section of `spaid.toml`:

```toml
[sandbox]
enabled = true                     # master opt-in (default false)
allow_network = false              # keep network open (default false = deny)
allow_paths = ["/extra/writable"]  # extra writable dirs (cwd + code_exec temp always writable)
backend = "auto"                   # "auto" | "bwrap" | "landlock" | "off"
trust_allowlisted_commands = false # exempt [permissions].allow_commands from the sandbox
```

When enabled but the platform cannot enforce it, the command fails closed rather
than running unsandboxed.

The `delegate` tool spawns a nested subagent (depth-limited to 1). Pass an
optional `profile` argument to select a named profile with a focused system
prompt and restricted tool set:

| Profile | System prompt | Tools |
|---------|--------------|-------|
| `reviewer` | Read-only code analyser | `read_file`, `grep`, `glob`, `list_dir`, `web_search`, `web_fetch` |
| `tester` | Test writer and runner | `bash`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `list_dir` |
| `general` | Default (full tool set) | all parent tools |

Add custom profiles in `spaid.toml` under `[[subagent.profiles]]`. User profiles
override builtins of the same name:

```toml
[[subagent.profiles]]
name         = "deployer"
description  = "CI/CD expert."
system_prompt = "You are a deployment expert. Run CI commands and inspect logs."
tools        = ["bash", "read_file", "glob", "grep", "list_dir", "git"]
```

Sessions are file-backed and auto-compact when they grow large.

---

## MCP servers

`spai` can connect to external [MCP](https://modelcontextprotocol.io) servers and
expose their tools to the model. Add one `[[mcp.servers]]` block per server to
`~/.config/spaish/spaid.toml`:

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

---

## Local models

```bash
spai llm list           # available local models
spai llm pull <model>   # download via Ollama
spai llm use <model>    # set the active local model
spai --local "..."      # force the local model for one request
```

---

## Documentation

- [Architecture](docs/architecture.md) — the agent loop and provider layer
- [Roadmap](docs/roadmap.md) — what's built, what's next
- [Legal](docs/legal.md) — license, disclaimer
- [Contributing](docs/contributing.md)

---

## Status

**Experimental. Personal project. Use at your own risk.** You are responsible
for API costs and for any command you approve.

## License

Apache 2.0 — see [LICENSE](LICENSE)
