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
| `/model` | show the active provider and model |
| `/tools` | list available tools |
| `/clear` | wipe the conversation context |
| `/compact` | summarise and compact the session |
| `/history` | print the session history |
| `/help`, `/quit` | help, exit |

Reference a file with `@path` to include its contents. `Ctrl+C` cancels the
current turn; `Ctrl+D` exits.

### Modes

- **manual** (default) — write/elevated/destructive actions ask first.
- **auto** — run everything without prompting (`--autonomous` does the same one-shot).
- **plan** — propose the tool calls but never execute (`--dry-run`).

---

## How it works

`spai` runs a native tool-calling loop: the model streams text and tool calls,
each tool call is classified for risk and gated, executed, and the result fed
back until the task is done. Tools: `bash`, `read_file`, `write_file`,
`edit_file`, `glob`, `grep`, `list_dir`.

Command safety is decided by **parsing** each shell command (not substring
matching), so `rm -rf`, `rm --recursive`, and `a && rm -rf b` are all caught,
while `echo "rm -rf"` is not.

Sessions are file-backed and auto-compact when they grow large.

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
