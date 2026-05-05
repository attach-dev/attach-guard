# attach-guard CLI/tests inspection — card t_6f85b57c

Date: 2026-05-05
Repo: `attach-guard`
Branch: `docs/guard-cli-tests-inspection`
Kanban: `t_6f85b57c` / MASTER_PLAN Phase 2.1
Scope: read-only inspection plus this docs/plans report

## Executive summary

`attach-guard` is already a substantial Go CLI and Claude Code plugin around one main evaluation pipeline:

```text
Claude Code Bash tool
→ PreToolUse hook
→ attach-guard hook
→ parser.ParseAll(raw command)
→ provider.Provider / versionselect.Selector
→ policy.Engine
→ rewrite.Command when safe
→ Claude hookSpecificOutput allow/ask/deny
→ local JSONL audit log
```

The current implementation is ready for an Attach Open Score integration seam, but still defaults to Socket terminology/config/docs. The next technical wedge should not rewrite the whole CLI. It should add an Open Score-compatible provider/result mapping behind the existing provider interface, then unwind Socket-default public docs/config to make Socket BYO-token only.

## Current CLI commands

The binary entry point is `cmd/attach-guard/main.go`.

Documented and implemented commands:

| Command | Purpose | Notes |
|---|---|---|
| `attach-guard evaluate <command>` | Evaluate a package-manager command and print JSON result | Uses shell/env mode detection via `internal/envdetect`; best for manual checks. Complex shell quoting is more accurate through the hook path. |
| `attach-guard hook [run]` | Read Claude Code hook JSON from stdin and return `hookSpecificOutput` | Main enforcement path. `hook` and `hook run` are aliases. Internal errors exit 2 to block. |
| `attach-guard config init` | Write default config to `~/.attach-guard/config.yaml` | Uses `config.WriteDefault`. |
| `attach-guard version` | Print version | Build-time `-ldflags` sets version. |
| `attach-guard help` / `--help` / `-h` | Usage output | Simple manual CLI, no cobra/urfave dependency. |

README examples also cover:

```bash
attach-guard evaluate npm install axios
attach-guard evaluate npm install axios@1.14.1
attach-guard evaluate pip install litellm
attach-guard evaluate pip install litellm==1.82.8
attach-guard evaluate go get golang.org/x/net
attach-guard evaluate go get golang.org/x/net@v0.25.0
attach-guard evaluate cargo add serde
attach-guard evaluate cargo add serde@=1.0.200
attach-guard hook
```

## Current package-manager coverage

Parsed/evaluated ecosystems and commands:

| Ecosystem / PM | Parser | Guarded commands | Notes |
|---|---|---|---|
| npm | `internal/parser/npm` | `npm install`, `npm i`, `npm add` | `npm install` with no packages is passthrough because it installs from `package.json`. |
| pnpm | `internal/parser/pnpm` | `pnpm add`, `pnpm install`, `pnpm i` | Uses `api.EcosystemPNPM`; scoring adapter maps pnpm to npm-like Socket ecosystem today. |
| pip / pip3 | `internal/parser/pip` | direct `pip install`, `pip3 install` | Handles local vs non-local source flags; requirements/constraints and remote indexes force manual review/ASK. |
| Go modules | `internal/parser/gomod` | `go get`, `go install` | Rejects ambiguous/local specs into unparsed/manual review paths. |
| Cargo | `internal/parser/cargo` | `cargo add`, `cargo install` | Handles `--version`; `--git`/`--registry`/`--path` paths are treated as unparsed/manual review-sensitive. |

Current roadmap still says shell shims, `install`, `doctor`, and first-class `explain` command are not implemented.

## Build and test commands

Makefile targets:

```bash
make test        # go test ./...
make vet         # go vet ./...
make build       # go build -ldflags="-s -w -X main.version=0.2.0" -o attach-guard ./cmd/attach-guard
make plugin-build
make release     # test + vet + plugin-build + checksums
```

Local environment result in the Hermes runtime:

```text
command -v go       → no go binary found
make test           → fails: go: No such file or directory
make vet            → fails: go: No such file or directory
make build          → fails: go: not found
```

So commands are identified, but execution is blocked until Go is installed in this runtime or the card is run in a Go-capable worker/sandbox. The repo declares `go 1.18` in `go.mod`; docs mention Go 1.21+ for development. That mismatch is worth resolving in a follow-up docs/toolchain card.

## Test coverage map

Existing test files cover:

- `internal/cli/evaluate_test.go` — allow/deny/ask, provider outages, shell wrapping, unsafe rewrites, non-local args, new package managers.
- `e2e/evaluate_test.go` — high-level evaluator behavior with mock provider.
- `internal/policy/policy_test.go` — threshold decisions, malware, age, provider unavailable local/CI, allowlist/denylist, alerts.
- `internal/versionselect/select_test.go` — pinned/unpinned selection, fallback, deprecated skip, unsupported source paths.
- `internal/parser/*_test.go` — npm, pnpm, pip, Go, Cargo, shell operator and bypass parsing.
- `internal/provider/socket/socket_test.go` — Socket/PURL parsing, registry version ordering, PyPI/Go/Cargo metadata, unsupported private Go source behavior.
- `internal/hook/claude/claude_test.go` — hook input/output contract and guarded tool detection.
- `internal/audit/audit_test.go` — JSONL logging.
- `plugin/plugin_test.go` — plugin hook command quoting, explain skill wrapper, Socket token env mapping.

This is enough structure for an Open Score provider PR to be test-first without needing live network calls.

## Provider and policy integration seams

### 1. Provider interface

Primary seam:

```go
type Provider interface {
    Name() string
    GetPackageScore(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error)
    ListVersions(ctx context.Context, ecosystem api.Ecosystem, name string) ([]api.VersionInfo, error)
    IsAvailable(ctx context.Context) bool
}
```

Location: `internal/provider/provider.go`.

A first Open Score adapter can implement this interface without touching parser/policy/hook code. The main registration point is currently `loadConfigAndProvider` in `cmd/attach-guard/main.go`.

### 2. Internal domain types

Current `pkg/api` types are score-centric and Socket-shaped:

- `Decision`: `allow`, `ask`, `deny`
- no `unknown` decision yet
- `PackageScore`: `SupplyChain`, `Overall` float64
- `PackageAlert`: `Severity`, `Title`, `Category`
- `VersionInfo`: version, published time, score, alerts, deprecated
- `EvaluationResult`: decision, reason, original command, rewritten command, package evaluations

Attach Open Score v0 verdicts include richer fields:

- uppercase public decisions: `ALLOW`, `ASK`, `DENY`, `UNKNOWN`
- nullable integer `score`, where **higher means riskier**
- `confidence`
- `reasons[]` with reason codes and evidence/source refs
- `source_refs[]`
- `evaluated_at`, `ttl_seconds`, and required `limitations[]`
- optional `policy_profile`

Recommended mapping for first integration:

| Open Score verdict | attach-guard internal result |
|---|---|
| `ALLOW` | `api.Allow` |
| `ASK` | `api.Ask` |
| `DENY` | `api.Deny` |
| `UNKNOWN` | local default should map to `api.Ask`; CI/team mode may map stricter by config |

Do not add Open Score fields directly everywhere at first. Prefer a narrow adapter and possibly extend `PackageAlert` / `PackageEvaluation` later once CLI UX needs structured reason codes and source refs.

### 3. Policy engine

`internal/policy/policy.go` currently applies local thresholds to provider-normalized scores:

1. allowlist
2. denylist
3. provider unavailable local/CI behavior
4. malware alert deny
5. minimum package age deny
6. hard supply-chain threshold deny
7. gray-band ask
8. critical/high alerts ask
9. allow

This means Open Score has two possible integration styles:

- **Adapter-as-facts:** convert Open Score facts into `VersionInfo.Score`/`Alerts`, then let existing policy decide.
- **Adapter-as-verdict:** treat Open Score decisions as already-policy-shaped and map them into equivalent internal score/alert data or add a provider verdict path.

For fastest compatible integration, use adapter-as-facts only if the open-score engine exposes enough stable facts. If Open Score v0 is decision-first, add a small decision-aware bridge rather than pretending a single score is enough.

Critical polarity warning: Attach Open Score's numeric `score` is a **risk score** where higher means riskier. attach-guard's current `PackageScore.SupplyChain` / `Overall` are treated as **safety-ish scores** where lower means worse (`policy.Engine` denies when `supply_chain < threshold`). Do not map Open Score `score` directly into `VersionInfo.Score.SupplyChain` or `Overall`; that would invert risk and safety. Either keep verdict-first mapping at the adapter boundary or define an explicit, reviewed transformation such as `safety = 100 - risk` with tests that lock the polarity. Forcing verdict semantics through the old Socket-shaped fields without this guard would be fake precision. We are many things; a spreadsheet haunted by Socket is not one of them.

### 4. Config provider registration

Current defaults:

```yaml
provider:
  kind: socket
  api_token_env: SOCKET_API_TOKEN
```

`ATTACH_GUARD_PROVIDER` can override provider kind. Current supported kinds in `main.go`:

- `socket`
- `mock`

Open Score follow-up should add one of:

```yaml
provider:
  kind: open-score
```

or, if local CLI engine lives in a sibling binary/library:

```yaml
provider:
  kind: attach-open-score
```

Do not make `socket` the default going forward unless Socket remains explicitly BYO/local-only in docs/config.

## Stale Socket-default assumptions to unwind

Current public README/config/plugin copy says or implies:

- Requires Socket.dev API token for normal install.
- Checks package scores, age, and alerts via Socket.dev.
- Fails closed when provider unavailable.
- Real examples are tied to Socket pages/scores.
- Plugin userConfig asks for `socket_api_token`.
- Default config provider is `socket`.
- Plugin bootstrap refuses to run without `SOCKET_API_TOKEN`.

These conflict with the current Attach source-of-truth direction:

- Socket is allowed only as BYO-token local provider or future official partner.
- Default Attach scoring should be first-party Attach Open Score from public/open allowed sources.
- Local developer posture should prefer ASK/WARN on unknown/provider unavailable, not default fail-closed.

Note: code defaults already set provider-unavailable local behavior to `ask` and CI to `deny`, but README line-level messaging still says “fails closed when provider unavailable,” which is too broad.

## Recommended next PR sequence

1. **Docs/config positioning PR in `attach-guard`**
   - Update README/docs to say Socket is optional BYO-token provider, not default strategic backend.
   - Add Open Score provider direction and decision mapping docs.
   - Clarify local provider unavailable behavior is ASK by default; CI can DENY.
   - Resolve Go version mismatch: `go.mod` says 1.18; docs say 1.21+.

2. **Open Score provider design PR**
   - Decide provider kind name and adapter mode.
   - Define mapping between Open Score v0 verdict schema and `pkg/api` / policy outputs.
   - Explicitly decide score-polarity handling: Open Score risk score is higher-riskier; attach-guard current scores are higher-safer.
   - Decide whether `UNKNOWN` becomes a first-class internal decision or maps to ASK at boundary.

3. **Implementation PR**
   - Add `internal/provider/openscore` or equivalent.
   - Register in `loadConfigAndProvider`.
   - Add mock fixture-driven tests, no network required.
   - Keep Socket provider available only as BYO-token/local.

## Source/legal notes

- No proprietary vendor score data was copied or generated in this inspection.
- Existing repo still contains Socket-specific adapter code and docs/examples. Treat that as BYO-token/local integration unless a reviewed partnership changes the posture.
- Open Score integration should use Attach Open Score schema/methodology and allowed public/open source refs; do not calibrate against Socket/Snyk/Aikido/Sonatype/Endor scores.

## Blockers / follow-ups

- **Environment blocker:** Go is not installed in the current Hermes runtime, so `go test ./...`, `go vet ./...`, and `make build` could not execute here.
- **Toolchain mismatch:** `go.mod` declares Go 1.18, while README says Go 1.21+.
- **Internal decision mismatch:** attach-guard currently has no `unknown` internal decision; Open Score v0 does.
- **Docs mismatch:** README/plugin messaging still positions Socket as required/default and provider unavailable as fail-closed.
