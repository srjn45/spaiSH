# WP3.4 — Cross-Session Learned Memory Design

**Date:** 2026-07-21
**Status:** Draft
**Branch:** `wp3-4-learned-memory`

---

## Problem

Today `spai` injects project context from the static `SPAI.md` file into the
system prompt. That file must be written and maintained by the user; the agent
has no way to persist facts it discovers during a session (e.g. "the build
command is `make gen`", "this project uses table-driven tests", "the reviewer
is Alice"). Those facts must be re-discovered on every new session, wasting
context window space and repeated tool calls.

---

## Goals

1. A lightweight **memory store** under `.spai/` where the agent (or the user)
   can persist short key/value facts across sessions.
2. A **`remember_fact` tool** the agent calls to record a fact.
3. **Automatic injection** of stored facts into the system prompt at session
   start, composing with (not replacing) `SPAI.md`.
4. A **config toggle** (`[memory] enabled = true/false`, default `false`) so
   the feature is opt-in and safe by default.
5. **Size / dedup bounds**: max 200 facts, update-in-place when a key already
   exists.
6. **Backward compatibility**: when disabled or when no facts exist, the system
   prompt is byte-identical to today.

---

## Non-Goals

- Automatic extraction of facts without agent involvement (no passive
  background scanning of conversation history).
- Global user-level memory (all memory is project-scoped under `.spai/`).
- Fine-grained expiry or TTLs per fact (a future extension).
- A separate `/remember` slash command (the `remember_fact` tool is sufficient;
  users can ask the agent to remember something and the agent calls the tool).

---

## Store format and location

**File:** `<projectRoot>/.spai/memory.jsonl`

One JSON object per line (JSONL / newline-delimited JSON):

```jsonl
{"key":"build-command","value":"make gen","learned_at":"2026-07-21T10:00:00Z"}
{"key":"test-runner","value":"go test ./...","learned_at":"2026-07-21T10:01:00Z"}
{"key":"preferred-style","value":"table-driven tests","learned_at":"2026-07-21T10:02:00Z"}
```

**Why JSONL?**  
The `internal/session/` package mirrors this idiom — it uses newline-oriented
plain-text files (`history.md`) for append-friendly writes and JSON for
structured state (`cache.json`). JSONL combines both: structured records and
append-oriented layout. It is also human-readable and easily git-diffable.

**Why `.spai/`?**  
All project-scoped state already lives here: `.spai/checkpoints/`,
`.spai/commands/`, `.spai/settings.toml`. Placing memory in `.spai/` follows
the existing convention, is easy to `.gitignore`, and is found via the same
`projectRoot()` walk already used by `CheckpointStore`.

**Project-root discovery:**  
Reuses the `projectRoot(workingDir string)` helper from
`internal/session/checkpoint.go` (walks up to the nearest `.git` ancestor).

---

## Capture mechanism — `remember_fact` tool

The agent records a fact by calling the built-in `remember_fact` tool:

```json
{"key": "build-command", "value": "make gen"}
```

**Why a tool, not a slash command?**  
Slash commands are REPL-only, user-typed interactions. Tools are what the agent
calls programmatically during its loop. Since the goal is agent auto-learning
(the agent discovers facts and persists them without blocking on user input), a
tool is the natural fit. The user can always ask the agent verbally to remember
something ("remember that the build command is `make gen`") and the agent calls
the tool in its next turn.

**Permission tier: `TierRead`**  
The tool writes only to `.spai/memory.jsonl` — an agent-managed metadata file,
not user source code. This is analogous to `todo_write` (also `TierRead`)
which writes to an in-memory task list: both are agent bookkeeping that should
not interrupt the user with a confirmation prompt on every fact learned.

**Schema:**

```
key   — string, required. Short unique identifier (e.g. "build-command").
value — string, required. The fact text (e.g. "make gen").
```

---

## System-prompt injection

When `[memory] enabled = true` and at least one fact is stored, a
`## Learned context` section is appended to the system prompt:

```
## Learned context
- build-command: make gen
- test-runner: go test ./...
- preferred-style: table-driven tests
```

This section appears **after** the `## Project instructions (SPAI.md)` section,
so SPAI.md always takes precedence as the authoritative project context.
When no facts exist or the feature is disabled, the section is absent and the
system prompt is byte-identical to today.

**Loading:** The learned context is loaded once at the start of each agent run
(inside `app.RunAgent`) and passed to `agent.Config.LearnedContext`. The
per-agent caching model matches the existing `projectContextOnce` pattern for
`SPAI.md`. Facts written during a session via `remember_fact` are stored to
disk immediately but take effect only in the **next** session — this is the
intended cross-session semantics.

---

## Config schema

New `[memory]` section in `spaid.toml` (mirrors the idiom of `[routing]`,
`[sandbox]`, `[retry]` — all optional with safe zero values):

```toml
[memory]
enabled   = true   # opt-in; default false
max_facts = 200    # entries cap; 0 uses the default (200)
```

Config struct additions (`internal/config/config.go`):

```go
// MemoryConfig holds the cross-session learned memory configuration.
// The zero value (absent [memory] section) disables the feature, preserving
// existing behaviour.
type MemoryConfig struct {
    // Enabled opts in to learned memory. Default false (feature off).
    Enabled  bool `toml:"enabled"`
    // MaxFacts caps the number of stored facts; oldest entries are pruned when
    // the limit is exceeded. 0 resolves to the DefaultMaxFacts constant (200).
    MaxFacts int  `toml:"max_facts"`
}
```

Added to `Config`:
```go
Memory MemoryConfig `toml:"memory"`
```

---

## Size and dedup bounds

- **MaxFacts default:** 200. When a new fact would push the count above the
  cap, the oldest entry (first line of the JSONL file) is discarded.
- **Deduplication:** by `key`. When `Append(key, value)` is called with a key
  that already exists, the value and `learned_at` are updated in place and the
  entry order is preserved. No duplicate keys ever appear in the file.
- **Key length:** not capped (practical keys are short: "build-command", etc.).
- **Value length:** not capped at the store level (the system prompt as a whole
  is bounded by the provider's context window; excessive fact values are a user
  concern, not a store concern).

---

## Backward compatibility

| Condition | Behaviour |
|---|---|
| `[memory]` absent from config | `enabled=false`; no tool registered, no injection |
| `enabled = false` | same as absent |
| `enabled = true`, no `.spai/memory.jsonl` | `Load()` returns nil; no injection |
| `enabled = true`, file present | facts loaded, section appended after SPAI.md |
| Config present, file corrupt (bad JSON line) | corrupt lines are skipped with a log warning; rest of store is still loaded |

The `remember_fact` tool is **only registered** when `enabled = true`, so the
model never sees it when the feature is off. This avoids polluting the tool list
with a capability the user hasn't opted into.

---

## Files changed

- `docs/superpowers/specs/2026-07-21-wp3-4-learned-memory-design.md` (this file)
- `internal/session/memory.go` — `Fact`, `MemoryStore`, `NewMemoryStore`, `Load`, `Append`
- `internal/session/memory_test.go` — store unit tests (load/save/dedup/bounds/corrupt)
- `internal/tools/remember.go` — `RememberFact` tool implementation
- `internal/tools/remember_test.go` — tool unit tests
- `internal/tools/tool.go` — `KeyArg` helper for display
- `internal/config/config.go` — `MemoryConfig`, `Config.Memory`
- `internal/config/config_test.go` — parse `[memory]` section
- `internal/agent/agent.go` — `Config.LearnedContext`; inject into `loop()`; classify `remember_fact`
- `internal/agent/classify_memory_internal_test.go` — `remember_fact` classifier test
- `internal/app/app.go` — load learned context, register tool when enabled
- `README.md` — add `remember_fact` to tool table; document `[memory]` config
- `docs/spai.1` — add `remember_fact` tool and `.spai/memory.jsonl` to FILES section
- `docs/roadmap.md` — move "Session memory" from Next/Tier 3 → Recently completed
- `site/src/content/docs/reference/roadmap.md` — same update
