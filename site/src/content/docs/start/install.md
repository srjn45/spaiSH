---
title: Install & setup
description: Install spai via Homebrew, a Linux package, or the install script, then configure a provider.
---

`spai` installs as a single binary to `~/.local/bin`. No root, no systemd, no
background service.

## Homebrew (macOS)

```bash
brew tap srjn45/spaish https://github.com/srjn45/spaiSH
brew install spai
```

## Linux packages (.deb / .rpm)

Download the `.deb` or `.rpm` for your architecture from the latest
[GitHub Release](https://github.com/srjn45/spaiSH/releases), then install:

```bash
# Debian / Ubuntu
sudo dpkg -i spai_*.deb

# Fedora / RHEL / openSUSE
sudo rpm -i spai_*.rpm
```

Packages ship the `spai` binary at `/usr/bin/spai` along with shell completions
and the man page.

## Install script

```bash
git clone https://github.com/srjn45/spaiSH && cd spaiSH
./install.sh
```

`install.sh` downloads a **prebuilt binary** for your OS/arch from the latest
release when one is available, and otherwise **builds from source** (requires Go
1.25+). Force a source build with `SPAI_FROM_SOURCE=1 ./install.sh`. Override
the install location with `INSTALL_DIR=/path ./install.sh`.

Prebuilt binaries cover linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64.
Each release also ships a `*_checksums.txt` (sha256) for verification.

## Configure a provider

```bash
spai init
```

The wizard lets you pick Anthropic, an OpenAI-compatible endpoint, or Ollama,
sets the model, and runs a live connection test.

For an API provider you reference an environment variable rather than storing
the key in a file:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

The config is written to `~/.config/spaish/spaid.toml` — see
[Configuration](/spaiSH/reference/configuration/) for every key.

## Next

Head to the [Quickstart](/spaiSH/start/quickstart/).
