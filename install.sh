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
go build -o "$INSTALL_DIR/spai-fuse" ./cmd/spai-fuse/

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

echo "Creating FUSE mount point..."
if [ ! -d "/ai" ]; then
  sudo mkdir -p /ai
  echo "  → Created /ai"
else
  echo "  → /ai already exists"
fi

echo "Installing spai-fuse systemd service..."
cp "$REPO_DIR/systemd/spai-fuse.service" "$SYSTEMD_DIR/spai-fuse.service"
systemctl --user daemon-reload

# Read auto_mount from config if it exists, default to true
AUTO_MOUNT="true"
if [ -f "$CONFIG_DIR/spaid.toml" ]; then
  if grep -q 'auto_mount\s*=\s*false' "$CONFIG_DIR/spaid.toml"; then
    AUTO_MOUNT="false"
  fi
fi

if [ "$AUTO_MOUNT" = "true" ]; then
  systemctl --user enable --now spai-fuse
  echo "  → spai-fuse enabled and started"
else
  echo "  → auto_mount = false in config — skipping start (run 'spai mount' manually)"
fi

# Inject per-shell session ID into detected shell rc files.
# The literal $$ in the exported value expands at shell startup to the PID of
# the current shell, giving each terminal window a unique session.
inject_session_id() {
  local rc_file="$1"
  if [ -f "$rc_file" ]; then
    if grep -q 'SPAI_SESSION_ID' "$rc_file"; then
      echo "  → SPAI_SESSION_ID already in $rc_file — skipping"
    else
      printf '\n# spaiOS: per-shell session isolation\nexport SPAI_SESSION_ID=$$\n' >> "$rc_file"
      echo "  → Added SPAI_SESSION_ID to $rc_file"
    fi
  fi
}

echo "Configuring shell session isolation..."
inject_session_id "$HOME/.bashrc"
inject_session_id "$HOME/.zshrc"

echo ""
echo "Installation complete."
echo ""
echo "Next steps:"
echo "  1. Edit ~/.config/spaios/spaid.toml — set your API endpoint and model."
echo "  2. Set your API key:  export SPAI_API_KEY='your-key'  (add to ~/.bashrc)"
echo "  3. Restart your shell (or run: source ~/.bashrc) to activate session isolation."
echo "  4. Run: spai 'is my system healthy?'"
echo "  5. The /ai FUSE filesystem is now mounted. Try: cat /ai/explain/etc/fstab"
echo ""
echo "Or to use a local model instead:"
echo "  Install a local model runtime, then set prefer_local = true in spaid.toml"
