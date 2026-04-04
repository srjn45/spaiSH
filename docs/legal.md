# Legal

## Disclaimer

spaiOS is an **experimental personal project**. It is provided as-is, with no warranties of any kind — express, implied, or statutory.

By installing or using spaiOS, you acknowledge:

1. **This software is experimental.** It is not production-ready. It may contain bugs that cause data loss, system instability, or unintended command execution.

2. **You are responsible for your own system.** spaiOS executes shell commands on your behalf. You must review and confirm every proposed action before it runs. The confirmation prompt is not optional.

3. **You are responsible for your API costs.** spaiOS connects to whichever AI provider you configure using your own credentials. All usage costs are billed to you by your provider. spaiOS has no visibility into your billing.

4. **spaiOS is not affiliated with any AI provider, Linux distribution, or commercial software vendor.** It is an independent open-source project. Any AI provider or Linux distribution you use alongside spaiOS is governed by their own terms.

5. **No support is guaranteed.** This is a personal project maintained on a best-effort basis.

---

## Third-Party Software

spaiOS does not bundle or redistribute any third-party proprietary software. It communicates with external services via standard HTTP APIs using credentials you provide.

If you choose to use a cloud AI provider:
- You must have a valid account and agree to that provider's terms of service
- You must supply your own API key — spaiOS does not provide one
- Your queries are sent to the provider's servers subject to their privacy policy

If you choose to use a local model runtime (such as Ollama):
- Install and operate it independently according to its own documentation and license
- spaiOS communicates with it via its local HTTP API

---

## Trademarks

spaiOS does not claim any affiliation with, endorsement by, or relationship with any third-party company, product, or trademark. Any product names, trademarks, or registered trademarks mentioned in this documentation belong to their respective owners and are referenced solely for descriptive purposes.

---

## Privacy

When using a cloud AI provider:
- Your queries, file contents, and command outputs may be sent to your provider's servers
- Review your provider's privacy policy to understand how your data is handled
- Use `--local` flag or set `prefer_local = true` in config to keep all data on-device

When using a local model:
- No data leaves your device
- spaiOS itself does not collect, transmit, or store any telemetry

---

## License

spaiOS is released under the **Apache License, Version 2.0**.

You may use, modify, and distribute this software freely. If you distribute
modified versions, you must state what you changed. You cannot use the spaiOS
name or trademarks to endorse derived products without permission.

The full license text is in the [LICENSE](../LICENSE) file. A copy is also
available at http://www.apache.org/licenses/LICENSE-2.0

Key points in plain language:
- **Free to use** for any purpose, including commercial
- **Free to modify** — but modified files must carry a notice saying you changed them
- **Patent grant included** — contributors cannot later sue you for patent infringement
- **No warranty** — provided as-is, use at your own risk
- **No trademark grant** — you cannot call your fork "spaiOS" without permission

---

## For Contributors

By contributing to spaiOS, you agree that your contributions are licensed under the same MIT License. Do not submit code that:

- Includes proprietary third-party code without a compatible license
- Hardcodes any third-party brand names, trademarks, or product identifiers in distributed artifacts
- Bundles or redistributes proprietary software binaries
- Includes API keys, credentials, or any secrets
