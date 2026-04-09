# spaiSH — AI Shell Design

**Date:** 2026-04-09
**Status:** Approved

---

## Overview

spaiSH is a PTY-based shell wrapper that gives users a full AI-native terminal experience. It wraps the user's existing shell (`$SHELL`) transparently — all existing config, aliases, and history work unchanged. AI assistance is silent by default, activates automatically on errors, and is always available via a `?` prefix or natural language input.

spaiSH is a third binary alongside `spai` and `spaid`. It is an extension of spaiOS, not a replacement — users who want explicit one-off AI commands use `spai`; users who want a full AI shell experience use `spaiSH`. Both use `spaid` as their backend.

---

## Architecture

```
Terminal (user's keyboard/screen)
        │  raw terminal I/O
        ▼
   spaiSH process
        │  intercepts I/O, detects events
        ├─────────────────────────────────▶ spaid (Unix socket)
        │  ShellEvent{command, output,       AI suggestion, streamed
        │  stderr, exit_code, cwd}          back to spaiSH
        ▼
   bash / zsh (PTY slave)
        │  real command execution
        ▼
   OS / filesystem / network
```

spaiSH uses `github.com/creack/pty` to fork the user's shell into a PTY slave. It proxies all I/O in raw mode. spaid gets one new handler (`onShell`) and one new wire type (`ShellEvent`). No other changes to spaid.

### Three operating modes

| Mode | Trigger | Behaviour |
|------|---------|-----------|
| **Pass-through** | Normal commands | All I/O flows transparently to bash/zsh |
| **AI conversation** | Error, `?` prefix, natural language | PTY paused, interactive AI exchange, return to shell on `done` or empty Enter |
| **Pattern notification** | Repeated command sequence | Single dim line below prompt, non-blocking, `y/n` to act |

---

## I/O Interception

### PTY setup
spaiSH forks `$SHELL` (default: `bash`) into a PTY slave using `creack/pty`. It runs in raw terminal mode, forwarding all I/O between the terminal and the shell process. Terminal resize events (`SIGWINCH`) are forwarded to the PTY slave.

### Exit code and CWD capture
spaiSH injects a hook into the spawned shell at startup. The mechanism differs by shell:

**bash** — uses `PROMPT_COMMAND`:
```bash
__spaish_hook() { printf "\x00SPAISH:%d:%s\x00" $? "$PWD"; }
PROMPT_COMMAND="__spaish_hook;${PROMPT_COMMAND}"
```

**zsh** — uses `precmd`:
```zsh
__spaish_hook() { printf "\x00SPAISH:%d:%s\x00" $? "$PWD"; }
precmd_functions+=(__spaish_hook)
```

spaiSH detects the shell name from the resolved `$SHELL` path and injects the appropriate hook. The null-byte-delimited marker is stripped before display. This delivers exit code and CWD to spaiSH after every command without polling.

### Output capture
spaiSH buffers PTY output between hook markers. This gives it the complete output of each command. At the PTY level, stdout and stderr are merged into a single stream — they cannot be separated. Output is trimmed to 8 KB before sending to spaid (tail-trimmed — the last 8 KB is kept, as errors typically appear at the end).

---

## Event Detection

### Natural language detection
Before a line is sent to the shell, spaiSH checks in order:

1. Starts with `?` → AI prompt (strip `?`, send as freeform query)
2. First word found by `exec.LookPath` → shell command, pass through
3. First word is a known shell built-in (`cd`, `export`, `alias`, `source`, `echo`, `exit`, `set`, `unset`, `eval`, `exec`, `read`, `type`, `which`) → shell command, pass through
4. Otherwise → natural language, route to AI

### Pattern detection
spaiSH maintains a ring buffer of the last 50 commands (excluding AI conversation turns). After each command:

- **Alias candidate:** same single command seen 5+ times in the session → suggest alias
- **Script candidate:** same sequence of 3+ consecutive commands seen 3+ times → suggest script
- **Long command:** command longer than 60 chars run 3+ times → suggest alias

Pattern suggestions appear as a single dim line below the prompt after the relevant command completes:

```
  💡 You've run this sequence 3 times. Want a `gship` alias? (y/n)
```

`y` opens AI conversation to create and apply it. `n` or any other key dismisses and suppresses for the rest of the session.

---

## Interactive Conversation Mode

### Error flow

```
$ systemctl restart nginx
Job for nginx.service failed. See 'journalctl -xe' for details.
[exit 1]

 spaiSH  nginx failed to restart. Most likely cause: port 80 is already
         bound by another process. Check: lsof -i :80

 you ▶
```

### Natural language / `?` flow

```
$ ? what's eating my disk
 spaiSH  Run: du -sh /* 2>/dev/null | sort -rh | head -20

 you ▶ just show me /home
 spaiSH  Run: du -sh /home/* 2>/dev/null | sort -rh

 you ▶ run it
[command runs, output appears inline]
 you ▶
[Enter — exits conversation, returns to shell prompt]
$
```

### Conversation controls

| Input | Effect |
|-------|--------|
| Any text | Follow-up question to AI |
| `run it` / `try that` / `yes` | Runs the last suggested command, stays in conversation |
| Empty Enter / `done` | Exits conversation, returns to shell prompt |
| A shell command (detected as executable) | Runs it, output shown, conversation continues with that context |
| Ctrl+C | Exits conversation immediately |

### Dissatisfaction and rethink
If the user's reply contains any of: `wrong`, `no`, `doesn't work`, `not right`, `try again`, `that's not`, `still failing` — spaiSH automatically re-sends the request to spaid with full session history instead of the smart window. The AI response is prefixed with a dim `[reconsidering with full context]` indicator.

---

## Session Management

### Storage
spaiSH uses the existing session infrastructure (`internal/session`). Session ID is `spaish_<pid>`. Each spaiSH instance gets its own isolated session file in `~/.local/share/spaios/sessions/`.

### What is stored per exchange
```
command:    "systemctl restart nginx"
output:     "Job for nginx.service failed..."
exit_code:  1
cwd:        "/home/srajan"
ai_reply:   "nginx failed to restart..."
timestamp:  2026-04-09T03:00:00Z
```

### Smart window
In memory: last 20 command+output pairs + last 10 AI exchanges. This is the default context sent to spaid on every AI call.

### Full history (rethink path)
On dissatisfaction, spaiSH sends the full session history to spaid. Maps onto the existing `rebuild-context` mechanism — same summarisation prompt, same flow.

### Cross-tool visibility
spaiSH sessions are visible via `spai sessions` and `spai history` — same commands, same storage format, same UI. Sessions can be pinned with `spai sessions pin`.

---

## Wire Protocol

### New types in `internal/protocol/protocol.go`

```go
// ShellEvent is the payload for "shell" request type.
// spaiSH sends this when a shell event requires AI input.
type ShellEvent struct {
    Trigger     string // "error" | "prompt" | "pattern" | "rethink"
    Command     string // command that was run (empty for freeform prompts)
    Output      string // merged stdout+stderr from PTY, tail-trimmed to 8KB
    ExitCode    int    // exit code of the command
    CWD         string // working directory at time of event
    Query       string // user's natural language input ("prompt" trigger)
    FullHistory string // populated only on "rethink" trigger
}
```

`Request.Shell *ShellEvent` added to `Request` struct alongside `Agent`, `Session`. `Type: "shell"` added to the types comment.

---

## File Map

### New files

| File | Responsibility |
|------|---------------|
| `cmd/spaish/main.go` | Entry point: parse flags, load config, resolve shell, start PTY event loop |
| `internal/spaish/pty.go` | PTY fork, raw mode I/O proxy, SPAISH marker parsing, SIGWINCH forwarding |
| `internal/spaish/detector.go` | Natural language detection, pattern ring buffer, pattern matching logic |
| `internal/spaish/conversation.go` | Interactive conversation UI, dissatisfaction detection, run-command flow |

### Modified files

| File | Change |
|------|--------|
| `internal/protocol/protocol.go` | Add `ShellEvent` type, `Request.Shell` field |
| `internal/socket/server.go` | Add `ShellHandler` type, 7th param to `Serve` |
| `cmd/spaid/main.go` | Add `onShell` handler |
| `config/spaid.toml` | Add `[spaish]` section |
| `install.sh` | Build and install `spaish` binary |

---

## Configuration

```toml
[spaish]
# Shell to wrap. Defaults to $SHELL environment variable, then bash.
shell = ""
# Exit codes >= this value trigger AI suggestions. 1 = any failure.
error_threshold = 1
# Times a pattern must repeat before a suggestion is shown.
pattern_min_count = 3
# Number of recent command+output pairs kept in the smart context window.
context_window = 20
```

---

## Go Dependency

PTY management uses `github.com/creack/pty` — the standard Go PTY library, actively maintained, used in production by tools like `tmux`, `asciinema`, and `charm`.

---

## Success Criteria

A user installs spaiSH and launches it with `spaish`. Their existing `.bashrc`, aliases, and shell history work identically. When a command fails, a helpful AI suggestion appears automatically. They can ask follow-up questions, ask spaiSH to run suggestions, and return to the shell at any time. Natural language questions work without any prefix. After running the same deploy sequence three times, spaiSH offers to automate it.

`spai` continues to work alongside spaiSH — users can switch between them freely.
