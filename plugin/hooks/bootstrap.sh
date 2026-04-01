#!/usr/bin/env bash
# bootstrap.sh — Platform-detection wrapper for attach-guard plugin.
# Selects the correct precompiled binary for the current OS/architecture
# and execs it with all arguments forwarded.
#
# Called in two modes:
#   bootstrap.sh hook          — PreToolUse hook (timeout-sensitive, uses hook JSON)
#   bootstrap.sh evaluate ...  — CLI evaluation (no timeout, plain stderr errors)
#
# If the binary is missing and Go source is available (local dev), it builds
# automatically on non-hook paths. In hook mode it skips auto-build (which
# could exceed the 30s hook timeout) and fails open instead.
set -euo pipefail

PLUGIN_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODE="${1:-}"

# --- Error handlers: mode-aware ---

# In hook mode, errors emit hook-shaped JSON or use exit 2.
# In non-hook mode (evaluate, version, etc.), errors go to stderr and exit 1.

fatal_error() {
  local msg="$1"
  if [[ "$MODE" == "hook" ]]; then
    # Exit 2 = blocking error. Claude shows stderr as feedback.
    echo "attach-guard: $msg" >&2
    exit 2
  else
    echo "attach-guard: error: $msg" >&2
    exit 1
  fi
}

warn_and_allow() {
  local msg="$1"
  echo "attach-guard: WARNING: $msg" >&2
  if [[ "$MODE" == "hook" ]]; then
    cat <<EOJSON
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}
EOJSON
    exit 0
  else
    exit 1
  fi
}

# --- Platform detection ---

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    fatal_error "unsupported architecture: $ARCH"
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    fatal_error "unsupported OS: $OS"
    ;;
esac

BINARY="${PLUGIN_ROOT}/hooks/bin/attach-guard-${OS}-${ARCH}"

# --- Auto-build from source (non-hook mode only) ---
# Hook mode skips auto-build to stay within the 30s hook timeout.
# Users should run 'make plugin-build' or 'claude --plugin-dir ./plugin'
# (which triggers a non-hook evaluate first) to populate the binary.

if [[ ! -x "$BINARY" ]]; then
  GO_MAIN="${PLUGIN_ROOT}/../cmd/attach-guard"

  if [[ "$MODE" == "hook" ]]; then
    # In hook mode, don't attempt a slow build — fail open
    warn_and_allow "binary not found — attach-guard is not active. Run 'make plugin-build' to compile, or invoke any /explain command to trigger auto-build."
  elif [[ -d "$GO_MAIN" ]] && command -v go &>/dev/null; then
    echo "attach-guard: binary not found, building from source..." >&2
    mkdir -p "${PLUGIN_ROOT}/hooks/bin"
    if GOOS="$OS" GOARCH="$ARCH" go build -ldflags="-s -w" -o "$BINARY" "$GO_MAIN"; then
      echo "attach-guard: built $BINARY" >&2
    else
      fatal_error "failed to build from source. Run 'make plugin-build' to fix."
    fi
  else
    fatal_error "binary not found and Go source not available. Install from source or wait for a release with prebuilt binaries."
  fi
fi

[[ -x "$BINARY" ]] || fatal_error "binary exists but is not executable: $BINARY"

# --- Exec ---

export ATTACH_GUARD_PLUGIN_CONFIG_DIR="${PLUGIN_ROOT}/config"

# Map plugin userConfig token to the env var the binary expects, if not already set
if [[ -z "${SOCKET_API_TOKEN:-}" && -n "${CLAUDE_PLUGIN_OPTION_SOCKET_API_TOKEN:-}" ]]; then
  export SOCKET_API_TOKEN="$CLAUDE_PLUGIN_OPTION_SOCKET_API_TOKEN"
fi

exec "$BINARY" "$@"
