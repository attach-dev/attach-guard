# attach-guard

AI agents install packages. Attach checks them first.

<video src="https://github.com/user-attachments/assets/efca5e13-8e3f-44b7-9aab-c653d048ad0d" width="100%" autoplay loop muted playsinline></video>

## The Problem

Claude Code installs packages on your behalf — often without you reviewing each one. Existing security tools scan after the fact or rely on advisory prompts that Claude can skip. There is no open-source guardrail that sits directly in front of package install commands and blocks risky packages before they execute.

## What attach-guard Does

attach-guard is a Claude Code plugin that intercepts package installation commands and evaluates them against policy **before execution**. It is not an advisory scanner. It is a hard enforcement boundary.

- Installs as a Claude Code plugin — no manual hook configuration needed
- Intercepts `npm install`, `pnpm add`, direct `pip` / `pip3 install`, `python -m pip install` / `python3 -m pip install` wrappers, `uv pip install`, `go get` / `go install`, and `cargo add` / `cargo install` commands via PreToolUse hooks
- Evaluates packages with the configured provider before install commands run
- Supports explicit provider configuration, including Attach Open Score-compatible verdict endpoints and local BYO-token providers
- Denies known malware and high-confidence dangerous packages automatically
- Asks for confirmation on gray-band packages and provider-unavailable cases in local mode
- Rewrites unpinned installs to safe pinned versions when possible
- Logs every decision to a local JSONL audit trail

Attach Open Score is the first-party scoring direction. Today, `open-score` is an opt-in HTTP provider for explicitly configured Attach Open Score-compatible verdict endpoints; no hosted/default Attach scoring endpoint is baked into attach-guard. See [Attach Open Score provider semantics](docs/OPEN_SCORE_PROVIDER.md) and [Local Attach Open Score dogfood guide](docs/LOCAL_OPEN_SCORE_DOGFOOD.md) for public-safe verification.

## Smart Version Replacement: Block Without Breaking Flow

Most security tools just say "no." attach-guard says "no, but here's a safe alternative."

When a risky version is blocked, attach-guard finds the newest version that passes policy and offers it as a replacement. Claude sees the safe alternative and can proceed immediately — your flow doesn't stop, it gets redirected to a safe path.

**npm** — axios v1.14.1 and v0.30.4 were publicly reported compromised versions published via a hijacked maintainer account:

```
> npm install axios

attach-guard evaluates:
  axios@1.14.1  -->  DENY (known compromised version)
  axios@1.14.0  -->  ALLOW (passes configured policy checks)

Result: ASK + rewritten command
  "npm install axios@1.14.0"
```

**pip** — litellm v1.82.7 and v1.82.8 were malicious versions published to PyPI:

```
> pip install litellm

attach-guard evaluates:
  litellm==1.82.8  -->  DENY (compromised version)
  litellm==1.82.6  -->  ALLOW (passes all policy checks)

Result: ASK + rewritten command
  "pip install litellm==1.82.6"
```

These examples illustrate the current enforcement flow: attach-guard blocks known compromised or policy-failing versions and offers a safe pinned alternative when one is available.

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

Supported pip wrappers such as `python -m pip install`, `python -I -m pip install`, and `uv pip install` are evaluated before execution. If a wrapped pip command needs a safe-version rewrite, attach-guard asks for manual review instead of rewriting the wrapper command.

Your flow only fully stops when there is genuinely no safe version to offer.

## Why a Hook, Not a Skill or MCP

attach-guard uses Claude Code **hooks** — not skills or MCP servers. The distinction matters:

- **Hooks** run automatically on every matching tool call. They enforce rules deterministically — Claude cannot skip or override them.
- **Skills** are instructions Claude follows when invoked. They guide behavior but cannot block actions.
- **MCP servers** provide advisory context. They inform but do not enforce.

Security enforcement requires interception at the tool-call boundary, before execution. Hooks are the only Claude Code extension point that guarantees this.

## Installation

### Quick Start: Claude Code Plugin

The fastest way to try the current packaged plugin. It installs the hook and local config; provider credentials remain explicit local setup. Attach Open Score is the first-party scoring direction, and `open-score` can be configured against compatible verdict endpoints. No hosted/default Attach scoring endpoint ships in this package.

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

During installation or enablement, the current local BYO-token provider path may prompt for a provider API token (stored securely in your system keychain). That token is for explicit local provider use only.

> **If the install/enable prompt didn't appear**, re-trigger it with:
> ```bash
> claude plugin disable attach-guard@attach-dev && claude plugin enable attach-guard@attach-dev
> ```
> Or set the provider-specific token environment variable in your shell profile before starting Claude Code.

The prebuilt binary is downloaded automatically for your platform. The hook, config, and skill are all registered — no further setup needed.

Once running, the plugin provides:
- **Automatic enforcement** — `npm install`, `pnpm add`, direct and `python -m pip` / `uv pip` forms of `pip install`, `go get` / `go install`, and `cargo add` / `cargo install` commands are intercepted and checked
- **`/explain <package>`** — look up any package's risk score, alerts, and version history

#### Local development (from source)

If you want to develop or modify attach-guard, clone the repo and load the plugin directly. Requires [Go 1.21+](https://go.dev/dl/).

```bash
git clone https://github.com/attach-dev/attach-guard.git
cd attach-guard
claude --plugin-dir ./plugin
```

The binary auto-builds from source on the first `/explain` invocation.

Local `claude --plugin-dir ./plugin` development may not run the marketplace install/enable config flow. If Claude does not inject the plugin config in this mode, configure any required provider token in your shell before starting Claude Code.

### Manual Installation

For use without the plugin system, or to install the binary globally.

#### Prerequisites

- [Go 1.21+](https://go.dev/dl/) (to build from source; not needed for the plugin install above)
- A provider API token only when using an explicit BYO-token local provider path

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

#### Step 2: Configure provider credentials if needed

Provider setup is explicit: BYO-token local providers require their provider-specific token environment variable, while Attach Open Score-compatible HTTP endpoints require `provider.kind: open-score` plus an `endpoint`.

#### Step 3: Initialize config

```bash
attach-guard config init
# Default config written to ~/.attach-guard/config.yaml
```

This creates `~/.attach-guard/config.yaml` with compatibility defaults. Review [Configuration](#configuration) before relying on provider behavior; no hosted/default Attach scoring endpoint is shipped in this package.

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
[attach-guard] deny: axios@1.14.1: known compromised version
```

Then try a safe version:

```
> Install axios

Claude: I'll install axios.
[attach-guard] allow: package passes all policy checks
```

## How It Works

When Claude Code or Codex calls the Bash tool with a package install command (e.g., `npm install axios`, `pip install requests`, `python -I -m pip install requests`, `go get golang.org/x/net`, `cargo add serde`):

1. The runtime fires the PreToolUse hook before execution
2. The hook pipes the tool input JSON to `attach-guard hook` via stdin
3. attach-guard parses the command, evaluates packages against policy
4. Returns a `hookSpecificOutput` JSON response:
   - `permissionDecision: "allow"` — install proceeds
   - `permissionDecision: "ask"` — Claude shows the reason and asks the user
   - `permissionDecision: "deny"` — install is blocked, reason shown to the runtime
5. On internal errors, exits with code 2 (blocking) to fail closed

Codex PreToolUse does not support `permissionDecision: "ask"` today. In the Codex adapter, Attach Guard maps manual-review `ask` decisions to `deny` so the package install does not proceed silently.

## CLI Commands

```
attach-guard evaluate <command>    Evaluate a package manager command against policy
attach-guard hook [run|claude|codex]
                                   Read runtime hook JSON from stdin and respond
attach-guard run [--dry-run] <claude|codex> [args...]
                                   Run an agent after Attach Platform setup preflight and runtime hardening
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
attach-guard evaluate python -I -m pip install litellm

# Go
attach-guard evaluate go get golang.org/x/net
attach-guard evaluate go get golang.org/x/net@v0.25.0

# Cargo
attach-guard evaluate cargo add serde
attach-guard evaluate cargo add serde@=1.0.200

# Use as a Claude Code hook (reads JSON from stdin)
attach-guard hook

# Use as a Codex hook (reads JSON from stdin)
attach-guard hook codex

# Preview supported agent commands without starting the agent or checking setup
attach-guard run --dry-run claude
attach-guard run --dry-run codex
attach-guard run --dry-run claude --model sonnet
attach-guard run --dry-run codex --sandbox read-only "Review this diff"

# Run supported agents after `attach setup` has created ~/.attach/config.json
attach-guard run claude
attach-guard run codex
```

`attach-guard run` applies runtime-native hardening before exec:

- Codex runs with `--sandbox workspace-write`, `--ask-for-approval on-request`, and command network access disabled unless the user supplies an explicit supported setting.
- Codex `danger-full-access`, `--yolo`, and explicit command network enablement are rejected.
- Claude Code runs with session settings that enable the native Bash sandbox, fail closed if that sandbox is unavailable, select default permission mode, disable bypass/auto permission modes, and deny common direct web/secrets access rules.
- Claude Code `bypassPermissions`, `acceptEdits`, `auto`, `dontAsk`, `--dangerously-skip-permissions`, and user-supplied `--settings` are rejected by the wrapper. This uses Claude Code's native sandbox/settings surface; it is still not an Attach Platform or Gateway sandbox claim.

## Configuration

Default config location: `~/.attach-guard/config.yaml`

`attach-guard config init` currently writes a compatibility config for the local BYO-token provider path. That compatibility default is not hosted/default Attach scoring; use it only with an explicit local provider token. Attach Open Score is the first-party scoring direction, but no hosted/default Attach scoring endpoint is shipped in this package.

```yaml
provider:
  kind: socket                     # explicit local BYO-token provider
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
  unknown_behavior:
    local: ask                     # Open Score UNKNOWN: ask | deny | allow
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

Opt in to an Attach Open Score-compatible HTTP verdict endpoint explicitly:

```yaml
provider:
  kind: open-score
  endpoint: http://127.0.0.1:8757/v0/verdict
  timeout_seconds: 5
```

The `open-score` provider posts only public package coordinates (`ecosystem`, `name`, `version`) and consumes verdict fields (`decision`, optional `score`, optional `confidence`, `reasons`, summarized `source_refs`). Provider failures and malformed responses map to `UNKNOWN`/provider-unavailable behavior, never `ALLOW`.

For BYO-token local provider configuration, see [Advanced: BYO-token local providers](#advanced-byo-token-local-providers).

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
5. Use Attach Open Score-compatible provider verdicts when present (`ALLOW`, `ASK`, `DENY`, `UNKNOWN`)
6. Deny scores below hard threshold (supply chain < 50) for legacy score providers
7. Ask on gray-band scores (50-70)
8. Ask on critical/high alerts
9. Allow everything else

### Unpinned version handling

When you run an unpinned supported command such as `npm install axios`, `pip install requests`, `python -m pip install requests`, `go get golang.org/x/net`, or `cargo add serde`:
- attach-guard fetches candidate versions from the configured provider and evaluates them against policy
- If the latest passes policy, the command runs as-is
- If the latest fails but an older version passes, direct package-manager command shapes get a rewrite suggestion using ecosystem-native syntax
- In Claude Code mode, direct rewrites return `ask` with the rewritten command via `updatedInput`
- In Codex mode, manual-review `ask` decisions are mapped to `deny` because Codex PreToolUse does not support `ask` today
- Wrapped pip commands are evaluated, but wrapper-preserving rewrites are not emitted yet; commands such as `python -m pip install requests` or `uv pip install requests` ask for manual review when a rewrite would be required
- If no version passes, denies

Attach Open Score integration should be verdict-first: `ALLOW` → allow, `ASK` → ask, `DENY` → deny, and `UNKNOWN` → ask/warn locally by default. CI/team policy may map unknowns to deny by explicit configuration. See [Attach Open Score provider semantics](docs/OPEN_SCORE_PROVIDER.md).

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
  "original_command": "npm install example-malware@1.0.0",
  "decision": "deny",
  "reason": "example-malware@1.0.0: known malware alert",
  "packages": [{"ecosystem":"npm","name":"example-malware","selected_version":"1.0.0","score":{"supply_chain":0,"overall":0},"provider_verdict":{"decision":"DENY","risk_score":95,"reasons":["known-malware-synthetic"],"source_refs":["osv:GHSA-0000-0000-0000"]},"alerts":[{"severity":"critical","title":"known malware","category":"malware"}]}],
  "provider": "mock",
  "mode": "claude"
}
```

## Advanced: BYO-token local providers

The current Socket.dev adapter is available only as an explicit local BYO-token provider. Socket data must not be used as default Attach scoring or as an Attach Open Score input/source unless a future written partnership and policy explicitly permit it.

```yaml
provider:
  kind: socket
  api_token_env: SOCKET_API_TOKEN
```

```bash
export SOCKET_API_TOKEN="your-token-here"
```

Provider calls can fail when a local BYO-token account is unavailable, rate-limited, or out of quota. Provider startup/unavailability follows `provider_unavailable_behavior`; local defaults to ask/warn, and CI/team fail-closed behavior requires explicit configuration such as `provider_unavailable_behavior.ci: deny`. Individual scoring errors can still block a pinned install, and unpinned rewrites need real provider results to identify a version that passes policy.

## Current Limitations

- Direct `pip` / `pip3`, `python -m pip` / `python3 -m pip` wrappers with safe interpreter flags before `-m`, `uv pip`, `go get` / `go install`, and `cargo add` / `cargo install` are supported
- Rewrites are emitted only for direct package-manager command shapes; wrapped pip commands are evaluated but ask for manual review when a safe-version rewrite is needed
- `uv add` / `uv sync`, pip extras/range/constraint-aware handling, Cargo requirement syntax, and non-semver Go queries are intentionally deferred to manual review rather than being auto-evaluated
- Default hosted scoring is not baked into this package; today, Attach Open Score-compatible verdicts require explicit `open-score` endpoint configuration or platform-issued credentials from `attach setup`
- Local BYO-token providers can be affected by upstream availability, rate limits, or quota during evaluation and version rewrite
- No transitive dependency analysis
- No lockfile graph support
- Single provider at a time
- No org-level policy distribution
- No remote audit export
- Vendor API response formats may vary for local BYO-token providers; adapters are based on documented endpoints

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
