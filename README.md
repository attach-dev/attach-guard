# attach-guard

Supply chain security plugin for Claude Code. Blocks compromised packages before they're installed.

https://github.com/user-attachments/assets/26f7cd85-c482-48fe-842a-ec389e5fd21d

## The Problem

Claude Code installs packages on your behalf — often without you reviewing each one. Existing security tools scan after the fact or rely on advisory prompts that Claude can skip. There is no open-source guardrail that sits directly in front of package install commands and blocks risky packages before they execute.

## What attach-guard Does

attach-guard is a Claude Code plugin that intercepts package installation commands and evaluates them against policy **before execution**. It is not an advisory scanner. It is a hard enforcement boundary.

- Installs as a Claude Code plugin — no manual hook configuration needed
- Intercepts `npm install`, `pnpm add`, `pip install`, `go get`, and `cargo add` commands via PreToolUse hooks
- Checks package scores, age, and alerts via Socket.dev
- Denies known malware and low-score packages automatically
- Asks for confirmation on gray-band packages
- Rewrites unpinned installs to safe pinned versions when possible
- Fails closed when the provider is unavailable
- Logs every decision to a local JSONL audit trail

## Smart Version Replacement: Block Without Breaking Flow

Most security tools just say "no." attach-guard says "no, but here's a safe alternative."

When a risky version is blocked, attach-guard finds the newest version that passes policy and offers it as a replacement. Claude sees the safe alternative and can proceed immediately — your flow doesn't stop, it gets redirected to a safe path.

**npm** — axios v1.14.1 and v0.30.4 were [compromised versions](https://socket.dev/blog/axios-npm-account-compromise) published via a hijacked maintainer account:

```
> npm install axios

attach-guard evaluates:
  axios@1.14.1  -->  DENY (supply chain score 40, below threshold 50 — compromised version)
  axios@1.14.0  -->  ALLOW (supply chain score 71, passes all policy checks)

Result: ASK + rewritten command
  "npm install axios@1.14.0"
```

**pip** — litellm v1.82.7 and v1.82.8 were [malicious versions](https://socket.dev/npm/package/litellm) published to PyPI:

```
> pip install litellm

attach-guard evaluates:
  litellm==1.82.8  -->  DENY (compromised version)
  litellm==1.82.6  -->  ALLOW (passes all policy checks)

Result: ASK + rewritten command
  "pip install litellm==1.82.6"
```

These are real examples — attach-guard blocks compromised versions automatically based on their supply chain scores.

| Scenario | Example | Decision | What happens |
|---|---|---|---|
| Package is safe | `npm install axios@1.14.0` | **Allow** | Install proceeds normally |
| Pinned to compromised version | `pip install litellm==1.82.8` | **Deny** | Blocked — compromised version |
| Unpinned, latest is risky | `npm install axios` | **Ask + rewrite** | Safe alternative offered: `axios@1.14.0` |
| All versions fail | malware-only package | **Deny** | Blocked with clear explanation |

This works across all supported ecosystems — the rewrite uses the native pinning syntax for each:

| Ecosystem | Unpinned command | Rewritten command |
|---|---|---|
| npm / pnpm | `npm install axios` | `npm install axios@1.14.0` |
| pip | `pip install litellm` | `pip install litellm==1.82.6` |
| Go | `go get golang.org/x/net` | `go get golang.org/x/net@v0.25.0` |
| Cargo | `cargo add serde` | `cargo add serde@=1.0.200` |

Your flow only fully stops when there is genuinely no safe version to offer.

## Why a Hook, Not a Skill or MCP

attach-guard uses Claude Code **hooks** — not skills or MCP servers. The distinction matters:

- **Hooks** run automatically on every matching tool call. They enforce rules deterministically — Claude cannot skip or override them.
- **Skills** are instructions Claude follows when invoked. They guide behavior but cannot block actions.
- **MCP servers** provide advisory context. They inform but do not enforce.

Security enforcement requires interception at the tool-call boundary, before execution. Hooks are the only Claude Code extension point that guarantees this.

## Installation

### Quick Start: Claude Code Plugin

The fastest way to try attach-guard. Requires a [Socket.dev](https://socket.dev) API token (free tier available).

```bash
# Add the marketplace and install (one-time)
claude plugin marketplace add attach-dev/attach-guard
claude plugin install attach-guard@attach-dev
```

Or from within a Claude Code session:
```
/plugin marketplace add attach-dev/attach-guard
/plugin install attach-guard@attach-dev
```

During installation or enablement, Claude Code will prompt for your Socket API token (stored securely in your system keychain). Get a free token at [socket.dev](https://socket.dev).

> **If the install/enable prompt didn't appear**, re-trigger it with:
> ```bash
> claude plugin disable attach-guard@attach-dev && claude plugin enable attach-guard@attach-dev
> ```
> Or set the token as an environment variable: `export SOCKET_API_TOKEN="your-token"` in your shell profile.

The prebuilt binary is downloaded automatically for your platform. The hook, config, and skill are all registered — no further setup needed.

Once running, the plugin provides:
- **Automatic enforcement** — direct `npm install`, `pnpm add`, `pip install`, `go get`, and `cargo add` commands are intercepted and checked
- **`/explain <package>`** — look up any package's risk score, alerts, and version history

#### Local development (from source)

If you want to develop or modify attach-guard, clone the repo and load the plugin directly. Requires [Go 1.21+](https://go.dev/dl/).

```bash
git clone https://github.com/attach-dev/attach-guard.git
cd attach-guard
claude --plugin-dir ./plugin
```

The binary auto-builds from source on the first `/explain` invocation.

Local `claude --plugin-dir ./plugin` development may not run the marketplace install/enable config flow. If Claude does not inject the plugin config in this mode, export `SOCKET_API_TOKEN` manually before starting Claude Code.

### Manual Installation

For use without the plugin system, or to install the binary globally.

#### Prerequisites

- [Go 1.21+](https://go.dev/dl/) (to build from source; not needed for the plugin install above)
- A [Socket.dev](https://socket.dev) API token (free tier available)

#### Step 1: Build and install the binary

```bash
make build
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

#### Step 2: Set up your Socket API token

```bash
export SOCKET_API_TOKEN="your-token-here"
```

Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) to persist across sessions.

#### Step 3: Initialize config

```bash
attach-guard config init
# Default config written to ~/.attach-guard/config.yaml
```

This creates `~/.attach-guard/config.yaml` with sensible defaults. See [Configuration](#configuration) below to customize policy thresholds.

#### Step 4: Add the Claude Code hook

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

#### Step 5: Verify

Try installing a known-compromised version to verify attach-guard blocks it:

```
> Install axios@1.14.1

Claude: I'll install axios@1.14.1.
[attach-guard] deny: axios@1.14.1: supply chain score 40 is below minimum threshold 50
```

Then try a safe version:

```
> Install axios

Claude: I'll install axios.
[attach-guard] allow: package passes all policy checks
```

## How It Works

When Claude Code calls the Bash tool with a package install command (e.g., `npm install axios`, `pip install requests`, `go get golang.org/x/net`, `cargo add serde`):

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
# npm
attach-guard evaluate npm install axios
attach-guard evaluate npm install axios@1.14.1

# pip
attach-guard evaluate pip install litellm
attach-guard evaluate pip install litellm==1.82.8

# Go
attach-guard evaluate go get golang.org/x/net
attach-guard evaluate go get golang.org/x/net@v0.25.0

# Cargo
attach-guard evaluate cargo add serde
attach-guard evaluate cargo add serde@=1.0.200

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
  pip: true
  go: true
  cargo: true
logging:
  path: "~/.attach-guard/audit.jsonl"
```

### Environment variable overrides

- `ATTACH_GUARD_LOG_PATH` — override log path
- `ATTACH_GUARD_PROVIDER` — override provider kind

### Config precedence

Highest priority wins (later sources override earlier):

1. Built-in defaults
2. Plugin-bundled config (when installed as a plugin)
3. User-global config (`~/.attach-guard/config.yaml`)
4. Project-local config (`.attach-guard/config.yaml`)
5. Environment variables

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

When you run an unpinned supported command such as `npm install axios`, `pip install requests`, `go get golang.org/x/net`, or `cargo add serde`:
- attach-guard fetches candidate versions from the matching registry and scores them via Socket.dev
- If the latest passes policy, the command runs as-is
- If the latest fails but an older version passes, attach-guard suggests a rewrite using ecosystem-native syntax
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
  "original_command": "npm install axios@1.14.1",
  "decision": "deny",
  "reason": "axios@1.14.1: supply chain score 40 is below minimum threshold 50",
  "packages": [{"ecosystem":"npm","name":"axios","selected_version":"1.14.1","score":{"supply_chain":40,"overall":40}}],
  "provider": "socket",
  "mode": "claude"
}
```

## API Quota

attach-guard uses the [Socket.dev API](https://socket.dev) for package risk scoring. The free tier provides **500 quota units per hour**.

| Ecosystem | Endpoint | Cost per call |
|---|---|---|
| npm / pnpm | `GET /v0/npm/{name}/{version}/score` | 10 units |
| PyPI, Go, Cargo | `POST /v0/purl` (batch) | 100 units |

**What this means in practice:**
- npm packages: ~50 individual version scores per hour
- PyPI/Go/Cargo packages: ~5 batch scoring calls per hour (each batch scores up to 10 versions)
- Pinned installs (e.g. `pip install litellm==1.82.8`) use one call to score a single version
- Unpinned installs (e.g. `pip install litellm`) use one batch call to score up to 10 candidate versions

**When quota is exhausted**, scoring calls fail and attach-guard falls back to zero scores. This means:
- Pinned installs are **denied** (score 0 < threshold 50) — safe, fails closed
- Unpinned installs show "no acceptable version found" instead of offering a safe alternative — the version rewrite feature requires real scores to identify which version passes policy

To check your remaining quota:
```bash
curl -s -H "Authorization: Bearer $SOCKET_API_TOKEN" "https://api.socket.dev/v0/quota"
```

Quota resets hourly. For higher limits, see [Socket.dev pricing](https://socket.dev/pricing).

## Current Limitations

- Direct `pip` / `pip3`, `go get`, and `cargo add` are supported, but wrapper forms such as `python -m pip`, `uv pip`, `go install`, and `cargo install` are not yet guarded
- pip extras/range specs, Cargo requirement syntax, and non-semver Go queries are intentionally passed through for manual review rather than being auto-evaluated
- PyPI, Go, and Cargo scoring uses Socket's `POST /v0/purl` endpoint which has higher quota cost (100 units) compared to npm (10 units)
- No transitive dependency analysis
- No lockfile graph support
- Single provider at a time
- No org-level policy distribution
- No remote audit export
- Socket API response format may vary; adapter is based on documented endpoints

## Development

```bash
# Run all tests
make test

# Build
make build

# Evaluate a command
./attach-guard evaluate npm install lodash
```

### Plugin development

Cross-compile plugin binaries for all platforms:

```bash
make plugin-build
```

Test the plugin locally:

```bash
claude --plugin-dir ./plugin
```

## License

MIT
