# Attach Guard — Setup Modes

Attach Guard scores packages through **Attach Open Score**. There are two
provider wirings and two onboarding modes built on top of them.

## Provider wirings (the building blocks)

| Wiring | `provider.kind` | Token? | Network? | Config keys |
|--------|-----------------|--------|----------|-------------|
| Hosted HTTP | `open-score` | **yes** (Bearer) | yes → `score.attach.dev` | `endpoint`, `api_token_env` |
| Local command | `open-score` | no | no | `command` (defaults to `attach-open-score`) |

Mode A and Mode B below both target the **Hosted HTTP** wiring. The local
command wiring is the no-token fallback.

---

## Mode A — Managed setup (`attach setup` via Attach Platform)

Goal: the user never copies a token by hand. `attach setup` authenticates with
Attach Platform, which provisions an Open Score token and writes the Guard
config for them.

### End-user steps
1. Run `attach setup`.
2. Authenticate with Attach Platform when prompted (browser / SSO).
3. That's it — setup provisions a token, writes the Guard config, and wires the
   shell environment automatically.
4. Verify: `attach guard status` (or trigger any install and watch it score).

### What `attach setup` configures under the hood
- `~/.attach-guard/config.yaml`:
  ```yaml
  provider:
    kind: open-score
    endpoint: https://score.attach.dev/v0/verdict
    api_token_env: ATTACH_OPEN_SCORE_API_TOKEN
  ```
- Token written to `~/.attach-guard/score.env` (mode `0600`) as
  `ATTACH_OPEN_SCORE_API_TOKEN=…`. The plugin reads this file as data; it does
  not source or execute it.

### To build (not yet implemented)
- **Attach Platform: token issuance** — an endpoint that mints a per-user/org
  Open Score token (scoped, revocable). `attach setup` calls it after auth.
- **Open Score service: multi-token validation** — today the service checks a
  single shared static Bearer token (`ATTACH_OPEN_SCORE_API_TOKEN` on the box).
  Managed mode needs it to accept platform-issued tokens (verify against the
  platform, or accept a managed key set). **This is the key backend gap for
  Mode A** — without it, every managed user shares one token.
- **`attach setup`**: write config + `score.env` + shell wiring (the same
  artifacts created manually in Mode B).

---

## Mode B — Standalone plugin (paste token at prompt)

Goal: someone installs the Claude Code plugin directly, with no platform. The
plugin prompts for a token on enable; the user pastes one in.

### End-user steps
1. Add the marketplace and install the plugin in Claude Code:
   ```
   /plugin marketplace add attach-dev/attach-guard
   /plugin install attach-guard@attach-dev
   ```
2. When prompted for **"Attach Open Score API Token"**, obtain a token:
   - from the Attach Platform dashboard (self-service), **or**
   - self-host Open Score (`attach-open-score serve --auth-token <your-token>`)
     and use your own token, **or**
   - ask your Attach admin for one.
3. Paste the token at the prompt. The plugin stores it and starts scoring
   against `https://score.attach.dev/v0/verdict`.

### No-token alternative (fully local, no network)
Skip the token entirely and run Open Score locally:
1. Install the `attach-open-score` binary on your `PATH`.
2. Leave the plugin on its local-command default
   (`provider.kind: open-score`, `provider.command: attach-open-score`).
Guard scores packages on-device with no token and no network.

### How it is wired (implemented)
The plugin defaults to the **local command** provider and is *promoted* to the
hosted endpoint only when a token is present — so no Go selection-logic change
was needed (Guard already uses the HTTP provider when `endpoint` is set and the
local `command` when it is not).

- **`plugin/.claude-plugin/plugin.json`** — `userConfig` prompts for
  `attach_open_score_api_token` (sensitive).
- **`plugin/config/config.yaml`** — default provider keeps `command:
  attach-open-score` (local) and declares `api_token_env:
  ATTACH_OPEN_SCORE_API_TOKEN`. No `endpoint` by default.
- **`plugin/hooks/bootstrap.sh`** — resolves the token
  (env → plugin prompt → `~/.attach-guard/score.env`) and, when a token exists
  and no endpoint is already set, exports
  `ATTACH_OPEN_SCORE_ENDPOINT=https://score.attach.dev/v0/verdict`. That env
  override flips the provider to hosted HTTP for that run; with no token the
  endpoint stays unset and Guard uses the local command.

Net behavior:
- Token pasted at prompt (or in `score.env`) → **hosted** `score.attach.dev`.
- No token, `attach-open-score` on PATH → **local**, no network.
- `ATTACH_OPEN_SCORE_ENDPOINT` pre-set (self-hosted) → that endpoint is used
  as-is (token sent if present).

---

## Token resolution & precedence (both modes)

The HTTP provider sends `Authorization: Bearer <token>` where the token comes
from the env var named by `provider.api_token_env`. Resolution order:

1. `ATTACH_OPEN_SCORE_API_TOKEN` already in the environment (CI secrets, etc.)
2. Plugin `userConfig` prompt → exported by `bootstrap.sh` (Mode B)
3. `~/.attach-guard/score.env` (written by `attach setup` in Mode A, or by hand)

If none is set, the HTTP provider sends no auth → the hosted endpoint returns
`401` and Guard treats it as `provider-unavailable` (honoring
`provider_unavailable_behavior`). Prefer the local-command fallback for a clean
no-token experience.

## Verify (either mode)
```bash
TOKEN="$(sed -n -E "s/^[[:space:]]*(export[[:space:]]+)?ATTACH_OPEN_SCORE_API_TOKEN=[[:space:]]*['\"]?([^'\"#[:space:]]+)['\"]?[[:space:]]*(#.*)?$/\2/p" ~/.attach-guard/score.env | tail -1)"
curl -s https://score.attach.dev/health                                   # {"status":"ok"}
curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST https://score.attach.dev/v0/verdict \
  -d '{"ecosystem":"npm","name":"left-pad","version":"1.3.0"}'            # 401 (no token)
curl -s -X POST https://score.attach.dev/v0/verdict \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"ecosystem":"npm","name":"left-pad","version":"1.3.0"}'           # 200 + verdict
```

## Rotate / revoke
- **Managed (Mode A):** revoke from Attach Platform; `attach setup` re-provisions.
- **Standalone (Mode B):** rotate the token on the Open Score box
  (`/etc/attach-open-score/score.env`, then `systemctl restart attach-open-score`)
  and re-paste in the plugin prompt or update `~/.attach-guard/score.env`.
