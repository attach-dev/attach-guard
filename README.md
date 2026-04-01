# attach-guard

Hard-enforcement dependency install guard for AI coding agents and developers.

## The Problem

AI coding agents and developers install packages before anyone reviews them. Existing tools scan after the fact or rely on advisory prompts. There is no open-source, local-first guardrail that sits directly in front of `npm install` and blocks risky packages before they execute.

## What attach-guard Does

attach-guard intercepts package installation commands and evaluates them against policy **before execution**. It is not an advisory scanner. It is a hard enforcement boundary.

- Intercepts `npm install`, `npm i`, `pnpm add` commands
- Checks package scores, age, and alerts via a pluggable risk provider
- Denies known malware and low-score packages
- Asks for confirmation on gray-band packages
- Rewrites unpinned installs to safe pinned versions when possible
- Works inside Claude Code (via PreToolUse hooks)
- Fails closed in CI when the provider is unavailable
- Logs every decision to a local JSONL audit trail

## Smart Version Replacement: Block Without Breaking Flow

Most security tools just say "no." attach-guard says "no, but here's a safe alternative."

When a risky version is blocked, attach-guard doesn't stop the developer — it finds the newest version that passes policy and offers it as a replacement:

```
> npm install new-pkg

attach-guard evaluates:
  new-pkg@2.0.0  -->  FAIL (published 1 hour ago, minimum age is 48 hours)
  new-pkg@1.9.0  -->  PASS (30 days old, supply chain score 92)

Result: ASK + rewritten command
  "npm install new-pkg@1.9.0"
```

In Claude Code, this means Claude sees the safe alternative and can proceed immediately. The developer flow doesn't stop — it gets redirected to a safe path.

| Scenario | Decision | What happens |
|---|---|---|
| Package is safe | **Allow** | Install proceeds normally |
| Latest is risky, older version is safe | **Ask + rewrite** | Claude shows safe alternative, user confirms |
| All versions fail (malware, all too new, etc.) | **Deny** | Blocked with clear explanation |

Your flow only fully stops when there is genuinely no safe version to offer. In the common case — a package that's just too new or has a recent score drop — you get a one-click safe alternative.

## Why a Hook, Not a Skill or MCP

attach-guard is a Claude Code **hook**, not a skill or MCP server. The distinction matters:

- **Hooks** run automatically on every matching tool call. They enforce rules deterministically — Claude cannot skip or override them.
- **Skills** are instructions Claude follows when invoked. They guide behavior but cannot block actions.
- **MCP servers** provide advisory context. They inform but do not enforce.

A security guardrail must be a hook because enforcement requires interception at the tool-call boundary, before execution.

## Installation

### Prerequisites

- [Go 1.21+](https://go.dev/dl/) (to build from source)
- A [Socket.dev](https://socket.dev) API token (free tier available)

### Step 1: Build and install the binary

```bash
go build -o attach-guard ./cmd/attach-guard
```

Move the binary somewhere on your PATH:

```bash
# Option A: Move to a standard location
sudo mv attach-guard /usr/local/bin/

# Option B: Move to a user-local bin directory
mkdir -p ~/.local/bin
mv attach-guard ~/.local/bin/
# Make sure ~/.local/bin is in your PATH (add to ~/.bashrc or ~/.zshrc):
# export PATH="$HOME/.local/bin:$PATH"
```

Verify it works:

```bash
attach-guard version
# attach-guard v0.1.0
```

### Step 2: Set up your Socket API token

```bash
export SOCKET_API_TOKEN="your-token-here"
```

Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) to persist across sessions.

### Step 3: Initialize config

```bash
attach-guard config init
# Default config written to ~/.attach-guard/config.yaml
```

This creates `~/.attach-guard/config.yaml` with sensible defaults. See [Configuration](#configuration) below to customize policy thresholds.

### Step 4: Add the Claude Code hook

Add the following to your project's `.claude/settings.json` (shared with team) or `.claude/settings.local.json` (personal, gitignored):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "attach-guard hook"
          }
        ]
      }
    ]
  }
}
```

For global protection across all projects, add it to `~/.claude/settings.json` instead.

### Step 5: Verify

Ask Claude Code to install a package. You should see attach-guard intercept the command:

```
> Install axios for me

Claude: I'll install axios.
[attach-guard] allow: package passes all policy checks
```

If the package is risky, Claude will be blocked or asked to confirm.

## How It Works

When Claude calls the Bash tool with a command like `npm install axios`:

1. Claude Code fires the PreToolUse hook before execution
2. The hook pipes the tool input JSON to `attach-guard hook` via stdin
3. attach-guard parses the command, evaluates packages against policy
4. Returns a `hookSpecificOutput` JSON response:
   - `permissionDecision: "allow"` — install proceeds
   - `permissionDecision: "ask"` — Claude shows the reason and asks the user
   - `permissionDecision: "deny"` — install is blocked, reason shown to Claude
5. On internal errors, exits with code 2 (blocking) to fail closed

## CLI Commands

```
attach-guard evaluate <command>    Evaluate a package manager command against policy
attach-guard hook [run]            Read Claude Code hook JSON from stdin and respond
attach-guard config init           Write default config to ~/.attach-guard/config.yaml
attach-guard version               Print version
attach-guard help                  Show help
```

### Examples

```bash
# Evaluate a command directly
attach-guard evaluate npm install axios

# Use as a Claude Code hook (reads JSON from stdin)
attach-guard hook
```

## Configuration

Default config location: `~/.attach-guard/config.yaml`

```yaml
provider:
  kind: socket                     # risk intelligence provider
  api_token_env: SOCKET_API_TOKEN
policy:
  deny_known_malware: true
  min_supply_chain_score: 70       # hard allow threshold
  min_overall_score: 70
  gray_band_min_supply_chain_score: 50  # hard deny below this
  minimum_package_age_hours: 48    # deny versions newer than this
  provider_unavailable_behavior:
    local: ask                     # ask | deny | allow
    ci: deny
  auto_rewrite_unpinned:
    local: false                   # auto-pin to safe version?
    ci: false
  allowlist: []                    # always allow these packages
  denylist: []                     # always deny these packages
package_managers:
  npm: true
  pnpm: true
logging:
  path: "~/.attach-guard/audit.jsonl"
```

### Environment variable overrides

- `ATTACH_GUARD_LOG_PATH` — override log path
- `ATTACH_GUARD_PROVIDER` — override provider kind

### Config precedence

1. Environment variables
2. Project-local config (`.attach-guard/config.yaml`)
3. User-global config (`~/.attach-guard/config.yaml`)
4. Built-in defaults

## Policy Model

### Decision flow

1. Check allowlist/denylist
2. Check provider availability
3. Deny known malware
4. Deny versions under minimum age (48 hours default)
5. Deny scores below hard threshold (supply chain < 50)
6. Ask on gray-band scores (50-70)
7. Ask on critical/high alerts
8. Allow everything else

### Unpinned version handling

When you run `npm install axios` (no version pin):
- attach-guard fetches candidate versions
- If the latest passes policy, the command runs as-is
- If the latest fails but an older version passes, attach-guard suggests a rewrite: `npm install axios@1.6.8`
- In Claude Code mode: returns `ask` with the rewritten command via `updatedInput`
- If no version passes, denies

### Failure handling

- Local/interactive mode: asks on provider failure
- CI mode: denies on provider failure (fail closed)
- Internal errors in hook mode: exit code 2 (blocks the install)

## Audit Log

Every decision is logged to `~/.attach-guard/audit.jsonl`:

```json
{
  "timestamp": "2026-01-15T10:30:00Z",
  "user": "dev",
  "cwd": "/home/dev/project",
  "package_manager": "npm",
  "original_command": "npm install axios",
  "decision": "allow",
  "reason": "package passes all policy checks",
  "packages": [{"ecosystem":"npm","name":"axios","selected_version":"1.7.0","score":{"supply_chain":92,"overall":88}}],
  "provider": "socket",
  "mode": "claude"
}
```

## Current Limitations

- npm and pnpm only (no yarn, pip, uv yet)
- No transitive dependency analysis
- No lockfile graph support
- Single provider at a time
- No org-level policy distribution
- No remote audit export
- Socket API response format may vary; adapter is based on documented endpoints

## Development

```bash
# Run all tests
go test ./...

# Build
go build -o attach-guard ./cmd/attach-guard

# Evaluate a command
./attach-guard evaluate npm install lodash
```

## License

MIT
