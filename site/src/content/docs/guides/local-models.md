---
title: Local models
description: Install, configure, and switch local models via the built-in LLM manager (Ollama).
---


spaiSH ships with a built-in LLM manager that helps you install, configure, and switch between local AI models without leaving your terminal. It is designed for users who are new to self-hosted AI.

## Why

Running `spai` without an API key requires a local model runtime. Setting one up — finding the right software, downloading a model, pointing spaiSH at it — is a multi-step process that stops most beginners. The LLM manager handles all of that with a single command.

## Commands

```bash
spai llm status                  # show what's installed, what's running, active model
spai llm install [runtime]       # install a runtime (ollama, bitnet) — default: ollama
spai llm use-runtime <runtime>   # switch active runtime (ollama, bitnet)
spai llm list                    # list installed + recommended models for active runtime
spai llm pull <model>            # download a model (e.g. qwen2.5-coder:7b)
spai llm use <model>             # set the active model for local inference
```

## How It Works

The LLM manager is built into `spaid` as an extension of the existing model router. It does not make AI API calls — all operations are either system checks or shell commands run through spaiSH's standard confirmation flow.

```
spai llm install
    ↓
spai CLI → spaid daemon → LLM Manager
    ↓
Checks system → Generates install commands → Returns plan
    ↓
User confirms (same [Elevated] flow as any system command)
    ↓
spaid executes via executor → streams output back
```

Every command the LLM manager proposes goes through the permission tier engine before it reaches you. Nothing runs without your confirmation.

## State

The active runtime and model are persisted in `~/.config/spaish/llm-state.json`:

```json
{
  "active_runtime": "bitnet",
  "active_model": "BitNet-b1.58-2B-4T",
  "runtimes": {
    "ollama": {
      "installed": true,
      "version": "0.6.1",
      "endpoint": "http://localhost:11434"
    },
    "bitnet": {
      "installed": true,
      "endpoint": "http://localhost:8080"
    }
  }
}
```

This file is read by `spaid` at startup. Changes made via `spai llm use` take effect on the next daemon restart.

## Supported Runtimes

| Runtime | Status | Notes |
|---------|--------|-------|
| Ollama  | Supported | Single binary, Docker optional, wide model support |
| BitNet (bitnet.cpp) | Supported | Microsoft 1-bit AI — runs on CPU only, no GPU required |
| vLLM    | Planned | Requires Python + CUDA — power users only |
| llama.cpp | Planned | Low-level, maximum control |

## Recommended Models

These models are tested and curated for spaiSH users:

### Ollama models

| Model | RAM Required | Best For |
|-------|-------------|---------|
| `qwen2.5-coder:7b` | 8 GB | Coding, config editing, system tasks |
| `llama3.2:3b` | 4 GB | Fast general assistant |
| `phi4-mini` | 4 GB | Low-end hardware |
| `mistral:7b` | 8 GB | General purpose |

### BitNet models

BitNet models run entirely on CPU using 1-bit quantization. They require more RAM at rest but use dramatically less compute per token than standard quantized models.

| Model | RAM Required | Best For |
|-------|-------------|---------|
| `BitNet-b1.58-2B-4T` | 4 GB | Default — fast, efficient, great for most tasks |
| `BitNet-b1.58-3B` | 6 GB | Stronger reasoning on low-end hardware |
| `Llama3-8B-1.58-100B-tokens` | 12 GB | Best BitNet quality, Llama 3 architecture |

Models are downloaded by `setup_env.py` during `spai llm install bitnet`.

## Backward Compatibility

If you already have Ollama installed and running, spaiSH detects it automatically. `spai llm install` will tell you it's already installed. No migration needed.
