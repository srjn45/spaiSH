# AI-Native OS — Architecture & Vision

> *A ground-up rethinking of the operating system where AI is not a layer on top — it is the interface itself.*

---

## 1. The Core Idea

Modern operating systems were designed for humans to issue precise, syntactic commands to a machine. The AI-native OS inverts this: the machine interprets **intent**, and translates it into system operations autonomously.

The terminal is not replaced by a chatbot. It is replaced by an **AI runtime** that happens to speak bash, Python, and every other system language underneath — but presents a single, natural interface to the user.

This is not a wrapper. This is a new class of operating system.

---

## 2. The Vision — Universal AI Runtime

### The Java Analogy

Java introduced "write once, run anywhere" via the JVM — an abstraction layer that made the underlying OS irrelevant to the developer. 

The AI-native OS proposes the same for AI-powered applications:

- Developers ship **intent-based app manifests**, not platform-specific code
- Any AI runtime (Claude, GPT, Gemini, local LLM) can interpret and execute them
- Users run apps by asking their AI — no installation wizards, no dependency hell

```
AppManifest {
  name: "Budget Tracker"
  intent: "Track monthly expenses and visualise insights"
  capabilities_required: [filesystem, network]
  actions: [read_csv, compute_totals, render_chart]
  ui_hint: "cli" | "browser"
}
```

The AI reads the manifest, determines *how* to execute it on the current environment, and runs it. The manifest is the program.

### Why Now

- Claude Code already has filesystem, subprocess, and network access
- Local models (Llama 3.3, Qwen 2.5) are now capable enough for real system tasks
- The gap between "AI assistant" and "AI that runs your computer" is closing fast
- No one has standardised the layer *above* the models — that is the opportunity

---

## 3. Starting Point — ClaudeOS

Before the universal runtime, there is a concrete v1: **Ubuntu + Claude Code as the default shell**.

### What This Means

- The terminal opens into a Claude Code session, not bash
- Users express intent in natural language or commands — Claude interprets both
- Bash, Python, and system tools still exist — Claude calls them underneath
- The AI is the interface; the OS is the substrate

### What Changes for the User

```
# Old way
$ sudo systemctl restart nginx
$ journalctl -u nginx -n 50
$ vim /etc/nginx/nginx.conf

# New way
"nginx keeps failing, fix it"
→ Claude reads logs, identifies config error, shows proposed fix, applies with confirmation
```

The underlying operations are identical. The cognitive load is not.

---

## 4. Deep Integration — Beyond the Shell

The shell is just the beginning. Claude can be integrated at every layer of the Linux stack.

```
┌─────────────────────────────────────────────┐
│  Wayland Compositor                         │
│  (Claude sees all windows, interprets UI)   │
├─────────────────────────────────────────────┤
│  FUSE Filesystem  (/ai/* paths)             │
├─────────────────────────────────────────────┤
│  PAM Module  (auth & sudo decisions)        │
├─────────────────────────────────────────────┤
│  Systemd Integration  (process management)  │
├─────────────────────────────────────────────┤
│  Claude Shell  (replaces bash)              │
├─────────────────────────────────────────────┤
│  eBPF Probes  (syscall observation)         │
├─────────────────────────────────────────────┤
│  /dev/claude  (kernel interface)            │
├─────────────────────────────────────────────┤
│               Linux Kernel                  │
└─────────────────────────────────────────────┘
```

### Layer Breakdown

#### Shell Layer
The entry point. Claude Code replaces bash as the default login shell. Natural language and commands both work. Bash is still available as a sub-process.

#### Systemd Level
Claude runs as a privileged systemd service that boots before the user session. It becomes the process supervisor — starting, stopping, and healing services based on intent rather than unit files.

```
"my web server keeps crashing"
→ Claude reads journald, identifies the issue, patches config, restarts service
→ Autonomously, before you even open a terminal
```

#### PAM (Pluggable Authentication Modules)
PAM controls sudo, login, and SSH authentication in Linux. A custom PAM module makes Claude part of the auth decision:

```
User requests sudo
→ PAM calls claused: who is asking, what command, what context
→ Claude evaluates risk, decides: allow / deny / prompt user
→ Richer, context-aware permissions — not just a password
```

#### eBPF — The Kernel Boundary
eBPF lets you run sandboxed programs inside the kernel, watching every syscall, network packet, and file access in real time with near-zero overhead.

Claude attached to eBPF probes means:
- Unknown binary executes → Claude sandboxes and explains it
- Process makes unusual outbound call → Claude intercepts and evaluates
- File in `/etc` modified unexpectedly → Claude flags and logs intent

This is AI woven into the execution fabric, not watching from outside.

#### FUSE Filesystem
A custom FUSE mount makes AI a universal interface — accessible from any language, any tool, any process:

```
/ai/explain/var/log/syslog        → Claude explains that log
/ai/fix/etc/nginx/nginx.conf      → Claude returns corrected config
/ai/summarise/home/user/docs/     → Claude summarises a directory
```

No API calls. No SDK. Just file reads. Universally compatible with every program ever written.

#### Wayland Compositor
At the display server level, Claude can see everything on screen — not just the terminal. Capabilities include:
- Proactively explaining error dialogs system-wide
- Context-aware help based on the active application
- Intercepting and explaining permission prompts visually

#### /dev/claude — Kernel Interface
An optional kernel module exposes a `/dev/claude` device. Any process can write requests to it and read responses. AI becomes a hardware-like resource — a first-class OS primitive.

---

## 5. The Central Daemon — `claused`

All integration points talk to a single persistent daemon. One brain, many interfaces.

```
                    ┌──────────────┐
  Shell ────────────▶              │
  PAM ─────────────▶   claused    │◀──── Model Router
  eBPF ────────────▶              │           │
  FUSE ────────────▶   (local     │    ┌──────┴──────┐
  Systemd ─────────▶    socket)   │    │             │
  Compositor ──────▶              │  Local        Claude
                    └──────────────┘  Model         API
```

### Responsibilities

- **Context management** — maintains full system state: running processes, recent logs, user patterns, session history
- **Model routing** — decides which model handles each request based on complexity, connectivity, and user tier
- **Permission enforcement** — the single source of truth for what is allowed
- **Session memory** — sudo elevation, recent operations, user preferences persist within a session

### Permission Tiers

| Operation | Behaviour |
|---|---|
| Read (ls, cat, ps) | Execute silently |
| Write / modify | Show plan, confirm once |
| Sudo required | Explicit prompt, session-scoped timeout |
| Destructive (rm -rf, format) | Hard confirm, show exact command |

Claude can *explain* what it's about to do before doing it — which is inherently safer than raw bash.

---

## 6. Local & Cloud Model Strategy

### The Model Stack

The OS ships with **Ollama** as the local inference runtime — a daemon that manages model downloads, runs inference, and exposes an OpenAI-compatible API. Switching between local and cloud is a config change, not a code change.

**Recommended local models:**

| Model | Best For | Min RAM |
|---|---|---|
| Phi-4 | Low-resource hardware, basic tasks | 4GB |
| Llama 3.2 8B | Everyday shell tasks, CPU-only | 8GB |
| Qwen 2.5 Coder | Coding and system tasks | 8GB |
| Llama 3.3 70B (quantized) | Full capability, near-Claude quality | 16GB |

### Smart Routing

Not every task needs a cloud model. `claused` routes by complexity:

```
ls, cd, git status
  → direct bash passthrough (no AI at all)

"explain this log file"
  → local model sufficient

"refactor this entire codebase"
  → Claude API

"rm -rf detected, allow?"
  → local model always (safety must work offline)
```

### Tiered Access Model

```
Free Tier
  → Local models via Ollama
  → No internet required
  → No token limits
  → Full privacy — nothing leaves the device

Pro Tier (Claude subscription)
  → Claude API for latest models
  → Advanced reasoning and coding capability
  → Seamless fallback to local when limit reached

Enterprise
  → Self-hosted Claude (Anthropic offers this)
  → Or any OpenAI-compatible API endpoint
  → Air-gapped deployments supported
```

### Why Local-First Matters

- **Privacy** — all data stays on device by default, a genuine differentiator
- **Reliability** — works offline, on planes, in air-gapped environments
- **Safety** — permission and auth decisions never depend on API availability
- **Cost** — free tier is truly free, not rate-limited free

---

## 7. Build Phases

### Phase 1 — Proof of Concept (weeks)
- Shell wrapper script that invokes Claude Code
- Sudo escalation prompt with session memory
- Confirmation step before any write/destructive operation
- Runs on stock Ubuntu, no OS modifications needed

### Phase 2 — Custom Distro (months)
- Ubuntu base, replace default shell in `/etc/passwd`
- Pre-install Ollama + default local model
- `claused` daemon running as a systemd service
- Ship as bootable ISO

### Phase 3 — Deep Integration (months)
- PAM module for Claude-aware authentication
- FUSE filesystem (`/ai/*` paths)
- eBPF probes for syscall observation
- Smart passthrough for direct commands
- Context window management for long sessions

### Phase 4 — Full Stack (long term)
- Wayland compositor integration
- GUI terminal emulator showing Claude's reasoning
- `/dev/claude` kernel interface
- Universal app manifest format
- Multi-model routing and provider abstraction

---

## 8. Hard Problems to Solve

**Latency** — LLM round-trips are 1–3 seconds. Direct commands (`ls`, `cd`) must bypass AI entirely. Passthrough detection is critical.

**Non-determinism** — Unlike bash, the same input may produce different commands. The confirmation step before execution is non-negotiable.

**Context window in long sessions** — A full workday of terminal usage exceeds any context window. Aggressive summarisation and session checkpointing needed.

**Security** — Prompt injection is a real attack surface. A malicious file could contain instructions that hijack `claused`. Sandboxing and input sanitisation are essential.

**Offline model quality** — Local models are good, not great. The free tier experience will be noticeably weaker than Pro for complex tasks. Honest expectation-setting matters.

---

## 9. Why This Is Different

| | Traditional OS | AI Assistant bolted on | AI-Native OS |
|---|---|---|---|
| Interface | Commands | Commands + chat | Intent |
| AI role | None | Tool | Runtime |
| Offline | Full | Partial | Full (local models) |
| Permissions | Password | Password | Context-aware |
| System visibility | None | Limited | Full (eBPF, FUSE) |
| App model | Install binaries | N/A | Intent manifests |

---

## 10. The Bigger Picture

This starts as an OS. It becomes a platform.

Once `claused` is a standardised daemon with a well-defined API, third parties can build on it. The FUSE filesystem and `/dev/claude` interface mean **any existing Linux software gets AI capabilities without modification**.

The long-term vision — apps shipped as intent manifests that any AI runtime can execute — becomes achievable once the runtime layer is proven and trusted. ClaudeOS is the proof of concept for a new computing paradigm.

The JVM took years to become the platform developers trusted. This starts the same way: one distro, one daemon, one shell.

---

*Document generated from architecture discussion — April 2026*
