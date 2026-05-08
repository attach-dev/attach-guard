# 2026-05-07 Local Open Score Dogfood Guide Plan

This plan records the scope for the local Open Score dogfood guide.

## Goal

Document a public-safe local dogfood path for attach-guard's Attach Open Score verdict-semantics layer without claiming a hosted/default Open Score service has shipped.

## Scope

Docs only:

- Add `docs/LOCAL_OPEN_SCORE_DOGFOOD.md`.
- Link it from `README.md` and `docs/OPEN_SCORE_PROVIDER.md`.
- Use existing tests and current CLI commands as the dogfood path.

No service code, provider code, dependencies, lockfiles, credentials, private data, or generated artifacts are added.

## Current-State Decisions

- The current code has a verdict-first `ProviderVerdict` path with `ALLOW`, `ASK`, `DENY`, and `UNKNOWN` semantics.
- The networked `open-score` provider kind is now opt-in and must not be described as hosted/default Attach scoring.
- Local default behavior for `UNKNOWN` and provider-unavailable cases remains ask/warn, not fail-closed.
- CI/team strictness requires explicit configuration.
- Socket remains an explicit BYO-token local provider and is not hosted/default Attach scoring.

## Verification Commands

```bash
go test ./internal/policy ./internal/versionselect ./internal/cli ./e2e -run 'OpenScore|ProviderVerdict|Unknown|ProviderUnavailable|ProviderOutage'
go test ./...
go build -o /tmp/attach-guard-dogfood ./cmd/attach-guard
```

## Public-Safety Review Points

- No secrets, tokens, private package names, private registry URLs, or internal endpoints.
- No proprietary vendor scores, Socket data, or vendor-derived thresholds.
- No hosted platform internals or payment/credits/admin details.
- No claim that Attach hosts a default Open Score service before that ships.
