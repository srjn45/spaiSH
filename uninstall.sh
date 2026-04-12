#!/usr/bin/env bash
set -euo pipefail

echo "Stopping and disabling spaid service..."
systemctl --user stop spaid 2>/dev/null || true
systemctl --user disable spaid 2>/dev/null || true

echo "Removing files..."
rm -f "$HOME/.local/bin/spai"
rm -f "$HOME/.local/bin/spaid"
rm -f "$HOME/.local/bin/spaish"
rm -f "$HOME/.config/systemd/user/spaid.service"
rm -f "$HOME/.local/share/spaios/spaid.sock"

systemctl --user daemon-reload

# Remove the SPAI_SESSION_ID export lines added by install.sh.
remove_session_id() {
  local rc_file="$1"
  if [ -f "$rc_file" ] && grep -q 'SPAI_SESSION_ID' "$rc_file"; then
    sed -i '/# spaiOS: per-shell session isolation/d' "$rc_file"
    sed -i '/SPAI_SESSION_ID/d' "$rc_file"
    echo "  → Removed SPAI_SESSION_ID from $rc_file"
  fi
}

echo "Cleaning shell init files..."
remove_session_id "$HOME/.bashrc"
remove_session_id "$HOME/.zshrc"

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
