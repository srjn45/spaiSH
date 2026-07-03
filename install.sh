#!/usr/bin/env bash
set -eu

# Configuration ---------------------------------------------------------------
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
REPO="srjn45/spaiSH"
# Resolve the repo dir for the from-source path. Works under bash; falls back to
# the current directory for plain POSIX sh.
if [ -n "${BASH_SOURCE:-}" ]; then
  REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
else
  REPO_DIR="$(pwd)"
fi

# Force from-source builds with: SPAI_FROM_SOURCE=1 ./install.sh
FROM_SOURCE="${SPAI_FROM_SOURCE:-0}"

# Helpers ---------------------------------------------------------------------
have() { command -v "$1" >/dev/null 2>&1; }

detect_os() {
  os="$(uname -s)"
  case "$os" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *) echo "unsupported" ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64 | amd64) echo "amd64" ;;
    arm64 | aarch64) echo "arm64" ;;
    *) echo "unsupported" ;;
  esac
}

# Download $1 to $2 using curl or wget. Returns non-zero on failure.
download() {
  url="$1"
  out="$2"
  if have curl; then
    curl -fsSL "$url" -o "$out"
  elif have wget; then
    wget -qO "$out" "$url"
  else
    return 1
  fi
}

# Fetch the latest release tag from the GitHub API. Prints empty on failure.
latest_tag() {
  api="https://api.github.com/repos/$REPO/releases/latest"
  if have curl; then
    curl -fsSL "$api" 2>/dev/null | grep '"tag_name"' | head -n1 | cut -d'"' -f4
  elif have wget; then
    wget -qO- "$api" 2>/dev/null | grep '"tag_name"' | head -n1 | cut -d'"' -f4
  fi
}

build_from_source() {
  if ! have go; then
    echo "Error: cannot build from source — 'go' is not installed." >&2
    echo "Install Go 1.25+ (https://go.dev/dl/) or use a prebuilt release." >&2
    exit 1
  fi
  echo "Building spai from source..."
  ( cd "$REPO_DIR" && go build -o "$INSTALL_DIR/spai" ./cmd/spai/ )
  echo "  → Built spai from source"
}

install_prebuilt() {
  os="$(detect_os)"
  arch="$(detect_arch)"
  if [ "$os" = "unsupported" ] || [ "$arch" = "unsupported" ]; then
    echo "  → No prebuilt binary for $(uname -s)/$(uname -m)"
    return 1
  fi

  tag="$(latest_tag)"
  if [ -z "$tag" ]; then
    echo "  → Could not determine latest release"
    return 1
  fi

  asset="spai_${tag}_${os}_${arch}"
  url="https://github.com/$REPO/releases/download/$tag/${asset}.tar.gz"
  tmp="$(mktemp -d 2>/dev/null || mktemp -d -t spai)"
  trap 'rm -rf "$tmp"' EXIT

  echo "Downloading prebuilt spai ${tag} (${os}/${arch})..."
  if ! download "$url" "$tmp/spai.tar.gz"; then
    echo "  → Download failed: $url"
    return 1
  fi

  if ! tar -xzf "$tmp/spai.tar.gz" -C "$tmp"; then
    echo "  → Failed to extract archive"
    return 1
  fi

  if [ ! -f "$tmp/spai" ]; then
    echo "  → Archive did not contain a spai binary"
    return 1
  fi

  install -m 0755 "$tmp/spai" "$INSTALL_DIR/spai" 2>/dev/null || {
    cp "$tmp/spai" "$INSTALL_DIR/spai"
    chmod 0755 "$INSTALL_DIR/spai"
  }
  echo "  → Installed prebuilt spai ${tag}"
  return 0
}

# Install ---------------------------------------------------------------------
mkdir -p "$INSTALL_DIR"

if [ "$FROM_SOURCE" = "1" ]; then
  build_from_source
elif ! install_prebuilt; then
  echo "Falling back to building from source..."
  build_from_source
fi

# Per-terminal session isolation ---------------------------------------------
# Inject a per-shell session ID so each terminal keeps its own conversation.
# The literal $$ expands at shell startup to the current shell's PID.
inject_session_id() {
  rc_file="$1"
  if [ -f "$rc_file" ]; then
    if grep -q 'SPAI_SESSION_ID' "$rc_file"; then
      echo "  → SPAI_SESSION_ID already in $rc_file — skipping"
    else
      printf '\n# spai: per-terminal session isolation\nexport SPAI_SESSION_ID=$$\n' >> "$rc_file"
      echo "  → Added SPAI_SESSION_ID to $rc_file"
    fi
  fi
}

install_completions() {
  comp_src="$REPO_DIR/completions"
  [ -d "$comp_src" ] || return 0

  echo "Installing shell completions..."

  bash_dir="$HOME/.local/share/bash-completion/completions"
  if mkdir -p "$bash_dir" 2>/dev/null; then
    cp "$comp_src/spai.bash" "$bash_dir/spai" 2>/dev/null && \
      echo "  → bash completion → $bash_dir/spai" || true
  fi

  zsh_dir="$HOME/.local/share/zsh/site-functions"
  if mkdir -p "$zsh_dir" 2>/dev/null; then
    cp "$comp_src/spai.zsh" "$zsh_dir/_spai" 2>/dev/null && \
      echo "  → zsh completion  → $zsh_dir/_spai" || true
  fi

  fish_dir="$HOME/.config/fish/completions"
  if mkdir -p "$fish_dir" 2>/dev/null; then
    cp "$comp_src/spai.fish" "$fish_dir/spai.fish" 2>/dev/null && \
      echo "  → fish completion → $fish_dir/spai.fish" || true
  fi
}

echo "Configuring per-terminal sessions..."
inject_session_id "$HOME/.bashrc"
inject_session_id "$HOME/.zshrc"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "  → Note: $INSTALL_DIR is not on your PATH — add it to use \`spai\` directly." ;;
esac

# Install man page (best-effort) -----------------------------------------------
MAN_DIR="${MAN_DIR:-$HOME/.local/share/man/man1}"

install_man_page() {
  src="$REPO_DIR/docs/spai.1"
  if [ ! -f "$src" ]; then
    echo "  → Man page not found at $src — skipping"
    return 0
  fi
  if ! mkdir -p "$MAN_DIR" 2>/dev/null; then
    echo "  → Could not create $MAN_DIR — skipping man page install"
    return 0
  fi
  if cp "$src" "$MAN_DIR/spai.1" 2>/dev/null; then
    echo "  → Installed man page → $MAN_DIR/spai.1"
  else
    echo "  → Could not install man page — skipping"
  fi
}

install_man_page
install_completions

echo ""
echo "Installed spai → $INSTALL_DIR/spai"
echo ""
echo "Next steps:"
echo "  1. Run:  spai init        — choose a provider (Anthropic / OpenAI / Ollama) and test it."
echo "  2. Restart your shell (or: source ~/.bashrc) for per-terminal sessions."
echo "  3. Try:  spai \"what's listening on port 8080?\"   or just  spai  for a session."
echo ""
