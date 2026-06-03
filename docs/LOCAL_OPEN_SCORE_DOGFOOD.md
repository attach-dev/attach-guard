# Local Attach Open Score dogfood guide

Status: local-only dogfood guide for current attach-guard code
Audience: attach-guard maintainers and contributors

This guide shows how to exercise the current CLI/provider/policy path with Attach Open Score verdict semantics, the local `attach-open-score package` provider, and the optional `open-score` HTTP provider without implying that a hosted/default Attach Open Score service has shipped.

## Current state

What exists today:

- attach-guard has a verdict-first provider payload, `ProviderVerdict`, with public decisions `ALLOW`, `ASK`, `DENY`, and `UNKNOWN`.
- attach-guard has an `open-score` provider that shells out to local `attach-open-score package` by default.
- The same provider can use configured Attach Open Score-compatible HTTP verdict endpoints when `provider.endpoint` is set.
- Policy and version selection consume that verdict payload before falling back to legacy safety-score thresholds.
- Audit/evaluation output preserves `provider_verdict` fields when a provider returns them.
- Local defaults are intentionally developer-friendly: provider unavailable and Open Score `UNKNOWN` map to ask/warn by default.

What does **not** exist yet:

- No public hosted Attach Open Score behavior is documented or promised here.
- No hosted/default `open-score` endpoint is baked into attach-guard; local scoring uses the configured command, and HTTP endpoints must be explicitly configured.
- The CLI `mock` provider is an empty local provider unless test code seeds fixtures; it is useful for tests, not for ad-hoc scoring demos.
- Socket.dev remains an explicit bring-your-own-token local provider only. Do not present Socket data, Socket scores, or Socket quota behavior as Attach Open Score.

For the provider contract, see [Attach Open Score provider semantics](OPEN_SCORE_PROVIDER.md).

## Prerequisites

- Go 1.21+.
- A clean checkout of this repository.
- A local `attach-open-score` binary on `PATH`, or `ATTACH_OPEN_SCORE_BIN=/path/to/attach-open-score`, when exercising the local command provider.
- No Attach-hosted credentials are required.
- A Socket API token is optional and only needed if you also want to exercise the current Socket BYO-token provider path.

## 1. Run the verdict-semantics dogfood tests

These tests are the safest public way to dogfood the current Attach Open Score semantics because they use synthetic fixture data and do not call proprietary scoring services.

```bash
go test ./internal/policy ./internal/versionselect ./internal/cli ./e2e -run 'OpenScore|ProviderVerdict|Unknown|ProviderUnavailable|ProviderOutage'
```

Or run the checked smoke helper, which uses only synthetic local tests, redirects
`HOME` and focused-smoke audit output under a temporary directory, preserves the
current Go module/build caches to avoid unnecessary downloads, unsets local
provider overrides, and unsets `SOCKET_API_TOKEN` so it cannot accidentally
exercise the BYO Socket provider path:

```bash
scripts/local-open-score-dogfood.sh
```

Set `ATTACH_GUARD_DOGFOOD_FULL=1` to run `go test ./...` after the focused
smoke target.

What this covers:

- A local `httptest` Attach Open Score-compatible HTTP server can drive the configured `provider.kind: open-score` evaluator path without real network access.
- A local `attach-open-score package` binary can drive the default `provider.kind: open-score` evaluator path without a hosted endpoint.
- `ALLOW` is handled as a verdict and is not accidentally denied because an Open Score risk score was copied into a legacy lower-is-worse safety field.
- `DENY` blocks even when a high risk score might otherwise look like a high safety score if polarity were inverted.
- `ASK` maps to local review/confirmation.
- `UNKNOWN` and provider HTTP failures map to local ask/warn by default and follow explicit mode config when configured.
- Provider unavailable behavior follows `policy.provider_unavailable_behavior`.
- Open Score confidence, reasons, and `source_refs` survive into evaluation and audit-visible structures.
- Unpinned-version selection preserves Open Score-style verdict reasons when it suggests or rejects candidate versions.

The consolidated e2e matrix lives in
`TestE2E_OpenScoreLocalDogfoodMatrixPreservesIdentityAndProvenance` and uses
only synthetic fixtures against an explicitly configured local test endpoint:

| Flow | Fixture command | Open Score request ecosystem | Expected local behavior |
|---|---|---|---|
| npm | `npm install npm-demo@1.0.0` | `npm` | `ALLOW` fixture allows |
| pnpm | `pnpm add pnpm-demo@2.0.0` | `npm` | `ASK` fixture asks |
| Yarn opt-in | `yarn add react@18.2.0` | `npm` | `ALLOW` fixture allows only when `provider.kind: open-score` is explicit |
| pip | `pip install flask==3.0.0` | `pypi` | `UNKNOWN` fixture asks locally |
| Cargo | `cargo install ripgrep --version 14.0.0` | `cargo` | `ALLOW` fixture allows |
| Go | `go install golang.org/x/tools/cmd/godoc@v0.20.0` | `go` | `ASK` fixture asks |

Adjacent regression tests keep the safety boundaries visible: provider outage
fixtures ask locally across the same package-manager families, private/custom
Yarn source shapes ask before any Open Score request is made, and default
Socket compatibility tests prove Yarn does not become guarded unless the
Open Score provider is explicitly selected.

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
- Structured Open Score `source_refs` are summarized as IDs or URLs in `provider_verdict.source_refs`; raw upstream source reference objects are not written back out.

Synthetic Open Score-style audit payloads may include:

```json
{
  "provider_verdict": {
    "decision": "UNKNOWN",
    "risk_score": 50,
    "confidence": "LOW",
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
  kind: open-score
  command: attach-open-score
policy:
  provider_unavailable_behavior:
    local: ask
    ci: deny
  unknown_behavior:
    local: ask
    ci: deny
```

Set `ATTACH_OPEN_SCORE_BIN=/path/to/attach-open-score` when the binary is not on `PATH`, and set `ATTACH_OPEN_SCORE_DB_PATH=/path/to/scores.json` to choose the local verdict cache used by the scorer.

For HTTP dogfood, configure an explicit local or test endpoint:

```yaml
provider:
  kind: open-score
  endpoint: http://127.0.0.1:8757/v0/verdict
  timeout_seconds: 5
policy:
  provider_unavailable_behavior:
    local: ask
    ci: deny
  unknown_behavior:
    local: ask
    ci: deny
```

Use a local command or local/test Attach Open Score-compatible endpoint for dogfood. Do not point public examples at private endpoints, and do not imply there is a hosted/default Attach Open Score service until that ships.

## Public-safety checklist for docs and demos

Before publishing dogfood notes, verify that they do not include:

- secrets, API tokens, private package names, private registry URLs, or internal endpoints,
- proprietary vendor scores, copied Socket/Snyk/Aikido/Sonatype/Endor data, or vendor-derived thresholds,
- claims that Attach hosts a default Open Score service before that behavior ships,
- claims that Socket is the hosted/default Attach scoring source,
- CI/team fail-closed behavior presented as the local default for provider-unavailable or `UNKNOWN` cases.

PRs that change behavior should update this guide and [Attach Open Score provider semantics](OPEN_SCORE_PROVIDER.md) together.
