// Package policy implements the decision engine for attach-guard.
package policy

import (
	"fmt"
	"strings"
	"time"

	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// Engine evaluates packages against policy.
type Engine struct {
	cfg *config.Config
}

// NewEngine creates a new policy engine.
func NewEngine(cfg *config.Config) *Engine {
	return &Engine{cfg: cfg}
}

// Input holds all data needed for a policy decision on a single package.
type Input struct {
	Ecosystem         api.Ecosystem
	Name              string
	RequestedSpec     string
	ResolvedVersion   string
	Score             api.PackageScore
	ProviderVerdict   *api.ProviderVerdict
	Alerts            []api.PackageAlert
	PublishedAt       time.Time
	ProviderAvailable bool
	Mode              api.Mode
	Pinned            bool
}

// Output holds the policy decision for a single package.
type Output struct {
	Decision api.Decision
	Reason   string
}

// Evaluate makes a policy decision for a single package.
func (e *Engine) Evaluate(input Input) Output {
	// Check allowlist
	if e.isAllowed(input.Name) {
		return Output{Decision: api.Allow, Reason: "package is on the allowlist"}
	}

	// Check denylist
	if e.isDenied(input.Name) {
		return Output{Decision: api.Deny, Reason: "package is on the denylist"}
	}

	// Provider unavailable
	if !input.ProviderAvailable {
		return e.handleProviderUnavailable(input.Mode)
	}

	// Deny known malware
	if e.cfg.Policy.DenyKnownMalware && hasMalwareAlert(input.Alerts) {
		return Output{
			Decision: api.Deny,
			Reason:   "package version has known malware alerts",
		}
	}

	// Minimum package age
	if e.cfg.Policy.MinimumPackageAgeHours > 0 && !input.PublishedAt.IsZero() {
		ageHours := time.Since(input.PublishedAt).Hours()
		if ageHours < float64(e.cfg.Policy.MinimumPackageAgeHours) {
			return Output{
				Decision: api.Deny,
				Reason: fmt.Sprintf(
					"package version is too new (%.0f hours old, minimum %d hours required)",
					ageHours, e.cfg.Policy.MinimumPackageAgeHours,
				),
			}
		}
	}

	if input.ProviderVerdict != nil {
		return e.evaluateProviderVerdict(input)
	}

	// Score-based decisions
	sc := input.Score.SupplyChain
	ov := input.Score.Overall

	// Hard deny
	if sc < e.cfg.Policy.GrayBandMinSupplyChain {
		return Output{
			Decision: api.Deny,
			Reason: fmt.Sprintf(
				"supply chain score %.0f is below minimum threshold %.0f",
				sc, e.cfg.Policy.GrayBandMinSupplyChain,
			),
		}
	}

	// Gray band — ask
	if sc < e.cfg.Policy.MinSupplyChainScore || ov < e.cfg.Policy.MinOverallScore {
		return Output{
			Decision: api.Ask,
			Reason: fmt.Sprintf(
				"scores are in the gray band (supply_chain=%.0f, overall=%.0f); review recommended",
				sc, ov,
			),
		}
	}

	// Critical/high alerts
	if hasCriticalOrHighAlert(input.Alerts) {
		return Output{
			Decision: api.Ask,
			Reason:   "package version has critical or high severity alerts",
		}
	}

	return Output{Decision: api.Allow, Reason: "package passes all policy checks"}
}

func (e *Engine) evaluateProviderVerdict(input Input) Output {
	verdict := input.ProviderVerdict
	decision := api.ProviderVerdictDecision(strings.ToUpper(strings.TrimSpace(string(verdict.Decision))))

	switch decision {
	case api.ProviderVerdictDeny:
		return Output{
			Decision: api.Deny,
			Reason:   verdictReason(verdict, "Attach Open Score verdict DENY"),
		}
	case api.ProviderVerdictAsk:
		return Output{
			Decision: api.Ask,
			Reason:   verdictReason(verdict, "Attach Open Score verdict ASK; review recommended"),
		}
	case api.ProviderVerdictUnknown:
		return e.handleUnknownVerdict(input, verdict)
	case api.ProviderVerdictAllow:
		if hasCriticalOrHighAlert(input.Alerts) {
			return Output{
				Decision: api.Ask,
				Reason:   "package version has critical or high severity alerts",
			}
		}
		return Output{
			Decision: api.Allow,
			Reason:   verdictReason(verdict, "Attach Open Score verdict ALLOW"),
		}
	default:
		return Output{
			Decision: api.Ask,
			Reason:   fmt.Sprintf("provider returned unrecognized verdict %q; manual review recommended", verdict.Decision),
		}
	}
}

func (e *Engine) handleUnknownVerdict(input Input, verdict *api.ProviderVerdict) Output {
	if verdictHasReason(verdict, "provider-unavailable") {
		return e.handleProviderUnavailable(input.Mode)
	}

	behavior := e.cfg.Policy.UnknownBehavior.Local
	if input.Mode == api.ModeCI {
		behavior = e.cfg.Policy.UnknownBehavior.CI
	}

	output := configuredDecision(
		behavior,
		verdictReason(verdict, "Attach Open Score verdict UNKNOWN; policy requires deny in this mode"),
		verdictReason(verdict, "Attach Open Score verdict UNKNOWN; manual review recommended"),
		verdictReason(verdict, "Attach Open Score verdict UNKNOWN; policy allows in this mode"),
	)
	if output.Decision == api.Allow && hasCriticalOrHighAlert(input.Alerts) {
		return Output{
			Decision: api.Ask,
			Reason:   "package version has critical or high severity alerts",
		}
	}
	return output
}

func verdictHasReason(verdict *api.ProviderVerdict, reason string) bool {
	if verdict == nil {
		return false
	}
	for _, r := range verdict.Reasons {
		if strings.EqualFold(strings.TrimSpace(r), reason) {
			return true
		}
	}
	return false
}

// ProviderUnavailableDecision returns the decision for when the provider is unavailable.
func (e *Engine) handleProviderUnavailable(mode api.Mode) Output {
	behavior := e.cfg.Policy.ProviderUnavailable.Local
	if mode == api.ModeCI {
		behavior = e.cfg.Policy.ProviderUnavailable.CI
	}

	return configuredDecision(
		behavior,
		"risk provider is unavailable and policy requires deny in this mode",
		"risk provider is unavailable; manual review recommended",
		"risk provider is unavailable; policy allows in this mode",
	)
}

func configuredDecision(behavior, denyReason, askReason, allowReason string) Output {
	switch behavior {
	case "deny":
		return Output{
			Decision: api.Deny,
			Reason:   denyReason,
		}
	case "allow":
		return Output{
			Decision: api.Allow,
			Reason:   allowReason,
		}
	default: // "ask"
		return Output{
			Decision: api.Ask,
			Reason:   askReason,
		}
	}
}

// ShouldAutoRewrite returns true if auto-rewrite is allowed for the given mode.
func (e *Engine) ShouldAutoRewrite(mode api.Mode) bool {
	if mode == api.ModeCI {
		return e.cfg.Policy.AutoRewriteUnpinned.CI
	}
	return e.cfg.Policy.AutoRewriteUnpinned.Local
}

func (e *Engine) isAllowed(name string) bool {
	for _, a := range e.cfg.Policy.Allowlist {
		if strings.EqualFold(a, name) {
			return true
		}
	}
	return false
}

func (e *Engine) isDenied(name string) bool {
	for _, d := range e.cfg.Policy.Denylist {
		if strings.EqualFold(d, name) {
			return true
		}
	}
	return false
}

func hasMalwareAlert(alerts []api.PackageAlert) bool {
	for _, a := range alerts {
		if strings.EqualFold(a.Category, "malware") {
			return true
		}
	}
	return false
}

func hasCriticalOrHighAlert(alerts []api.PackageAlert) bool {
	for _, a := range alerts {
		sev := strings.ToLower(a.Severity)
		if sev == "critical" || sev == "high" {
			return true
		}
	}
	return false
}

func verdictReason(verdict *api.ProviderVerdict, fallback string) string {
	if verdict == nil || len(verdict.Reasons) == 0 {
		return fallback
	}
	return fmt.Sprintf("%s (%s)", fallback, strings.Join(verdict.Reasons, ", "))
}
