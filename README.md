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
- Works inside Claude Code (via PreToolUse hooks) and in your normal shell (via shims)
- Fails closed in CI when the provider is unavailable
- Logs every decision to a local JSONL audit trail

## Why Hard Enforcement, Not Advisory MCP

Socket MCP and similar tools provide package intelligence as advisory context. attach-guard uses that intelligence as input but enforces decisions at the command execution boundary. The hook/shim blocks the install before the package manager runs. This is the difference between "here's some context about this package" and "this install is denied."

## Quickstart

### Build

```bash
go build -o attach-guard ./cmd/attach-guard
```

### Install

```bash
# Creates config, shims, and hook scripts
./attach-guard install
```

### Set up your Socket API token

```bash
export SOCKET_API_TOKEN="your-token-here"
```

### Verify

```bash
./attach-guard doctor
```

## Claude Code Setup

### Option 1: Auto-install hook

```bash
attach-guard hook install
```

This prints the Claude Code settings snippet and writes a hook script.

### Option 2: Manual

Add to `.claude/settings.json` or `.claude/settings.local.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "attach-guard hook run"
          }
        ]
      }
    ]
  }
}
```

When Claude attempts `npm install axios`, attach-guard intercepts and returns allow, ask (with rewritten command), or deny.

## Shell Setup

After `attach-guard install`, add the shim directory to your PATH:

```bash
export PATH="$HOME/.attach-guard/bin:$PATH"
```

Add this to your shell profile (`.bashrc`, `.zshrc`, etc.) to persist.

Now `npm install` and `pnpm add` commands are automatically guarded.

## CLI Commands

```
attach-guard evaluate --command "npm install axios" --mode shell --json
attach-guard hook install          # Set up Claude Code hooks
attach-guard hook run              # Run by hooks (reads stdin)
attach-guard install               # Install shims and config
attach-guard doctor                # Check environment
attach-guard config init           # Write default config
attach-guard explain npm axios 1.7.0  # Explain a package
attach-guard version
```

## Configuration

Default config location: `~/.attach-guard/config.yaml`

```yaml
mode: ask                          # ask | enforce
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

- `ATTACH_GUARD_MODE` — override mode
- `ATTACH_GUARD_LOG_PATH` — override log path
- `ATTACH_GUARD_PROVIDER` — override provider kind

### Config precedence

1. CLI flags
2. Environment variables
3. Project-local config (`.attach-guard/config.yaml`)
4. User-global config (`~/.attach-guard/config.yaml`)
5. Built-in defaults

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
- In Claude Code mode: returns `ask` with the rewritten command
- If no version passes, denies

### Failure handling

- Local interactive mode: asks on provider failure
- CI mode: denies on provider failure (fail closed)

## Audit Log

Every decision is logged to `~/.attach-guard/audit.jsonl`:

```json
{
  "timestamp": "2025-01-15T10:30:00Z",
  "user": "dev",
  "cwd": "/home/dev/project",
  "package_manager": "npm",
  "original_command": "npm install axios",
  "decision": "allow",
  "reason": "package passes all policy checks",
  "packages": [{"ecosystem":"npm","name":"axios","selected_version":"1.7.0","score":{"supply_chain":92,"overall":88}}],
  "provider": "socket",
  "mode": "shell"
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

# Run
./attach-guard evaluate --command "npm install lodash" --json
```

## License

MIT
