# Roadmap

## Phase 1 — Core Daemon + Shell Integration

**Status: In development**

The foundational layer. Establishes `spaid` as a working daemon and `spai` as a usable daily driver.

### Deliverables

- [ ] `spaid` daemon — Go binary, Unix socket, systemd user service
- [ ] `spai` CLI client — sends queries, streams responses
- [ ] Permission tier engine — static rule-based command classification
- [ ] Model router — cloud API + local Ollama fallback
- [ ] Session context — working directory, recent commands, git state
- [ ] `spai !!` — analyse last failed command
- [ ] `--dry-run` flag — show plan without executing
- [ ] `--local` flag — force local model
- [ ] One-time disclaimer on first run
- [ ] Single-command installer (`curl | bash`, no root)
- [ ] Uninstaller
- [ ] Config template with documentation

### Success Criteria

A developer can install spaiOS on a fresh Linux machine and use `spai` to diagnose and fix a real system problem within 5 minutes, with no commands executing without their confirmation.

---

## Phase 2 — FUSE Filesystem

**Status: Planned**

Makes `spaid` accessible from any process without an SDK or API call.

### Deliverables

- [ ] FUSE mount at `/ai/*`
- [ ] `cat /ai/explain/<path>` — explain any file
- [ ] `cat /ai/fix/<path>` — return corrected version of any config
- [ ] `cat /ai/summarise/<path>` — summarise a file or directory
- [ ] Mount management (auto-mount on boot, `spai mount/unmount`)

---

## Phase 3 — Deep System Integration

**Status: Research**

Weaves `spaid` into the Linux execution fabric.

### Deliverables

- [ ] PAM module — context-aware authentication (experimental, opt-in)
- [ ] eBPF probes — real-time syscall and network observation
- [ ] Behavioural anomaly detection (unknown binary execution, unusual outbound calls)
- [ ] Systemd integration — `spaid` as a service supervisor

---

## Phase 4 — Full Stack

**Status: Vision**

Completes the AI-native OS story.

### Deliverables

- [ ] Wayland compositor integration — system-wide context awareness
- [ ] GUI terminal with AI reasoning display
- [ ] Intent manifest specification — apps as declarative intent files
- [ ] Multi-provider abstraction — unified API across model providers
- [ ] Bootable ISO (Linux base + spaiOS pre-installed)

---

## Versioning

Releases follow semantic versioning: `MAJOR.MINOR.PATCH`

- Phase 1 completion → `v0.1.0`
- Phase 2 completion → `v0.2.0`
- Phase 3 completion → `v0.3.0`
- Phase 4 completion → `v1.0.0`
