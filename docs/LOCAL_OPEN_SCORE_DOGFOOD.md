# Local Attach Open Score dogfood guide

Status: local-only dogfood guide for current attach-guard code
Audience: attach-guard maintainers and contributors

This guide shows how to exercise the current CLI/provider/policy path with Attach Open Score verdict semantics without implying that a hosted Attach Open Score service or networked `open-score` provider has shipped.

## Current state

What exists today:

- attach-guard has a verdict-first provider payload, `ProviderVerdict`, with public decisions `ALLOW`, `ASK`, `DENY`, and `UNKNOWN`.
- Policy and version selection consume that verdict payload before falling back to legacy safety-score thresholds.
- Audit/evaluation output preserves `provider_verdict` fields when a provider returns them.
- Local defaults are intentionally developer-friendly: provider unavailable and Open Score `UNKNOWN` map to ask/warn by default.

What does **not** exist yet:

- No public hosted Attach Open Score behavior is documented or promised here.
- No networked `open-score` provider kind is wired into the CLI yet.
- The CLI `mock` provider is an empty local provider unless test code seeds fixtures; it is useful for tests, not for ad-hoc scoring demos.
- Socket.dev remains an explicit bring-your-own-token local provider only. Do not present Socket data, Socket scores, or Socket quota behavior as Attach Open Score.

For the planned provider contract, see [Attach Open Score provider semantics](OPEN_SCORE_PROVIDER.md).

## Prerequisites

- Go 1.21+.
- A clean checkout of this repository.
- No Attach-hosted credentials are required.
- A Socket API token is optional and only needed if you also want to exercise the current Socket BYO-token provider path.

## 1. Run the verdict-semantics dogfood tests

These tests are the safest public way to dogfood the current Attach Open Score semantics because they use synthetic fixture data and do not call proprietary scoring services.

```bash
go test ./internal/policy ./internal/versionselect ./internal/cli ./e2e -run 'OpenScore|ProviderVerdict|Unknown|ProviderUnavailable|ProviderOutage'
```

What this covers:

- `ALLOW` is handled as a verdict and is not accidentally denied because an Open Score risk score was copied into a legacy lower-is-worse safety field.
- `DENY` blocks even when a high risk score might otherwise look like a high safety score if polarity were inverted.
- `ASK` maps to local review/confirmation.
- `UNKNOWN` maps to local ask/warn by default and follows explicit mode config when configured.
- Provider unavailable behavior follows `policy.provider_unavailable_behavior`.
- Unpinned-version selection preserves Open Score-style verdict reasons when it suggests or rejects candidate versions.

Then run the full suite before sending a docs PR:

```bash
go test ./...
```

## 2. Exercise the current CLI path locally

Build the binary from source:

```bash
go build -o ./attach-guard ./cmd/attach-guard
./attach-guard version
```

Use a temporary audit log so dogfood runs do not mix with your personal log:

```bash
DOGFOOD_TMP=$(mktemp -d)
export ATTACH_GUARD_LOG_PATH="$DOGFOOD_TMP/attach-guard-dogfood.audit.jsonl"
```

### Provider unavailable / local ask default

Without a Socket token, the default provider path reports Socket unavailable and should follow the local provider-unavailable policy. This exercises the current CLI/evaluator path while keeping the Open Score positioning honest: it is not using Open Score network scoring.

```bash
unset SOCKET_API_TOKEN
./attach-guard evaluate npm install left-pad@1.3.0
```

Expected local semantics:

- decision is `ask` by default in local/interactive mode,
- reason explains provider unavailability,
- audit output is written to `ATTACH_GUARD_LOG_PATH` if logging succeeds.

If you explicitly configure stricter behavior, the result can change. For example, CI/team fail-closed behavior should be an explicit config choice, not the local default.

### Optional Socket BYO-token provider path

Only run this if you have your own Socket token and want to verify the currently shipped provider adapter:

```bash
# In a private shell, set SOCKET_API_TOKEN to your own value first.
./attach-guard evaluate npm install left-pad@1.3.0
```

This is local BYO-provider dogfooding. Do not copy returned Socket scores into public Attach Open Score examples, fixtures, threshold targets, screenshots, or marketing copy.

## 3. Inspect audit/evaluation shape

Evaluation output and audit data use related public result vocabulary, but not every field appears in every surface:

- `decision`: attach-guard decision, one of `allow`, `ask`, or `deny`.
- `provider`: provider name in audit entries, such as `mock` in tests or `socket` for local BYO-token runs.
- `packages[].provider_verdict`: present when the provider/test fixture supplied verdict-first data.

Synthetic Open Score-style audit payloads may include:

```json
{
  "provider_verdict": {
    "decision": "UNKNOWN",
    "risk_score": 50,
    "reasons": ["insufficient-public-evidence-synthetic"],
    "source_refs": ["osv:synthetic-public-fixture"]
  }
}
```

Keep examples synthetic and public-safe. Use public source references such as OSV or GHSA IDs only when they are real and relevant; otherwise use clearly marked synthetic IDs.

## 4. Local config to keep UNKNOWN/provider unavailable non-blocking

The default local posture is ask/warn. If you need an explicit dogfood config, keep local behavior non-blocking and reserve fail-closed behavior for CI/team policy:

```yaml
provider:
  kind: socket              # current local BYO-token provider; open-score is not wired yet
  api_token_env: SOCKET_API_TOKEN
policy:
  provider_unavailable_behavior:
    local: ask
    ci: deny
  unknown_behavior:
    local: ask
    ci: deny
```

Future `open-score` examples should remain clearly marked as planned until the provider kind is implemented.

## Public-safety checklist for docs and demos

Before publishing dogfood notes, verify that they do not include:

- secrets, API tokens, private package names, private registry URLs, or internal endpoints,
- proprietary vendor scores, copied Socket/Snyk/Aikido/Sonatype/Endor data, or vendor-derived thresholds,
- claims that Attach hosts a default Open Score service before that behavior ships,
- claims that Socket is the hosted/default Attach scoring source,
- CI/team fail-closed behavior presented as the local default for provider-unavailable or `UNKNOWN` cases.

PRs that change behavior should update this guide and [Attach Open Score provider semantics](OPEN_SCORE_PROVIDER.md) together.
