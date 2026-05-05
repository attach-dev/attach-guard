# Attach Open Score provider semantics

Status: design note for the attach-guard integration path
Audience: attach-guard maintainers, Attach Open Score implementers, policy authors

## Goal

attach-guard should treat Attach Open Score as the first-party default scoring direction while keeping proprietary providers, including Socket, as bring-your-own-token local integrations unless an explicit partnership permits broader hosted/default use.

This note defines how Attach Open Score verdicts should map into attach-guard behavior before code is added.

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
| `ASK` | require review / user confirmation | ask/warn | configurable; often deny or require policy approval |
| `DENY` | block install | deny | deny |
| `UNKNOWN` | insufficient evidence | ask/warn | configurable; often deny or require policy approval |

attach-guard currently has internal `allow`, `ask`, and `deny` decisions only. Until `unknown` becomes a first-class internal decision, Open Score `UNKNOWN` should map to `ask` at the provider/policy boundary for local mode. CI/team policy may map `UNKNOWN` to deny by explicit configuration.

## Score polarity warning

Attach Open Score's numeric `score` is a risk score: higher means riskier.

attach-guard's current `PackageScore.SupplyChain` and `PackageScore.Overall` fields are treated as safety-ish scores: lower means worse, and the current policy denies when `supply_chain < threshold`.

Therefore, do not map Open Score `score` directly into `PackageScore.SupplyChain` or `Overall`. That would invert behavior.

Acceptable implementation patterns:

1. **Verdict-first bridge** — map Open Score `decision` directly to attach-guard allow/ask/deny behavior, and preserve score/reasons/source refs for explanation/audit UX.
2. **Explicit score transform** — if existing threshold code must be reused temporarily, transform `safety_score = 100 - risk_score` and add tests proving polarity for ALLOW/ASK/DENY/UNKNOWN fixtures.
3. **Policy refactor** — make attach-guard policy understand decision-first verdicts and keep risk score as supporting context rather than the primary decision variable.

The preferred v0 integration is verdict-first. Less room for accidental foot-guns. The foot-guns have had enough product-market fit.

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

Future provider kinds should make the default direction clear, for example:

```yaml
provider:
  kind: open-score
```

Socket provider docs should show explicit opt-in:

```yaml
provider:
  kind: socket
  api_token_env: SOCKET_API_TOKEN
```

## Implementation checklist

Before adding code:

- [ ] Decide provider kind name: `open-score` vs `attach-open-score`.
- [ ] Decide whether Open Score runs as an embedded Go package, external CLI, or local/hosted HTTP API.
- [ ] Add fixture-driven tests using public-safe synthetic verdicts.
- [ ] Test `UNKNOWN` mapping in local and CI modes.
- [ ] Test score polarity so high-risk scores cannot accidentally become high-safety scores.
- [ ] Preserve source/legal attribution in docs and audit output.
- [ ] Keep Socket as explicit BYO-token/local provider.
