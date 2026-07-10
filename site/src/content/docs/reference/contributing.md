---
title: Contributing
description: How to contribute to spaiSH.
---


spaiSH is an experimental personal project. Contributions are welcome but reviewed carefully — this software executes commands on users' systems, so correctness and safety take priority over features.

---

## Before You Contribute

1. Read [Architecture](/spaiSH/concepts/architecture/) to understand the system design
2. Check the [Roadmap](/spaiSH/reference/roadmap/) — contributions aligned with active phases are prioritized
3. Open an issue before starting significant work — avoid building something that won't be merged

---

## Development Setup

### Requirements

- Go 1.22+
- Linux (the daemon uses Linux-specific APIs)
- Ollama (optional, for testing local model routing)

### Build

```bash
git clone https://github.com/srajanpathak/spaish
cd spaish
go build ./cmd/spai ./cmd/spaid
```

### Run locally

```bash
# Start the daemon in foreground (dev mode)
./spaid --dev

# In another terminal, test the client
./spai "list files in current directory"
```

### Tests

```bash
go test ./...
```

---

## Rules

**No third-party brand references in distributed code.** Do not hardcode any AI provider name, Linux distribution name, or commercial product name in source code, config templates, install scripts, or documentation shipped with the project. Use generic terms: "AI provider", "language model", "API endpoint", "Linux distribution". See [Legal](/spaiSH/reference/legal/).

**Safety first.** The permission tier engine must never be weakened. If you're touching `internal/permissions/`, every change needs tests proving the classification behaviour.

**No telemetry.** Do not add any analytics, crash reporting, or usage tracking. spaiSH does not phone home.

**Keep the daemon lean.** `spaid` is a system daemon. Dependencies should be minimal. Every new dependency needs a clear justification.

---

## Code Style

Standard Go conventions. Run before submitting:

```bash
gofmt -w .
go vet ./...
```

---

## Submitting

1. Fork the repo
2. Create a branch: `git checkout -b your-feature`
3. Make changes with tests
4. Open a pull request with a clear description of what and why

All contributions are subject to the MIT License.
