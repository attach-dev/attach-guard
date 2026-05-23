#!/usr/bin/env bash
set -euo pipefail

# Public-safe local smoke for Attach Open Score provider behavior.
# Uses only deterministic Go tests and synthetic httptest/mock fixtures.
# No hosted Attach endpoint, registry, Socket, or other live provider is called.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DOGFOOD_TMP="${DOGFOOD_TMP:-$(mktemp -d)}"
GO_MOD_CACHE="${GOMODCACHE:-$(go env GOMODCACHE)}"
GO_BUILD_CACHE="${GOCACHE:-$(go env GOCACHE)}"
mkdir -p "$DOGFOOD_TMP/home/.attach-guard"
export HOME="$DOGFOOD_TMP/home"
export GOMODCACHE="$GO_MOD_CACHE"
export GOCACHE="$GO_BUILD_CACHE"
export ATTACH_GUARD_LOG_PATH="$HOME/.attach-guard/audit.jsonl"
unset SOCKET_API_TOKEN
unset ATTACH_GUARD_PROVIDER

OPEN_SCORE_DOGFOOD_PATTERN='OpenScore|ProviderVerdict|ProviderUnavailable|ProviderOutage'

echo "attach-guard Open Score dogfood smoke"
echo "repo: $ROOT"
echo "audit log: $ATTACH_GUARD_LOG_PATH"
echo "pattern: $OPEN_SCORE_DOGFOOD_PATTERN"

go test ./internal/policy ./internal/versionselect ./internal/cli ./e2e -run "$OPEN_SCORE_DOGFOOD_PATTERN"

if [[ "${ATTACH_GUARD_DOGFOOD_FULL:-0}" == "1" ]]; then
  env -u ATTACH_GUARD_LOG_PATH go test ./...
fi

echo "dogfood smoke complete"
