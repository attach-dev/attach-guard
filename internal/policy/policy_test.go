package policy

import (
	"testing"
	"time"

	"github.com/hammadtq/attach-dev/attach-guard/internal/config"
	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
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
