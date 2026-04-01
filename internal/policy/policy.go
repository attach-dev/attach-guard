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

// ProviderUnavailableDecision returns the decision for when the provider is unavailable.
func (e *Engine) handleProviderUnavailable(mode api.Mode) Output {
	behavior := e.cfg.Policy.ProviderUnavailable.Local
	if mode == api.ModeCI {
		behavior = e.cfg.Policy.ProviderUnavailable.CI
	}

	switch behavior {
	case "deny":
		return Output{
			Decision: api.Deny,
			Reason:   "risk provider is unavailable and policy requires deny in this mode",
		}
	case "allow":
		return Output{
			Decision: api.Allow,
			Reason:   "risk provider is unavailable; policy allows in this mode",
		}
	default: // "ask"
		return Output{
			Decision: api.Ask,
			Reason:   "risk provider is unavailable; manual review recommended",
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
