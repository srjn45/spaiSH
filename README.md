# spaiOS

An experimental AI-native operating environment built on Linux.

spaiOS makes AI a first-class system primitive — not a chatbot on top of your OS, but a runtime woven into it. You keep your existing shell and workflow. AI gets involved only when you ask for it.

```bash
$ spai nginx keeps returning 502, fix it
  Reading: /var/log/nginx/error.log ... /etc/nginx/nginx.conf
  Found: upstream timeout — proxy_read_timeout set to 30s, backend needs 90s

  I will edit: /etc/nginx/nginx.conf
    proxy_read_timeout 30s  →  proxy_read_timeout 90s

  Apply? [y/n]:
```

---

## Quick Install

```bash
curl -fsSL https://get.spaios.dev/install.sh | bash
```

No root required. Works on any modern Linux distribution.

After install, set your AI provider key:

```bash
export SPAI_API_KEY="your-key-here"
```

Or configure a local model — see [Shell Integration](docs/shell-integration.md).

---

## Documentation

- [Vision](docs/vision.md) — what spaiOS is and why it exists
- [Architecture](docs/architecture.md) — how it works under the hood
- [Shell Integration](docs/shell-integration.md) — using `spai`, configuration, examples
- [Roadmap](docs/roadmap.md) — what's built, what's next
- [Legal](docs/legal.md) — license, disclaimer, compliance
- [Contributing](docs/contributing.md) — how to contribute

---

## Status

**Experimental. Personal project. Use at your own risk.**

Phase 1 (core daemon + shell integration) is under active development.
See [Roadmap](docs/roadmap.md) for current status.

---

## License

Apache 2.0 — see [LICENSE](LICENSE)
