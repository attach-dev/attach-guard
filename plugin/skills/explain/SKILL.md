---
name: explain
description: Look up a package's supply-chain risk score, alerts, and version history using attach-guard. Use this when a user asks about a package's safety, or when attach-guard blocks an install and you want to understand why.
---

# Explain Package Risk

When a user asks about the safety or risk profile of an npm/pnpm package, or when attach-guard has blocked or flagged an install, use this skill to provide detailed information.

## How to use

Run the attach-guard evaluate command via the plugin wrapper:

```bash
"${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh" evaluate npm install <package-name>
```

This returns JSON with:
- **decision**: `allow`, `ask`, or `deny`
- **reason**: why the decision was made
- **packages**: array with score details, age, and alerts

## Interpreting results

- **supply_chain score**: measures supply chain integrity (author history, publish patterns, dependency health). Below 50 is high risk, 50-70 is gray band, above 70 is safe.
- **overall score**: composite of supply chain, quality, maintenance, and vulnerability metrics.
- **age_hours**: how recently this version was published. Versions under 48 hours old are blocked by default.
- **alerts**: specific issues like malware detection, known vulnerabilities, or quality concerns.

## Example response format

After running the command, explain the results to the user in plain language:
- State the decision and reason
- Highlight any concerning alerts with their severity
- If a version was blocked, explain which version would pass and why
- Suggest alternatives if the package is entirely denied
