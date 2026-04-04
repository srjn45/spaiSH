#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/spaios"
SYSTEMD_DIR="$HOME/.config/systemd/user"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Building spaiOS..."
cd "$REPO_DIR"
go build -o "$INSTALL_DIR/spai" ./cmd/spai/
go build -o "$INSTALL_DIR/spaid" ./cmd/spaid/

echo "Installing config..."
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/spaid.toml" ]; then
  cp "$REPO_DIR/config/spaid.toml" "$CONFIG_DIR/spaid.toml"
  echo "Config installed at $CONFIG_DIR/spaid.toml"
  echo "  → Edit it to add your API endpoint and model name."
else
  echo "Config already exists at $CONFIG_DIR/spaid.toml — not overwriting."
fi

echo "Installing systemd user service..."
mkdir -p "$SYSTEMD_DIR"
cp "$REPO_DIR/systemd/spaid.service" "$SYSTEMD_DIR/spaid.service"
systemctl --user daemon-reload
systemctl --user enable --now spaid

echo ""
echo "Installation complete."
echo ""
echo "Next steps:"
echo "  1. Edit ~/.config/spaios/spaid.toml — set your API endpoint and model."
echo "  2. Set your API key:  export SPAI_API_KEY='your-key'  (add to ~/.bashrc)"
echo "  3. Run: spai 'is my system healthy?'"
echo ""
echo "Or to use a local model instead:"
echo "  Install a local model runtime, then set prefer_local = true in spaid.toml"
