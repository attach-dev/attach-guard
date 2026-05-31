# How `attach run` wraps Claude Code and Codex

## TL;DR

`attach-guard run claude ...` (and `... codex ...`) is a **plain Go `exec.Command` wrapper** around the agent's own binary. It does not use the Claude Agent SDK. It does not use `claude -p` headless mode. It does not run a PTY harness or embed a terminal. Stdin, stdout, stderr, and signals pass through, so the experience inside the wrapped session is identical to running `claude` (or `codex`) directly.

The product value is in what happens **before exec**:

1. Verify the user is signed up at Attach Platform.
2. Inject hardened CLI flags so the agent's own sandbox + permission policy are turned on and cannot be bypassed for the session.
3. Load Attach Guard hooks for the session so package installs are evaluated by policy.
4. Inject environment variables so MCP servers and hooks running inside the agent know about the platform identity.

That's it. The "wrap" is not exotic — what's load-bearing is the platform identity binding and the hardened defaults.

## Mechanism, step by step

### 1. Platform setup preflight

Before exec, the wrapper reads `~/.attach/config.json` (the file written by `attach setup`) and calls Attach Platform to verify the credential. If the file is missing or the credential is rejected, the wrapper prints:

```
Run `attach setup` before `attach-guard run`, then retry.
```

…and exits non-zero. This is the "sign up first" gate. No anonymous use.

Why the gate is mandatory: Attach Guard's scoring (Attach Open Score) is served through Attach Platform under the user's account. Even on the free tier, every score lookup is bound to a verified identity so the platform can attribute decisions, apply per-account rate limits, and later surface traces and policy in the dashboard. Sign-up is the price of admission, not the paywall.

Code: `internal/platform/setup.go`, `preflightAttachSetup` in `cmd/attach-guard/main.go`.

### 2. Hardened CLI flags injected into argv

`attach-guard run` builds a hardened argv and execs the agent binary directly. The wrap leans on each agent's already-shipping policy surface — both Claude Code and Codex expose flags for sandbox and permission policy, so no SDK is needed.

#### Claude Code

The wrapper injects `--settings <json>` containing:

- **Sandbox**
  - `sandbox.enabled: true`
  - `sandbox.failIfUnavailable: true` (the session refuses to start if the OS can't provide the sandbox)
  - `sandbox.autoAllowBashIfSandboxed: false`
  - `sandbox.allowUnsandboxedCommands: false`
- **Permissions**
  - `permissions.deny`: `WebFetch`, `WebSearch`, `Read(./.env)`, `Read(./.env.*)`, `Read(./secrets/**)`, `Bash(curl *)`, `Bash(wget *)`
  - `permissions.defaultMode: default`
  - `permissions.disableBypassPermissionsMode: disable`
- `disableAutoMode: disable`

It also adds `--permission-mode default` if the user did not supply one.

If no `--plugin-dir` was supplied, the wrapper looks for a local/source Attach Guard Claude plugin and injects `--plugin-dir <path>` when `plugin/.claude-plugin/plugin.json` is present. This makes local wrapped runs use the current plugin manifest and hook command instead of whatever version happens to be cached from the Claude Code marketplace. Set `ATTACH_GUARD_CLAUDE_PLUGIN_DIR=/path/to/plugin` to choose a plugin explicitly, or `ATTACH_GUARD_CLAUDE_PLUGIN_DIR=off` to skip plugin-dir injection.

The wrapper **refuses to start** if the user passes:

- `--dangerously-skip-permissions`
- their own `--settings` (would let the user override the hardened policy)
- `--permission-mode` with anything other than `default` or `plan`

#### Codex

The wrapper injects (only if the user hasn't already set them):

- `--sandbox workspace-write`
- `--ask-for-approval on-request`
- `-c sandbox_workspace_write.network_access=false`
- `-c features.hooks=true`
- `-c 'hooks.PreToolUse=[...]'` pointing at the current `attach-guard hook codex` binary

The wrapper **refuses to start** if the user passes:

- `--dangerously-bypass-approvals-and-sandbox`
- `--yolo`
- `--sandbox danger-full-access`
- a `-c` override that re-enables network access in the workspace-write sandbox

If the user already supplied Codex hook configuration through `-c hooks.*` or `-c features.hooks=...`, the wrapper preserves it and does not inject its own PreToolUse config. That keeps explicit user configuration predictable while making the default `attach-guard run codex` path actually load the Guard hook.

Code: `wrappedAgentCommand`, `hardenedClaudeArgs`, `hardenedCodexArgs`, `validateClaudeRunArgs`, `validateCodexRunArgs` in `cmd/attach-guard/main.go`.

### 3. Hooks loaded for the session

Hook loading is also part of argv construction:

- Claude Code gets `--plugin-dir <path>` when the local/source plugin manifest is present and the user did not supply a plugin dir.
- Codex gets inline `features.hooks=true` and `hooks.PreToolUse=[...]` config when the user did not supply hook config.

These are session-scoped. The wrapper does not install, update, or mutate global plugin/cache state.

### 4. Platform env vars injected

Both wrapped sessions get these in their environment:

| Variable | Purpose |
|---|---|
| `ATTACH_API_URL` | Attach Platform base URL the session should talk to |
| `ATTACH_API_KEY` | Scoped runtime credential redeemed by `attach setup` |
| `ATTACH_SCORE_API_URL` | Attach Score endpoint (same platform host today) |
| `ATTACH_SCORE_API_KEY` | Score credential |
| `ATTACH_RUNTIME_KIND` | `claude_code` or `codex` |
| `ATTACH_GUARD_ACTIVE` | `1` — signals the session is under `attach run` |
| `ATTACH_GUARD_AGENT_COMMAND` | The agent binary being wrapped |
| `ATTACH_GUARD_PLATFORM_SETUP` | `1` — preflight succeeded |

MCP servers running inside the agent (Attach Files, Attach Score, future modules) read these to bind their requests to the platform identity established by `attach setup`. No tokens are printed to the terminal.

Code: `guardedAgentEnv` in `cmd/attach-guard/main.go`.

### 5. Exec, with stdio + signal passthrough

```go
cmd := exec.Command(argv[0], argv[1:]...)
cmd.Env = env
cmd.Stdin = stdin
cmd.Stdout = stdout
cmd.Stderr = stderr
```

`SIGINT` is forwarded to the child. The wrapper waits for exit and propagates the child's exit code.

There is no PTY allocation, no subprocess management framework, no message bus between the wrapper and the agent. The agent is the foreground process for the rest of the session.

Code: `executeAgent` in `cmd/attach-guard/main.go`.

## FAQ

**Did you use the Claude Agent SDK?**
No. The wrap doesn't need to interpret the agent's tool calls or messages — it only needs to control the agent's startup configuration. Claude Code's `--settings` flag and Codex's `--sandbox` / `--ask-for-approval` / `-c` flags already expose everything required. SDK integration would be over-engineering for this layer.

**Is it `claude -p` headless mode?**
No. `attach run claude` gives you the full interactive Claude Code TUI. The wrap is invisible once the session starts.

**Do you embed a terminal inside an app?**
No. `attach run` is a CLI command. It execs the agent in your existing terminal.

**How is this different from running `claude` directly with a custom `--settings` file?**
Three things the wrapper enforces that a hand-written `--settings` cannot:
1. **Bypass refusal.** The wrapper rejects flags like `--dangerously-skip-permissions` before exec, so the user cannot opt out of the policy at invocation time.
2. **Platform binding.** The wrapper attaches a verified Attach Platform identity to the session via env, so in-runtime modules know who the user is and can talk to the platform with scoped credentials.
3. **Preflight gate.** No sign-up at Attach Platform → no run.

**What happens if Attach Platform is unreachable?**
The preflight fails and the agent does not start. This is intentional. The platform binding is a precondition, not an enhancement.

**Can I still pass my own flags to `claude` or `codex`?**
Yes — anything that doesn't try to disable the hardened policy is appended after the injected flags.

```bash
attach-guard run claude --model claude-sonnet-4-7
attach-guard run codex --model gpt-5
```

**What about `attach run --dry-run`?**
`attach-guard run --dry-run claude ...` prints the exact hardened command the wrapper would exec, without running it or contacting the platform. Useful for inspection and CI.

## Where this fits in the product

`attach run` is the per-session entry point that ties the Attach Platform identity to the agent runtime and enforces safe defaults. The capabilities the user actually sees (Attach Files handoff, Attach Score-driven package decisions, and the planned Memory / Secrets / Sandbox-governance modules) flow in through MCP servers and hooks that were installed once by `attach setup` and now know how to talk to the platform because of the env injected here.

The full module set and per-module state (`active` / `ready` / `planned`) is in `attach-platform/docs/plans/2026-05-30-runtime-module-contracts.md`.
