# Roadmap

## Phase 1: MVP (current)

- [x] npm parser and interceptor
- [x] pnpm parser and interceptor
- [x] Socket provider adapter
- [x] Policy engine with allow/ask/deny
- [x] Minimum package age enforcement
- [x] Version selection and command rewrite
- [x] Claude Code PreToolUse hook
- [x] JSONL audit logging
- [x] Config loading with env var overrides
- [x] CLI: evaluate, hook, config init, version
- [x] Unit tests
- [x] Documentation

### Parser hardening follow-ups

- [ ] Tokenizer: split shell operators embedded without spaces (e.g., `axios&&lodash`) so `firstCommandSegment()` can see them
- [ ] Claude Code plugin packaging (hook + optional skill)

### Not yet implemented

- [ ] Shell shims for npm and pnpm
- [ ] `install` command (auto-setup shims and config)
- [ ] `doctor` command (environment check)
- [ ] `explain` command (package detail lookup)

## Phase 2: Ecosystem Expansion

- [ ] Yarn support
- [ ] Python support (uv, pip)
- [ ] Lockfile preview resolution
- [ ] Better transitive dependency visibility
- [ ] Provider fusion (combine multiple providers)
- [ ] Signed policy bundles
- [ ] Homebrew formula
- [ ] Install script (curl | sh)

## Phase 3: Teams and Enterprise

- [ ] Org-level policy packs
- [ ] Remote audit ingestion
- [ ] Team dashboards
- [ ] Enterprise provider adapters
- [ ] RBAC for policy management
- [ ] Integration with CI/CD platforms (GitHub Actions, GitLab CI)
- [ ] Docker image for CI
- [ ] Policy-as-code with version control
