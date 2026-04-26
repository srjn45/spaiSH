# Vision

## The Problem

Modern operating systems were designed for humans to issue precise, syntactic commands to a machine. Every tool, every config file, every log — you must already know what you're looking for and how to ask for it.

This made sense when computers were expensive and humans were the cheap part. That equation has flipped.

## The Idea

spaiSH inverts the model: the machine interprets **intent**, and translates it into system operations. The underlying tools — bash, systemctl, nginx, docker — still exist and still run. The cognitive layer on top of them is replaced.

This is not a chatbot wrapper around your terminal. It is a rethinking of what the interface between a human and an operating system should look like in 2026.

## The Java Analogy

Java introduced "write once, run anywhere" via the JVM — an abstraction layer that made the underlying OS irrelevant to the developer.

spaiSH proposes the same for AI-powered system interaction:

- The `spaid` daemon is the runtime — a persistent, context-aware process that understands your system state
- Any AI model (cloud or local) can back it — swapping providers is a config change
- Applications can eventually ship as **intent manifests** rather than platform-specific binaries

```
AppManifest {
  name: "Budget Tracker"
  intent: "Track monthly expenses and visualise insights"
  capabilities_required: [filesystem, network]
  actions: [read_csv, compute_totals, render_chart]
}
```

The AI reads the manifest, determines how to execute it in the current environment, and runs it. The manifest is the program.

## Why Now

- Local models are now capable enough for real system tasks
- The gap between "AI assistant" and "AI that runs your computer" is closing fast
- No one has standardised the layer above the models — that is the opportunity
- The confirmation-before-execution model makes AI involvement inherently safer than raw shell access

## Design Principles

**Opt-in, not opt-out.** Your existing shell is unchanged. AI gets involved only when you call `spai`. No interception, no monitoring, no surprises.

**Local-first.** The free tier runs entirely on your device. Nothing leaves it unless you configure a cloud provider. Safety decisions (permission classification) always run locally.

**Model-agnostic.** spaiSH has no opinion about which AI model you use. Cloud, local, self-hosted — configure an endpoint and a model name. That's it.

**Safe by default.** Every action is classified before execution. Destructive operations require explicit confirmation. The AI shows its work before doing it.

## The Bigger Picture

This starts as a shell enhancement. It becomes a platform.

Once `spaid` is a stable daemon with a well-defined socket API, any process on the system can talk to it. The FUSE filesystem (`/ai/*` paths, Phase 2) makes AI accessible from any language, any tool, without an SDK.

The long-term vision — apps shipped as intent manifests that any AI runtime can execute — becomes achievable once the runtime layer is proven and trusted.

spaiSH is the proof of concept for a new computing paradigm. It starts with one command: `spai`.
