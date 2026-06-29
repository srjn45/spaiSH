#!/usr/bin/env bash
set -euo pipefail

echo "Removing spai binary..."
rm -f "$HOME/.local/bin/spai"

# Remove the SPAI_SESSION_ID export lines added by install.sh.
remove_session_id() {
  local rc_file="$1"
  if [ -f "$rc_file" ] && grep -q 'SPAI_SESSION_ID' "$rc_file"; then
    sed -i '/# spai: per-terminal session isolation/d' "$rc_file"
    sed -i '/SPAI_SESSION_ID/d' "$rc_file"
    echo "  → Removed SPAI_SESSION_ID from $rc_file"
  fi
}

echo "Cleaning shell init files..."
remove_session_id "$HOME/.bashrc"
remove_session_id "$HOME/.zshrc"

echo ""
echo "spai uninstalled."
echo ""
echo "The following were NOT removed (your data):"
echo "  ~/.config/spaish/spaid.toml      — your config"
echo "  ~/.config/spaish/llm-state.json  — active model and runtime state"
echo "  ~/.local/share/spaish/           — session history and logs"
echo ""
echo "To remove those too: rm -rf ~/.config/spaish ~/.local/share/spaish"
echo ""
echo "To also remove installed Ollama models:"
echo "  ollama list              — see installed models"
echo "  spai llm remove <model>  — remove a specific model"
echo "  rm -rf ~/.ollama         — remove Ollama and all models"
