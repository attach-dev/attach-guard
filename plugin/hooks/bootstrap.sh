#!/usr/bin/env bash
# bootstrap.sh — Platform-detection wrapper for attach-guard plugin.
# Selects the correct precompiled binary for the current OS/architecture
# and execs it with all arguments forwarded.
set -euo pipefail

PLUGIN_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)
    echo "attach-guard: unsupported architecture: $ARCH" >&2
    exit 2
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    echo "attach-guard: unsupported OS: $OS" >&2
    exit 2
    ;;
esac

BINARY="${PLUGIN_ROOT}/hooks/bin/attach-guard-${OS}-${ARCH}"

if [[ ! -x "$BINARY" ]]; then
  echo "attach-guard: binary not found: $BINARY" >&2
  echo "Run 'make plugin-build' from the attach-guard repository to compile binaries." >&2
  exit 2
fi

# Export plugin config directory so the binary can find bundled defaults
export ATTACH_GUARD_PLUGIN_CONFIG="${PLUGIN_ROOT}/config"

exec "$BINARY" "$@"
