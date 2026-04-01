#!/usr/bin/env bash
# bootstrap.sh — Platform-detection wrapper for attach-guard plugin.
# Selects the correct precompiled binary for the current OS/architecture
# and execs it with all arguments forwarded.
#
# If the binary is missing and Go source is available (local dev), it builds
# automatically. If neither binary nor source exists, it fails open so that
# a broken plugin install does not block all Bash usage.
set -euo pipefail

PLUGIN_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# fatal_hook emits a JSON hook response that blocks the tool call with a
# human-readable reason, then exits.
fatal_hook() {
  local msg="$1"
  echo "attach-guard: $msg" >&2
  cat <<EOJSON
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"attach-guard plugin error: $msg"}}
EOJSON
  exit 2
}

# warn_and_allow emits a JSON hook response that allows the tool call but
# prints a warning to stderr. Used when the plugin cannot run (missing binary,
# no Go toolchain) so it does not block all Bash commands.
warn_and_allow() {
  local msg="$1"
  echo "attach-guard: WARNING: $msg" >&2
  cat <<EOJSON
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}
EOJSON
  exit 0
}

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    fatal_hook "unsupported architecture: $ARCH"
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    fatal_hook "unsupported OS: $OS"
    ;;
esac

BINARY="${PLUGIN_ROOT}/hooks/bin/attach-guard-${OS}-${ARCH}"

# If the binary is missing, try to build it from source (local dev workflow).
if [[ ! -x "$BINARY" ]]; then
  # Check if Go source is available (we're in the repo checkout)
  GO_MAIN="${PLUGIN_ROOT}/../cmd/attach-guard"
  if [[ -d "$GO_MAIN" ]] && command -v go &>/dev/null; then
    echo "attach-guard: binary not found, building from source..." >&2
    mkdir -p "${PLUGIN_ROOT}/hooks/bin"
    if GOOS="$OS" GOARCH="$ARCH" go build -ldflags="-s -w" -o "$BINARY" "$GO_MAIN"; then
      echo "attach-guard: built $BINARY" >&2
    else
      warn_and_allow "failed to build from source — attach-guard is not active. Run 'make plugin-build' to fix."
    fi
  else
    warn_and_allow "binary not found and Go source not available — attach-guard is not active. Install from source or wait for a release with prebuilt binaries."
  fi
fi

[[ -x "$BINARY" ]] || fatal_hook "binary exists but is not executable: $BINARY"

# Export plugin config directory so the binary can find bundled defaults
export ATTACH_GUARD_PLUGIN_CONFIG_DIR="${PLUGIN_ROOT}/config"

exec "$BINARY" "$@"
