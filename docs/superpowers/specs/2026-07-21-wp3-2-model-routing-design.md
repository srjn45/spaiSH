# WP3.2 — Task-Based Model Routing Design

**Date:** 2026-07-21
**Status:** Draft
**Branch:** `wp3-2-model-routing`

---

## Problem

Every LLM call in spai today uses the same configured model regardless of task
nature. This wastes money and latency:

- **Cheap tasks** — session compaction/summarisation and the auto-compact
  background job only need a small, fast model. They are pure text tasks with
  no tool use: "summarise this conversation".
- **Reasoning tasks** — the main agent tool-calling loop (every agentic turn
  the user or the agent model drives) deserves the strongest configured model.

Without routing, a user who configures `claude-opus-4-8` for reasoning also
pays Opus rates for short summarisation calls that a `claude-haiku-4-5` could
handle equally well.

---

## Goals

1. A `[routing]` config knob: `model_small` for cheap/text-only calls and
   `model_strong` for reasoning/tool-calling turns.
2. Zero-config default: absent keys mean "use the provider's configured model",
   so all existing behaviour is byte-identical unless the user opts in.
3. No new providers, no new packages — routing is implemented as a thin type
   in `internal/ai/` and wired in `internal/app/`.
4. Full backward compatibility with the `/model` interactive override: the REPL
   switch still works and takes precedence over routing within the session.

---

## Non-Goals

- Per-tool or per-prompt routing (too granular for this WP).
- Routing between different providers (only model overrides within the same
  provider; `Request.Model` is already supported by all three backends).
- Automatic model detection or benchmarking.

---

## Config schema

Extend the existing `[routing]` section in `spaid.toml`:

```toml
[routing]
prefer_local = false          # existing key: prefer the local provider

# Task-based model routing (optional; leave unset to use the provider default).
# model_small  selects a cheap/fast model for summarisation and auto-compact.
# model_strong selects a reasoning-capable model for the agent tool-calling loop.
# When only one is set the other falls through to the provider's configured model.
model_small  = "claude-haiku-4-5"
model_strong = "claude-opus-4-8"
```

Config struct (`internal/config/config.go`) gains two fields in `RoutingConfig`:

```go
type RoutingConfig struct {
    PassthroughCommands []string `toml:"passthrough_commands"`
    PreferLocal         bool     `toml:"prefer_local"`
    ModelSmall          string   `toml:"model_small"`   // cheap text-only calls
    ModelStrong         string   `toml:"model_strong"`  // reasoning / tool-calling turns
}
```

Empty strings (`""`, the zero value) are the natural "not configured" state;
no additional boolean flag is needed.

---

## TaskKind and ModelRouter (`internal/ai/select.go`)

A lightweight, immutable struct added to the existing `select.go` file:

```go
// TaskKind classifies the nature of an LLM call for model routing.
type TaskKind int

const (
    // TaskKindReasoning is the main agent tool-calling loop (default).
    TaskKindReasoning TaskKind = iota
    // TaskKindCheap covers summarisation, auto-compact, and other text-only tasks.
    TaskKindCheap
)

// ModelRouter selects a per-task model override from the [routing] config.
// The zero value disables routing: ModelFor always returns "" (use provider
// default). Safe for concurrent use — immutable after construction.
type ModelRouter struct {
    Small  string // model for TaskKindCheap; "" = provider default
    Strong string // model for TaskKindReasoning; "" = provider default
}

// ModelFor returns the model override for kind, or "" when that kind is
// not configured. Callers pass "" to ai.Request.Model to use the provider
// default unchanged — this API needs no separate "enabled" boolean.
func (r ModelRouter) ModelFor(kind TaskKind) string {
    switch kind {
    case TaskKindCheap:
        return r.Small
    case TaskKindReasoning:
        return r.Strong
    }
    return ""
}
```

---

## Which call sites are "cheap" vs "reasoning"

### Cheap (small model)

All three are text-only, no tool calling, short context:

| Call site | Location | Description |
|---|---|---|
| `maybeAutoCompact` | `internal/app/app.go` | Background session compaction after a turn |
| `RunSession` (`compact`) | `internal/app/app.go` | User-triggered `/compact` command |
| `RunSession` (`rebuild-context`) | `internal/app/app.go` | `/rebuild-context` command |

All three use `provider.Complete` or `provider.Stream` with no tools. The model
override is passed via `ai.Request.Model` (all three providers check this field
before falling back to their configured model — see `anthropic.go:73`,
`openai.go:117`, `ollama.go:81`).

### Reasoning (strong model)

| Call site | Location | Description |
|---|---|---|
| `agent.loop` | `internal/agent/agent.go` | Every tool-calling turn in the agentic loop |

The agent loop calls `a.provider.Stream(ctx, ai.Request{...})` once per
iteration. A new `ModelOverride string` field in `agent.Config` carries the
strong-model name through and is included in every `ai.Request`.

---

## Wiring (`internal/app/app.go`)

Add a `router ai.ModelRouter` field to `App`:

```go
type App struct {
    // ...
    router ai.ModelRouter
}
```

Populate it in `New()`:

```go
router: ai.ModelRouter{
    Small:  cfg.Routing.ModelSmall,
    Strong: cfg.Routing.ModelStrong,
},
```

**Cheap calls** — pass the small model directly into the `ai.Request`:

```go
// maybeAutoCompact (existing CompleteText call becomes a direct Stream call
// with Model set):
model := a.router.ModelFor(ai.TaskKindCheap)
summary, err := ai.CompleteTextRouted(ctx, provider, "...", msgs, model)
```

`ai.CompleteTextRouted` is a new thin helper (6 lines) in `internal/ai/provider.go`
that forwards a `model` override into the `ai.Request` passed to `Stream`.
Adding it avoids touching `CompleteText`'s existing signature and all its callers.

**Reasoning calls** — pass the strong model via `agent.Config`:

```go
agentCfg := agent.Config{
    // ...
    ModelOverride: a.router.ModelFor(ai.TaskKindReasoning),
}
```

**`childConfig` propagation** — `ModelOverride` is a plain string in the
`Config` struct; Go's value copy inside `childConfig` propagates it
automatically to delegated sub-agents (same routing policy, smaller budget).

---

## `agent.Config` change (`internal/agent/agent.go`)

Add one field:

```go
// ModelOverride, when non-empty, is passed as Request.Model in every provider
// Stream call within the agent loop. It selects the "strong" model for
// reasoning turns when task-based routing is configured. An empty string keeps
// the provider's own configured model (unchanged from today).
ModelOverride string
```

In `loop()`:

```go
evCh, err := a.provider.Stream(ctx, ai.Request{
    System:   system,
    Messages: messages,
    Tools:    toolSpecs,
    Model:    a.config.ModelOverride, // "" = provider default
})
```

---

## Default behaviour (routing OFF)

When neither `model_small` nor `model_strong` is configured:

- `ModelRouter.ModelFor(...)` always returns `""`.
- Every `ai.Request.Model = ""`, so all three providers fall through to their
  configured default model.
- The agent loop is unchanged — `a.config.ModelOverride == ""`.
- `maybeAutoCompact` / `streamText` continue to use the provider default.

The code path at zero config is byte-identical to today.

---

## Backward compatibility

The `/model` REPL command sets `App.override`, which is plumbed through
`providers().Select()` and becomes the provider passed to the agent and text
calls. Model routing affects only the `Model` field of individual `ai.Request`
objects; a session-level `/model` switch still wins because it replaces the
entire provider (not just the model field). There is no conflict.

---

## Files changed

- `docs/superpowers/specs/2026-07-21-wp3-2-model-routing-design.md` (this file)
- `internal/config/config.go` — `RoutingConfig.ModelSmall`, `RoutingConfig.ModelStrong`
- `internal/config/config_test.go` — parse `[routing]` model fields
- `internal/ai/select.go` — `TaskKind`, `ModelRouter`, `ModelFor`
- `internal/ai/select_test.go` — `ModelRouter` unit tests
- `internal/ai/provider.go` — `CompleteTextRouted` helper
- `internal/agent/agent.go` — `Config.ModelOverride`; pass it in `loop()`
- `internal/agent/agent_test.go` — model-override propagation test
- `internal/app/app.go` — `router` field, wire cheap and reasoning call sites
- `internal/app/model_test.go` — routing initialisation test
- `config/spaid.toml` — document `model_small` / `model_strong`
- `README.md` — document model routing
- `docs/spai.1` — add `model_small` / `model_strong` to man page
- `docs/roadmap.md` — move "Model routing" from Next → Recently completed
- `site/src/content/docs/reference/roadmap.md` — same update
