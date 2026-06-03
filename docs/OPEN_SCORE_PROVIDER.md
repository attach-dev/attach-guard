# Attach Open Score provider semantics

Status: design note and v0 provider integration reference
Audience: attach-guard maintainers, Attach Open Score implementers, policy authors

## Goal

attach-guard should treat Attach Open Score as the first-party scoring direction. Proprietary providers, including Socket, remain explicit BYO-token local integrations unless a future written partnership and policy permit broader hosted/default use.

This note defines how Attach Open Score verdicts map into attach-guard behavior at the provider/policy boundary. The current code includes the verdict semantics layer, a local `attach-open-score package` provider, and an `open-score` HTTP provider for configured Attach Open Score-compatible verdict endpoints. No hosted/default Attach Open Score endpoint is baked into defaults.

For local dogfooding of the current verdict-semantics path, see [Local Attach Open Score dogfood guide](LOCAL_OPEN_SCORE_DOGFOOD.md).

## Source and licensing posture

Allowed first-party Attach Open Score inputs are public, open, or otherwise terms-permitted sources with attribution and source references, including OSV, GitHub Advisory Database, deps.dev, OpenSSF Scorecard, public registry metadata, and package artifacts where allowed by each source's license/terms.

Forbidden for default or hosted Attach scoring unless explicitly reviewed and permitted:

- copying, scraping, reselling, or redistributing Socket/Snyk/Aikido/Sonatype/Endor scores or vendor data
- using proprietary vendor scores as calibration labels, training data, fixtures, public examples, or threshold targets
- exposing an API that behaves like a raw upstream dataset redistribution service

Socket can remain useful as an explicit local BYO-token provider, but it must not be framed as the default Attach scoring source.

## Decision mapping

Attach Open Score v0 emits uppercase public decisions:

| Attach Open Score | attach-guard behavior | Local default | CI/team default |
|---|---|---|---|
| `ALLOW` | allow install | allow | allow |
| `ASK` | require review / user confirmation | ask/warn | ask/review; stricter team policy is future work |
| `DENY` | block install | deny | deny |
| `UNKNOWN` | insufficient evidence | ask/warn | configurable; often deny or require policy approval |

attach-guard currently has internal `allow`, `ask`, and `deny` decisions only. Until `unknown` becomes a first-class internal decision, Open Score `UNKNOWN` should map to `ask` at the provider/policy boundary for local mode. CI/team policy may map `UNKNOWN` to deny by explicit configuration.

## Integration boundary

The v0 implementation avoids forcing Open Score through the existing Socket-style `PackageScore` threshold path. attach-guard now carries explicit verdict data at the provider/policy boundary:

```go
type ProviderVerdict struct {
    Decision   string   // ALLOW, ASK, DENY, UNKNOWN
    RiskScore  *int     // Open Score risk score, higher means riskier
    Confidence string   // optional Open Score confidence label
    Reasons    []string // Open Score reason codes or rendered reason IDs
    SourceRefs []string // source reference IDs/URLs safe for audit output
}
```

Policy consumes `ProviderVerdict` directly when providers attach it to `VersionInfo`. Legacy local Socket score thresholds remain provider-specific signals, not the generic contract for Open Score.

Decision precedence should remain conservative:

- explicit local/team denylist beats provider `ALLOW`
- known malware or high-confidence critical evidence beats provider `ALLOW`
- provider `DENY` blocks unless an explicit allowlist/policy override exists
- provider `ASK` requires confirmation/review; provider `UNKNOWN` follows `policy.unknown_behavior` and may fail policy in CI/team mode
- provider unavailability maps to local ask/warn by default, not silent allow

Evaluation and audit output must preserve the provider verdict payload, including
optional `confidence` and public-safe `source_refs`, so downstream
explanation/audit UX can attribute OSV, GHSA, deps.dev, registry metadata, or
other allowed public evidence instead of reducing decisions to an opaque
allow/ask/deny.

## Score polarity warning

Attach Open Score's numeric `score` is a risk score: higher means riskier.

attach-guard's current `PackageScore.SupplyChain` and `PackageScore.Overall` fields are treated as safety-ish scores: lower means worse, and the current policy denies when `supply_chain < threshold`.

Therefore, do not map Open Score `score` directly into `PackageScore.SupplyChain` or `Overall`. That would invert behavior.

Acceptable implementation patterns:

1. **Verdict-first bridge** — map Open Score `decision` directly to attach-guard allow/ask/deny behavior, and preserve score/reasons/source refs for explanation/audit UX.
2. **Explicit score transform** — if existing threshold code must be reused temporarily, transform `safety_score = 100 - risk_score` and add tests proving polarity for ALLOW/ASK/DENY/UNKNOWN fixtures.
3. **Policy refactor** — make attach-guard policy understand decision-first verdicts and keep risk score as supporting context rather than the primary decision variable.

The preferred v0 integration is verdict-first. This leaves less room for accidental polarity inversions.

## Reason and source propagation

Open Score verdicts include reason codes, optional `confidence`, and `source_refs`. attach-guard should preserve these in user-facing reasons and audit logs where possible.

Initial implementation can compress reasons into a human-readable reason string, but should avoid discarding structured data permanently. Future audit entries should be able to include:

- Open Score reason codes
- severity / decision effect
- source reference IDs
- source names and URLs where safe
- evaluation timestamp and TTL

## Provider availability

Local developer mode should not default to fail-closed on provider unavailability or unknown evidence. Default local behavior:

```text
provider unavailable → ASK / warn
UNKNOWN verdict      → ASK / warn
```

CI/team mode can be stricter by explicit configuration:

```text
provider unavailable → DENY or policy failure
UNKNOWN verdict      → DENY or policy failure
```

## Config direction

Current config supports a single provider kind and environment overrides. The default provider is local Attach Open Score:

```yaml
provider:
  kind: open-score
  command: attach-open-score
```

```bash
ATTACH_OPEN_SCORE_BIN=/path/to/attach-open-score
```

When `endpoint` is omitted, attach-guard shells out to the configured command:

```bash
attach-open-score package --ecosystem <ecosystem> --name <name> --version <version>
```

`endpoint` switches the same provider kind to an Attach Open Score-compatible HTTP verdict URL; no hosted endpoint is baked into defaults.

```yaml
provider:
  kind: open-score
  endpoint: http://127.0.0.1:8757/v0/verdict
  timeout_seconds: 5
policy:
  unknown_behavior:
    local: ask                         # ask | deny | allow
    ci: deny
  provider_unavailable_behavior:
    local: ask                         # ask | deny | allow
    ci: deny
```

For a token-protected hosted endpoint, set `api_token_env` to the name of an
environment variable holding the Bearer token. When that variable is set,
attach-guard sends `Authorization: Bearer <token>` on every verdict request; if
it is unset or `api_token_env` is omitted, requests are anonymous (current
behavior).

```yaml
provider:
  kind: open-score
  endpoint: https://score.attach.dev/v0/verdict
  api_token_env: ATTACH_OPEN_SCORE_API_TOKEN
  timeout_seconds: 5
```

For the hosted **Attach Platform** edge (per-user API key, `POST /v1/score/evaluations`), use `kind: platform`:

```yaml
provider:
  kind: platform
  endpoint: https://api.attach.dev/v1/score/evaluations
  api_token_env: ATTACH_PLATFORM_API_TOKEN
  timeout_seconds: 5
```

The platform provider authenticates with the per-user platform key named by `api_token_env`, sends `{target, options}` with reasons/source-refs enabled, and maps `attach_result.open_score_decision` (plus score/confidence/reasons/source_refs) into the verdict. The shared Open Score backend token stays server-side on the platform; Guard only holds the per-user platform key.

The v0 HTTP provider posts only `ecosystem`, `name`, and `version`, then consumes `decision`, optional `score`, optional `confidence`, `reasons`, and `source_refs`. The local command provider consumes the same verdict shape from `attach-open-score package`. Structured reason and source reference objects are projected to reason codes and source reference IDs/URLs before attach-guard writes evaluation or audit output. The current provider scores explicit package coordinates; version listing for unpinned installs remains outside this v0 provider path.

Do not add proprietary-provider config examples to setup docs. Open Score
examples should use the local `attach-open-score` command or an
Attach Open Score-compatible endpoint with `ATTACH_OPEN_SCORE_API_TOKEN`.

## Implementation checklist

Current attach-guard code includes the verdict semantics layer and Open Score
providers. See the phased plan in
[`docs/plans/2026-05-07-open-score-provider-impl.md`](plans/2026-05-07-open-score-provider-impl.md).

- [x] Add provider kind: `open-score`.
- [x] Add runtime shapes: local `attach-open-score package` provider and HTTP client provider against a configured Attach Open Score-compatible endpoint.
- [x] Extend provider/policy result shape with a verdict-first result such as `ProviderVerdict`.
- [x] Add fixture-driven tests using public-safe synthetic verdicts.
- [x] Test `UNKNOWN` mapping in local and CI modes via `policy.unknown_behavior`.
- [x] Test provider-unavailable behavior via `policy.provider_unavailable_behavior`.
- [x] Test score polarity so high-risk scores cannot accidentally become high-safety scores.
- [x] Preserve source/legal attribution in docs and audit output.
- [x] Keep Socket as explicit BYO-token/local provider.
