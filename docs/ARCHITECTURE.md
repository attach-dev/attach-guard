# Architecture

## Overview

attach-guard is a single Go binary that interposes between package manager commands and their execution. It evaluates install commands against policy and returns allow/ask/deny decisions.

## Interception Model

### Claude Code Hooks

```
Claude Code → PreToolUse Hook → attach-guard hook → evaluate → hook response JSON
```

1. Claude Code calls a Bash tool with a command like `npm install axios`
2. The PreToolUse hook fires and pipes the hook JSON to `attach-guard hook`
3. attach-guard reads the hook input, extracts the command
4. If it's not an install command, returns allow (passthrough)
5. If it is, runs the evaluation pipeline
6. Returns hook output JSON with `hookSpecificOutput.permissionDecision`, `permissionDecisionReason`, and optionally `updatedInput`

## Data Flow

```
raw command string
    │
    ▼
┌─────────┐
│  Parser  │ ── unwraps prefixes (sudo, env, VAR=val), extracts PM, action, packages, flags
└────┬────┘
     │
     ▼
┌──────────────────┐
│  Version Selector │ ── for unpinned packages, fetches candidates via provider
└────────┬─────────┘
         │
         ▼
┌──────────┐
│  Provider │ ── Socket adapter fetches scores, versions, alerts
└────┬─────┘
     │
     ▼
┌─────────────┐
│ Policy Engine│ ── evaluates each package against thresholds, alerts, age
└──────┬──────┘
       │
       ▼
┌──────────┐
│  Rewriter │ ── if needed, rewrites command with pinned versions
└────┬─────┘
     │
     ▼
┌─────────┐
│  Logger  │ ── appends JSONL audit entry
└────┬────┘
     │
     ▼
  result (allow/ask/deny + reason + optional rewritten command)
```

## Provider Abstraction

The `provider.Provider` interface is the boundary between attach-guard and risk intelligence sources:

```go
type Provider interface {
    Name() string
    GetPackageScore(ctx, ecosystem, name, version) (*VersionInfo, error)
    ListVersions(ctx, ecosystem, name) ([]VersionInfo, error)
    IsAvailable(ctx) bool
}
```

The Socket adapter normalizes Socket API responses into internal `VersionInfo` and `PackageScore` types. No Socket-specific types leak into the policy engine.

To add a new provider:
1. Implement the `Provider` interface
2. Register it in the provider factory (currently in `cmd/attach-guard/main.go`)
3. Add the provider kind to config

## Policy Flow

The policy engine evaluates each package independently and takes the worst decision across all packages in a command.

Decision priority (highest to lowest):
1. Allowlist match → allow immediately
2. Denylist match → deny immediately
3. Provider unavailable → mode-dependent (deny in CI, ask locally)
4. Malware alert → deny
5. Below minimum age → deny
6. Below hard score threshold → deny
7. In gray band → ask
8. Critical/high alerts → ask
9. Everything else → allow

## Version Selection

For unpinned packages (`npm install axios` with no `@version`):

1. Fetch all versions from the provider, sorted newest-first
2. Skip deprecated versions
3. First pass: find the newest version that gets Allow
4. Second pass: if no Allow found, find the newest version that gets Ask
5. If the selected version is not the latest, mark it as a rewrite

The rewrite decision depends on mode:
- Claude Code: return `ask` with `updatedInput` containing the rewritten command
- Shell with auto-rewrite: return `allow` with rewritten command
- Shell without auto-rewrite: return `ask` with suggestion
- CI: deny unless auto-rewrite is explicitly enabled

## Hook Integration

Claude Code hook input:
```json
{"session_id":"...","tool_name":"Bash","tool_input":{"command":"npm install axios"}}
```

Hook output uses the `hookSpecificOutput` contract:
```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"npm install axios@1.6.8"}}}
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"...","updatedInput":{"command":"npm install axios@1.6.8"}}}
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"..."}}
```

## Failure Modes

| Scenario | CI | Local |
|---|---|---|
| Provider unavailable | deny | ask |
| Provider returns error for specific package | error propagated | error propagated |
| No versions found | deny | deny |
| All versions fail policy | deny | deny |
| Config file missing | use defaults | use defaults |
| Config file invalid | error | error |

## Package Structure

```
cmd/attach-guard/     CLI entry point
internal/
  cli/                Evaluate command logic
  config/             Config loading and merging
  parser/             Command parsing (with prefix unwrapping)
    npm/              npm-specific parser
    pnpm/             pnpm-specific parser
    spec/             Shared package spec parsing
  provider/           Provider interface + mock
    socket/           Socket.dev adapter
  policy/             Policy engine
  rewrite/            Command rewriting
  versionselect/      Version selection for unpinned packages
  hook/claude/        Claude Code hook I/O
  audit/              JSONL audit logging
  execx/              Safe subprocess execution
  envdetect/          Environment detection (CI, etc.)
pkg/api/              Public domain types
```
