# 2026-05-07 Attach Open Score Provider Implementation Plan

This plan records the scope and phasing for the Attach Open Score provider in
attach-guard. It is docs-only. No provider, policy, or API code is changed by
this plan, and no new dependencies are introduced.

## Goal

Define a phased, low-risk path to land the networked `open-score` provider kind
described in [`docs/OPEN_SCORE_PROVIDER.md`](../OPEN_SCORE_PROVIDER.md). The
provider/policy contract (`ProviderVerdict` with `ALLOW` / `ASK` / `DENY` /
`UNKNOWN`) and verdict-semantics layer are already wired in. What remains is the
runtime that produces a `ProviderVerdict` from an Attach Open Score-shaped HTTP
endpoint, plus its config surface and tests.

## Non-Goals

- No switch of the default scoring source to a networked provider. The default
  provider kind is unchanged by this work.
- No removal, deprecation, or behavior change of the existing Socket adapter.
  Socket remains an explicit BYO-token local provider.
- No real Attach hosted endpoint calls in unit or e2e tests. All HTTP traffic in
  tests is local (`net/http/httptest`) or mocked.
- No proprietary vendor data ingestion, calibration, or fixtures (no Socket /
  Snyk / Aikido / Sonatype / Endor scores in inputs, fixtures, or thresholds).
- No new internal decision states beyond what `ProviderVerdict` already carries.
- No changes to `internal/provider/*`, `internal/policy/*`, or `pkg/api/*` as
  part of this plan; those are touched in the implementation passes that follow.

## Phased Plan

### Phase 1 — HTTP client provider skeleton

- Add a new provider implementation under `internal/provider/openscore` (name
  finalized at implementation time) implementing the existing provider
  interface and producing `ProviderVerdict` directly.
- Use only the Go standard library (`net/http`, `encoding/json`, `context`,
  `time`). No new module dependencies.
- Wire construction behind the existing provider factory so that selecting the
  new kind is an additive code path; the default kind remains unchanged.

### Phase 2 — Config surface

- Extend the existing config struct to recognize `kind: open-score` with the
  shape already documented in `docs/OPEN_SCORE_PROVIDER.md`:

  ```yaml
  provider:
    kind: open-score
    endpoint: http://127.0.0.1:8757
    timeout_seconds: 5
  ```

- Continue to honor `ATTACH_GUARD_PROVIDER` as the environment override, so a
  user can opt in via env without touching config files.
- Validate `endpoint` is a well-formed http(s) URL and `timeout_seconds` is a
  positive duration; surface clear validation errors. No defaulting to a hosted
  Attach URL.

### Phase 3 — Request / response mapping

- Request: minimal, public-safe payload describing the package coordinate under
  evaluation (ecosystem, name, version). No tokens, no telemetry, no machine
  identifiers.
- Response: consume only Attach Open Score-shaped fields — `decision`
  (uppercase), optional `score` (risk-polarity), `reasons[]`, `source_refs[]`.
  Unknown fields are ignored.
- Map response into `ProviderVerdict` as-is. Preserve `source_refs` verbatim so
  audit/explain UX can attribute OSV / GHSA / deps.dev / registry metadata.
- Risk score is carried through as `RiskScore` only. It is never written into
  `PackageScore.SupplyChain` or `PackageScore.Overall` (see score-polarity
  safety below).

### Phase 4 — Error and timeout mapping

All transport-level failures map to a `ProviderVerdict` with
`Decision = UNKNOWN` and a stable, public-safe reason such as
`provider-unavailable`. The provider does not panic, does not retry beyond a
single bounded attempt, and never returns an `ALLOW` on error.

Cases mapped to `UNKNOWN` / `provider-unavailable`:

- DNS / connection refused / TCP reset
- TLS handshake failure
- Context deadline exceeded (`timeout_seconds`)
- Non-2xx HTTP status (including 5xx and 429)
- Malformed JSON / missing `decision` field / unrecognized `decision` value
- Response body exceeding a small, fixed size cap

The policy layer then resolves behavior via the existing
`policy.unknown_behavior` and `policy.provider_unavailable_behavior` knobs.
Local defaults remain ask/warn; CI/team can be configured to deny. No new
policy knobs are introduced.

### Phase 5 — Tests

- Unit tests in the provider package using a mock HTTP `RoundTripper` /
  fixtures for each branch of the mapping table.
- E2E tests using `net/http/httptest.NewServer` exercising the CLI evaluate
  path end-to-end against an in-process server, alongside the existing
  `TestE2E_ProviderOutageCI` / `TestE2E_ProviderOutageLocal` patterns.
- Re-use the public-safe synthetic fixture style already used by the
  verdict-semantics tests. No vendor data, no real package payloads scraped
  from third parties.

### Phase 6 — Docs and rollout

- Update `docs/OPEN_SCORE_PROVIDER.md` implementation checklist to flip the
  HTTP-provider boxes once code lands. (Out of scope for this plan; this plan
  only links itself from the checklist.)
- Update `README.md` provider section with an explicit opt-in example.
- Keep the Socket provider example in docs as explicit BYO-token.

## Test Matrix

| Layer | Scenario | Mechanism | Reference / new test |
|---|---|---|---|
| unit | `decision: ALLOW` round-trips into `ProviderVerdict{Decision: ALLOW}` | mock client / fixture | new |
| unit | `decision: ASK` round-trips | mock client / fixture | new |
| unit | `decision: DENY` round-trips | mock client / fixture | new |
| unit | `decision: UNKNOWN` round-trips with reasons + source_refs | mock client / fixture | new |
| unit | `RiskScore` carried through; `PackageScore.SupplyChain` not written from risk | mock client / fixture | new (score-polarity safety) |
| unit | malformed JSON → UNKNOWN / provider-unavailable | mock client | new |
| unit | non-2xx (500, 429, 404) → UNKNOWN / provider-unavailable | mock client | new |
| unit | context deadline exceeded → UNKNOWN / provider-unavailable | mock client | new |
| unit | unknown `decision` value → UNKNOWN | mock client | new |
| unit | `source_refs` preserved verbatim into verdict | mock client | new |
| policy | UNKNOWN behavior local=ask, ci=deny via `policy.unknown_behavior` | existing engine test style | aligns with `TestEngine_ProviderUnavailable_Local` / `TestEngine_ProviderUnavailable_CI` and existing UnknownBehavior tests in `internal/policy/policy_test.go` |
| policy | provider-unavailable behavior local=ask, ci=deny | existing engine | `TestEngine_ProviderUnavailable_CI`, `TestEngine_ProviderUnavailable_Local` |
| e2e | full evaluate pipeline against `httptest` server returning ALLOW / ASK / DENY / UNKNOWN | `net/http/httptest` | new, alongside `TestE2E_ProviderOutageCI` / `TestE2E_ProviderOutageLocal` |
| e2e | provider outage (server closed / 5xx / timeout) routes through provider-unavailable behavior | `net/http/httptest` | extends `TestE2E_ProviderOutageCI` / `TestE2E_ProviderOutageLocal` patterns |
| e2e | score-polarity safety: a high `score` (risky) does not produce an ALLOW | `net/http/httptest` | new |

Score-polarity safety is treated as a first-class test concern. Open Score
`score` is a risk score (higher = riskier); attach-guard's `PackageScore`
fields are safety-ish (lower = worse). Tests must prove the provider never
inverts that polarity.

## Source / Legal Posture

- Inputs to the provider are package coordinates only. No telemetry, no user
  identifiers, no tokens.
- Responses consumed are Attach Open Score-shaped only. No proprietary vendor
  scores, no Socket / Snyk / Aikido / Sonatype / Endor data ingested as input,
  fixture, calibration, or threshold target.
- `source_refs` are preserved end-to-end so audit/explain UX can attribute
  permitted public sources (OSV, GHSA, deps.dev, OpenSSF Scorecard, public
  registry metadata).
- Fixtures used by tests are public-safe synthetic verdicts in line with the
  existing verdict-semantics test fixtures.

## Rollout

- Feature-gated via the existing `provider.kind` config field and the
  `ATTACH_GUARD_PROVIDER` environment override. Selecting `open-score` is an
  explicit, additive opt-in.
- The default provider kind is unchanged by this work. No user is silently
  migrated to a networked provider.
- Docs updated to show the opt-in example and to keep the Socket example as
  explicit BYO-token.
- No hosted Attach endpoint is baked into defaults, fixtures, or tests.

## Verification (for the implementation passes that follow this plan)

```bash
go vet ./...
go test ./...
git diff --check
```

Plus a credential-pattern scan over the staged diff before each commit.

## Residual Risks

- Schema drift between attach-guard's parser and a future Attach Open Score
  emitter. Mitigation: tolerant decoding (ignore unknown fields), strict
  decision-field validation, fixture-driven tests, version negotiation deferred
  to a later pass.
- Accidental score-polarity inversion if a future refactor writes `RiskScore`
  into `PackageScore.SupplyChain` / `Overall`. Mitigation: explicit
  score-polarity test plus the documented warning in
  `docs/OPEN_SCORE_PROVIDER.md`.
- Misconfiguration pointing the provider at an arbitrary third-party endpoint.
  Mitigation: docs are explicit that `endpoint` must be an Attach Open
  Score-compatible service the operator controls or trusts; no defaulting.
- Fail-open on transport errors. Mitigation: every error branch maps to
  `UNKNOWN` / `provider-unavailable`; never to `ALLOW`.
- E2E flake from real network egress. Mitigation: tests use
  `net/http/httptest` only; no outbound network calls.
