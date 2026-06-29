#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$HOME/.local/bin"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Building spai..."
cd "$REPO_DIR"
mkdir -p "$INSTALL_DIR"
go build -o "$INSTALL_DIR/spai" ./cmd/spai/

# Inject a per-shell session ID so each terminal keeps its own conversation.
# The literal $$ expands at shell startup to the current shell's PID.
inject_session_id() {
  local rc_file="$1"
  if [ -f "$rc_file" ]; then
    if grep -q 'SPAI_SESSION_ID' "$rc_file"; then
      echo "  → SPAI_SESSION_ID already in $rc_file — skipping"
    else
      printf '\n# spai: per-terminal session isolation\nexport SPAI_SESSION_ID=$$\n' >> "$rc_file"
      echo "  → Added SPAI_SESSION_ID to $rc_file"
    fi
  fi
}

echo "Configuring per-terminal sessions..."
inject_session_id "$HOME/.bashrc"
inject_session_id "$HOME/.zshrc"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "  → Note: $INSTALL_DIR is not on your PATH — add it to use \`spai\` directly." ;;
esac

echo ""
echo "Installed spai → $INSTALL_DIR/spai"
echo ""
echo "Next steps:"
echo "  1. Run:  spai init        — choose a provider (Anthropic / OpenAI / Ollama) and test it."
echo "  2. Restart your shell (or: source ~/.bashrc) for per-terminal sessions."
echo "  3. Try:  spai \"what's listening on port 8080?\"   or just  spai  for a session."
echo ""
