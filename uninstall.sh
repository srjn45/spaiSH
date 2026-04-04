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
echo "  ~/.config/spaios/spaid.toml      — your config"
echo "  ~/.config/spaios/llm-state.json  — active model and runtime state"
echo "  ~/.local/share/spaios/           — session history and logs"
echo ""
echo "To remove those too: rm -rf ~/.config/spaios ~/.local/share/spaios"
echo ""
echo "To also remove installed Ollama models:"
echo "  ollama list              — see installed models"
echo "  spai llm remove <model>  — remove a specific model"
echo "  rm -rf ~/.ollama         — remove Ollama and all models"
