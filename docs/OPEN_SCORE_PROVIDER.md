# Attach Open Score provider semantics

Status: design note for the attach-guard integration path
Audience: attach-guard maintainers, Attach Open Score implementers, policy authors

## Goal

attach-guard should treat Attach Open Score as the first-party default scoring direction while keeping proprietary providers, including Socket, as bring-your-own-token local integrations unless an explicit partnership permits broader hosted/default use.

This note defines how Attach Open Score verdicts map into attach-guard behavior at the provider/policy boundary. The current code includes the verdict semantics layer; the networked Open Score provider/client remains future work.

## Source and licensing posture

Allowed default Attach Open Score inputs are public, open, or otherwise terms-permitted sources with attribution and source references, including OSV, GitHub Advisory Database, deps.dev, OpenSSF Scorecard, public registry metadata, and package artifacts where allowed by each source's license/terms.

Forbidden for default or hosted Attach scoring unless explicitly reviewed and permitted:

- copying, scraping, reselling, or redistributing Socket/Snyk/Aikido/Sonatype/Endor scores or vendor data
- using proprietary vendor scores as calibration labels, training data, fixtures, public examples, or threshold targets
- exposing a paid API that behaves like a raw upstream dataset redistribution service

Socket can remain useful as a local BYO-token provider, but it must not be framed as the default Attach scoring source.

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
    Reasons    []string // Open Score reason codes or rendered reason IDs
    SourceRefs []string // source reference IDs/URLs safe for audit output
}
```

Policy consumes `ProviderVerdict` directly when providers attach it to `VersionInfo`. Legacy Socket score thresholds remain provider-specific signals, not the generic contract for Open Score.

Decision precedence should remain conservative:

- explicit local/team denylist beats provider `ALLOW`
- known malware or high-confidence critical evidence beats provider `ALLOW`
- provider `DENY` blocks unless an explicit allowlist/policy override exists
- provider `ASK` requires confirmation/review; provider `UNKNOWN` follows `policy.unknown_behavior` and may fail policy in CI/team mode
- provider unavailability maps to local ask/warn by default, not silent allow

Evaluation and audit output must preserve the provider verdict payload, including
public-safe `source_refs`, so downstream explanation/audit UX can attribute OSV,
GHSA, deps.dev, registry metadata, or other allowed public evidence instead of
reducing decisions to an opaque allow/ask/deny.

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

Open Score verdicts include reason codes and `source_refs`. attach-guard should preserve these in user-facing reasons and audit logs where possible.

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

Current config supports a single provider kind and an environment override:

```yaml
provider:
  kind: socket
```

```bash
ATTACH_GUARD_PROVIDER=mock
```

Future provider kind for this integration should be `open-score`.

```yaml
provider:
  kind: open-score
  endpoint: http://127.0.0.1:8757      # local or hosted Attach Open Score-compatible HTTP endpoint
  timeout_seconds: 5
policy:
  unknown_behavior:
    local: ask                         # ask | deny | allow
    ci: deny
  provider_unavailable_behavior:
    local: ask                         # ask | deny | allow
    ci: deny
```

The v0 implementation target is an HTTP client provider. Embedded Go package or external CLI modes can be added later, but should not block the first provider pass.

Socket provider docs should show explicit opt-in:

```yaml
provider:
  kind: socket
  api_token_env: SOCKET_API_TOKEN
```

## Implementation checklist

Current attach-guard code includes the verdict semantics layer. The networked
Open Score provider/client remains a later implementation pass.

- [ ] Add provider kind for the next pass: `open-score`.
- [ ] Add runtime shape for the next pass: HTTP client provider against a local or hosted Attach Open Score-compatible endpoint.
- [x] Extend provider/policy result shape with a verdict-first result such as `ProviderVerdict`.
- [x] Add fixture-driven tests using public-safe synthetic verdicts.
- [x] Test `UNKNOWN` mapping in local and CI modes via `policy.unknown_behavior`.
- [ ] Test provider-unavailable behavior via `policy.provider_unavailable_behavior`.
- [x] Test score polarity so high-risk scores cannot accidentally become high-safety scores.
- [ ] Preserve source/legal attribution in docs and audit output.
- [ ] Keep Socket as explicit BYO-token/local provider.
