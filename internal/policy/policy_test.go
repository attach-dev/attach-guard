package policy

import (
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestEngine_Allow(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "lodash",
		Score:             api.PackageScore{SupplyChain: 95, Overall: 90},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Allow {
		t.Errorf("expected Allow, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_DenyMalware(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "evil-pkg",
		Score:             api.PackageScore{SupplyChain: 10, Overall: 10},
		Alerts:            []api.PackageAlert{{Category: "malware", Severity: "critical"}},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for malware, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_DenyLowScore(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "sketchy-pkg",
		Score:             api.PackageScore{SupplyChain: 30, Overall: 30},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for low score, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_AskGrayBand(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "moderate-pkg",
		Score:             api.PackageScore{SupplyChain: 60, Overall: 65},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for gray band, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_DenyTooNew(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "new-pkg",
		Score:             api.PackageScore{SupplyChain: 95, Overall: 90},
		PublishedAt:       time.Now().Add(-1 * time.Hour), // 1 hour old
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for too new, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_ProviderUnavailable_CI(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "some-pkg",
		ProviderAvailable: false,
		Mode:              api.ModeCI,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected Deny when provider unavailable in CI, got %s", result.Decision)
	}
}

func TestEngine_ProviderUnavailable_Local(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "some-pkg",
		ProviderAvailable: false,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Ask {
		t.Errorf("expected Ask when provider unavailable locally, got %s", result.Decision)
	}
}

func TestEngine_Allowlist(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.Allowlist = []string{"trusted-pkg"}
	engine := NewEngine(cfg)

	input := Input{
		Name:              "trusted-pkg",
		Score:             api.PackageScore{SupplyChain: 10, Overall: 10},
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for allowlisted package, got %s", result.Decision)
	}
}

func TestEngine_Denylist(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.Denylist = []string{"banned-pkg"}
	engine := NewEngine(cfg)

	input := Input{
		Name:              "banned-pkg",
		Score:             api.PackageScore{SupplyChain: 95, Overall: 90},
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for denylisted package, got %s", result.Decision)
	}
}

func TestEngine_CriticalAlert(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name:              "vuln-pkg",
		Score:             api.PackageScore{SupplyChain: 80, Overall: 80},
		Alerts:            []api.PackageAlert{{Severity: "critical", Title: "RCE", Category: "vulnerability"}},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for critical alert, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_OpenScoreVerdictAllowBypassesLegacyScoreThresholds(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)
	riskScore := 5

	input := Input{
		Name: "low-risk-pkg",
		Score: api.PackageScore{
			SupplyChain: 5,
			Overall:     5,
		},
		ProviderVerdict: &api.ProviderVerdict{
			Decision:  api.ProviderVerdictAllow,
			RiskScore: &riskScore,
			Reasons:   []string{"low-risk-synthetic"},
		},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Allow {
		t.Errorf("expected Allow from verdict despite low PackageScore, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_OpenScoreVerdictDenyDoesNotTreatHighRiskAsHighSafety(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)
	riskScore := 95

	input := Input{
		Name: "high-risk-pkg",
		Score: api.PackageScore{
			SupplyChain: 95,
			Overall:     95,
		},
		ProviderVerdict: &api.ProviderVerdict{
			Decision:  api.ProviderVerdictDeny,
			RiskScore: &riskScore,
			Reasons:   []string{"high-risk-synthetic"},
		},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected Deny from high-risk verdict despite high PackageScore, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_OpenScoreVerdictUnknownLocalDefaultAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)

	input := Input{
		Name: "unknown-pkg",
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictUnknown,
			Reasons:  []string{"insufficient-evidence-synthetic"},
		},
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for UNKNOWN verdict locally, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_OpenScoreVerdictUnknownUsesModeConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.UnknownBehavior.Local = "allow"
	cfg.Policy.UnknownBehavior.CI = "deny"
	engine := NewEngine(cfg)

	input := Input{
		Name: "unknown-pkg",
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictUnknown,
		},
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Allow {
		t.Errorf("expected configured local Allow for UNKNOWN verdict, got %s: %s", result.Decision, result.Reason)
	}

	input.Mode = api.ModeCI
	result = engine.Evaluate(input)
	if result.Decision != api.Deny {
		t.Errorf("expected configured CI Deny for UNKNOWN verdict, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_OpenScoreVerdictAskMapsToAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	engine := NewEngine(cfg)
	riskScore := 45

	input := Input{
		Name: "review-pkg",
		Score: api.PackageScore{
			SupplyChain: 95,
			Overall:     95,
		},
		ProviderVerdict: &api.ProviderVerdict{
			Decision:  api.ProviderVerdictAsk,
			RiskScore: &riskScore,
			Reasons:   []string{"review-synthetic"},
		},
		PublishedAt:       time.Now().Add(-72 * time.Hour),
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Ask {
		t.Errorf("expected Ask from ASK verdict, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEngine_OpenScoreVerdictUnknownConfiguredAllowStillAsksOnCriticalAlert(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.UnknownBehavior.Local = "allow"
	engine := NewEngine(cfg)

	input := Input{
		Name: "unknown-vuln-pkg",
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictUnknown,
		},
		Alerts:            []api.PackageAlert{{Severity: "critical", Title: "critical evidence", Category: "vulnerability"}},
		ProviderAvailable: true,
		Mode:              api.ModeShell,
	}

	result := engine.Evaluate(input)
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for UNKNOWN configured allow with critical alert, got %s: %s", result.Decision, result.Reason)
	}
}
