package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/internal/cli"
	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// setupMock creates a mock provider with a realistic set of packages.
func setupMock() *provider.MockProvider {
	mock := provider.NewMockProvider()

	// lodash: well-known, high-score package
	mock.AddVersion("lodash", api.VersionInfo{
		Version:     "4.17.21",
		PublishedAt: time.Now().Add(-8760 * time.Hour), // 1 year old
		Score:       api.PackageScore{SupplyChain: 95, Overall: 92},
	})
	mock.AddScore("lodash", "4.17.21", 95, 92)

	// axios: good package
	mock.AddVersion("axios", api.VersionInfo{
		Version:     "1.7.0",
		PublishedAt: time.Now().Add(-720 * time.Hour), // 30 days
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})
	mock.AddScore("axios", "1.7.0", 92, 88)

	// new-pkg: latest is too new, older is fine
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-1 * time.Hour), // 1 hour
		Score:       api.PackageScore{SupplyChain: 85, Overall: 80},
	})
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "1.9.0",
		PublishedAt: time.Now().Add(-720 * time.Hour), // 30 days
		Score:       api.PackageScore{SupplyChain: 88, Overall: 85},
	})

	// bad-pkg: low score
	mock.AddVersion("bad-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 30, Overall: 25},
	})
	mock.AddScore("bad-pkg", "1.0.0", 30, 25)

	// malware-pkg
	mock.AddVersion("malware-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 5, Overall: 5},
		Alerts:      []api.PackageAlert{{Severity: "critical", Title: "Known malware", Category: "malware"}},
	})
	mock.Scores["malware-pkg@1.0.0"] = &api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 5, Overall: 5},
		Alerts:      []api.PackageAlert{{Severity: "critical", Title: "Known malware", Category: "malware"}},
	}

	return mock
}

func TestE2E_AllowGoodPackage(t *testing.T) {
	mock := setupMock()
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	result, err := eval.Evaluate(context.Background(), "npm install lodash", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for lodash, got %s: %s", result.Decision, result.Reason)
	}
}

func TestE2E_UnpinnedTooNewAskRewrite(t *testing.T) {
	mock := setupMock()
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	result, err := eval.Evaluate(context.Background(), "npm install new-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for new-pkg, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "npm install new-pkg@1.9.0" {
		t.Errorf("expected rewrite to 1.9.0, got %q", result.RewrittenCommand)
	}
}

func TestE2E_PinnedLowScoreDeny(t *testing.T) {
	mock := setupMock()
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	result, err := eval.Evaluate(context.Background(), "npm install bad-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for bad-pkg@1.0.0, got %s: %s", result.Decision, result.Reason)
	}
}

func TestE2E_ProviderOutageCI(t *testing.T) {
	mock := setupMock()
	mock.Available = false
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	result, err := eval.Evaluate(context.Background(), "npm install axios", api.ModeCI)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny in CI with outage, got %s", result.Decision)
	}
}

func TestE2E_ProviderOutageLocal(t *testing.T) {
	mock := setupMock()
	mock.Available = false
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	result, err := eval.Evaluate(context.Background(), "npm install axios", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask locally with outage, got %s", result.Decision)
	}
}

func TestE2E_NonInstallIgnored(t *testing.T) {
	mock := setupMock()
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	cmds := []string{
		"npm run test",
		"npm test",
		"git status",
		"ls -la",
		"echo hello",
		"pnpm run build",
	}

	for _, cmd := range cmds {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatal(err)
		}
		if result.Decision != api.Allow {
			t.Errorf("expected Allow for %q, got %s", cmd, result.Decision)
		}
	}
}

func TestE2E_ShimForwardNonInstall(t *testing.T) {
	mock := setupMock()
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	// npm run test should be allowed without checking provider
	result, err := eval.Evaluate(context.Background(), "npm run test", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for npm run test, got %s", result.Decision)
	}
	if result.Reason != "not a guarded install command" {
		t.Errorf("unexpected reason: %s", result.Reason)
	}
}

func TestE2E_PNPMAdd(t *testing.T) {
	mock := setupMock()
	cfg := config.DefaultConfig()
	eval := cli.NewEvaluator(cfg, mock)

	// pnpm uses same npm ecosystem for scoring
	mock.AddVersion("express", api.VersionInfo{
		Version:     "4.18.2",
		PublishedAt: time.Now().Add(-2160 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 94, Overall: 91},
	})

	result, err := eval.Evaluate(context.Background(), "pnpm add express", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for pnpm add express, got %s: %s", result.Decision, result.Reason)
	}
}
