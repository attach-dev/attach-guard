# Security

## Threat Model

attach-guard is a local enforcement tool. It trusts:
- The local filesystem and config files
- The configured risk provider API
- The local user running the binary

It does not trust:
- Package names or version strings (they are parsed carefully, never interpolated into shell commands)
- Provider API responses (they are validated and normalized)
- Hook input JSON (it is parsed, not evaluated)

## Shell Injection Prevention

All subprocess execution uses `exec.Command` with explicit argv slices. No command string is ever passed through a shell. The parser tokenizes commands internally without shell evaluation.

## Credential Handling

- API tokens are read from environment variables, never from config files
- Tokens are never written to audit logs
- Tokens are never included in error messages or hook output

## Recursion Safety

Shell shims use the `ATTACH_GUARD_ACTIVE` environment variable to prevent infinite recursion. When set, shims bypass attach-guard and exec the real binary directly.

## Config Security

- Project-local config merges with but does not silently replace global config
- Allowlists in project config cannot override global denylists (planned)
- Config files are expected to be owned by the running user

## Audit Trail

All decisions are logged to a local JSONL file with:
- Timestamp, user, working directory
- Original and rewritten commands
- Decision and reason
- Package scores and alerts
- Provider name and execution mode

The audit log does not contain API tokens or provider credentials.

## CI Behavior

In CI mode (detected via common CI environment variables):
- Provider unavailability results in deny (fail closed)
- Auto-rewrite is disabled by default
- Interactive prompts are not shown

## Reporting Security Issues

If you find a security vulnerability, please report it via GitHub Security Advisories or email the maintainers directly. Do not open a public issue for security vulnerabilities.
