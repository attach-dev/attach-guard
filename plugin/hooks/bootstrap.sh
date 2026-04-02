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
    # No Go source available — attempt to download a prebuilt binary
    PLUGIN_VERSION="$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "${PLUGIN_ROOT}/.claude-plugin/plugin.json" | head -1)"
    if [[ -z "$PLUGIN_VERSION" ]]; then
      fatal_error "binary not found and could not read version from plugin.json."
    fi

    DOWNLOAD_URL="https://github.com/attach-dev/attach-guard/releases/download/v${PLUGIN_VERSION}/attach-guard-${OS}-${ARCH}"
    CHECKSUMS_URL="https://github.com/attach-dev/attach-guard/releases/download/v${PLUGIN_VERSION}/checksums.txt"

    echo "attach-guard: binary not found, downloading v${PLUGIN_VERSION} for ${OS}/${ARCH}..." >&2
    mkdir -p "${PLUGIN_ROOT}/hooks/bin"

    CURL_ERR="$(mktemp)"
    if curl -fSL --connect-timeout 5 --max-time 30 -o "$BINARY" "$DOWNLOAD_URL" 2>"$CURL_ERR"; then
      # Verify checksum — refuse to run unverified binaries
      CHECKSUMS_FILE="$(mktemp)"
      if curl -fSL --connect-timeout 5 --max-time 10 -o "$CHECKSUMS_FILE" "$CHECKSUMS_URL" 2>/dev/null; then
        EXPECTED="$(grep "attach-guard-${OS}-${ARCH}" "$CHECKSUMS_FILE" | awk '{print $1}')"
        if [[ -z "$EXPECTED" ]]; then
          rm -f "$CHECKSUMS_FILE" "$BINARY"
          fatal_error "no checksum found for attach-guard-${OS}-${ARCH} in checksums.txt."
        fi
        if command -v shasum &>/dev/null; then
          SHA_CMD="shasum -a 256"
        elif command -v sha256sum &>/dev/null; then
          SHA_CMD="sha256sum"
        else
          rm -f "$CHECKSUMS_FILE" "$BINARY"
          fatal_error "no SHA-256 tool found (need shasum or sha256sum)."
        fi
        ACTUAL="$($SHA_CMD "$BINARY" | awk '{print $1}')"
        rm -f "$CHECKSUMS_FILE"
        if [[ "$EXPECTED" != "$ACTUAL" ]]; then
          rm -f "$BINARY"
          fatal_error "checksum mismatch for downloaded binary (expected ${EXPECTED}, got ${ACTUAL}). Aborting."
        fi
      else
        rm -f "$CHECKSUMS_FILE" "$BINARY"
        fatal_error "could not download checksums — refusing to run unverified binary."
      fi
      rm -f "$CURL_ERR"
      chmod +x "$BINARY"
      echo "attach-guard: downloaded $BINARY" >&2
    else
      echo "attach-guard: curl error: $(cat "$CURL_ERR")" >&2
      rm -f "$BINARY" "$CURL_ERR"
      fatal_error "binary not found and download failed. Install Go and build from source, or check https://github.com/attach-dev/attach-guard/releases for available binaries."
    fi
  fi
fi

[[ -x "$BINARY" ]] || fatal_error "binary exists but is not executable: $BINARY"

# --- Exec ---

export ATTACH_GUARD_PLUGIN_CONFIG_DIR="${PLUGIN_ROOT}/config"

# Map plugin userConfig token to the env var the binary expects, if not already set
if [[ -z "${SOCKET_API_TOKEN:-}" && -n "${CLAUDE_PLUGIN_OPTION_SOCKET_API_TOKEN:-}" ]]; then
  export SOCKET_API_TOKEN="$CLAUDE_PLUGIN_OPTION_SOCKET_API_TOKEN"
fi

# Require a Socket API token — without it, every lookup returns "provider unavailable"
if [[ -z "${SOCKET_API_TOKEN:-}" ]]; then
  fatal_error "Socket API token not configured. Run: claude plugin config set attach-guard@attach-dev socket_api_token <your-token>  (get a free token at https://socket.dev)"
fi

exec "$BINARY" "$@"
