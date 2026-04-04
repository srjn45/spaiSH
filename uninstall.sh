#!/usr/bin/env bash
set -euo pipefail

echo "Stopping and disabling spaid service..."
systemctl --user stop spaid 2>/dev/null || true
systemctl --user disable spaid 2>/dev/null || true

echo "Removing files..."
rm -f "$HOME/.local/bin/spai"
rm -f "$HOME/.local/bin/spaid"
rm -f "$HOME/.config/systemd/user/spaid.service"
rm -f "$HOME/.local/share/spaios/spaid.sock"

systemctl --user daemon-reload

echo ""
echo "spaiOS uninstalled."
echo ""
echo "The following were NOT removed (your data):"
echo "  ~/.config/spaios/spaid.toml  — your config"
echo "  ~/.local/share/spaios/       — session history"
echo ""
echo "To remove those too: rm -rf ~/.config/spaios ~/.local/share/spaios"
