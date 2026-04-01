package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestEvaluate_AllowGoodPackage(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	// lodash with good scores, published 10 days ago
	mock.AddVersion("lodash", api.VersionInfo{
		Version:     "4.17.21",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})
	mock.AddScore("lodash", "4.17.21", 92, 88)

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install lodash", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEvaluate_DenyPinnedLowScore(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddScore("bad-pkg", "1.0.0", 30, 30)
	mock.Scores["bad-pkg@1.0.0"].PublishedAt = time.Now().Add(-240 * time.Hour)

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install bad-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for low score pinned package, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEvaluate_AskRewriteUnpinned(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	// Latest version is too new
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-1 * time.Hour), // 1 hour old
		Score:       api.PackageScore{SupplyChain: 90, Overall: 85},
	})
	// Older version is acceptable
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "1.9.0",
		PublishedAt: time.Now().Add(-720 * time.Hour), // 30 days old
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install new-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for rewritten package, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand == "" {
		t.Error("expected rewritten command")
	}
	if result.RewrittenCommand != "npm install new-pkg@1.9.0" {
		t.Errorf("expected rewritten to 1.9.0, got %s", result.RewrittenCommand)
	}
}

func TestEvaluate_AutoRewriteAllows(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

	// Latest version is too new
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 85},
	})
	// Older version is fine
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "1.9.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install new-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow when auto-rewrite is enabled, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "npm install new-pkg@1.9.0" {
		t.Errorf("expected rewritten command, got %q", result.RewrittenCommand)
	}
}

func TestEvaluate_ProviderOutageCI(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.Available = false

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install axios", api.ModeCI)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny in CI with provider outage, got %s", result.Decision)
	}
}

func TestEvaluate_ProviderOutageLocal(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.Available = false

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install axios", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask locally with provider outage, got %s", result.Decision)
	}
}

func TestEvaluate_NonInstallCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm run test", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for non-install command, got %s", result.Decision)
	}
}

func TestEvaluate_NonNPMCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "git status", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for non-npm command, got %s", result.Decision)
	}
}

func TestEvaluate_DisabledPackageManager(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PackageManagers.PNPM = false
	mock := provider.NewMockProvider()

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "pnpm add axios", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for disabled package manager, got %s", result.Decision)
	}
	if !strings.Contains(result.Reason, "not enabled") {
		t.Errorf("expected reason about disabled pm, got %q", result.Reason)
	}
}

func TestEvaluate_ReasonAggregation(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	// Two packages, both fail
	mock.AddScore("bad1", "1.0.0", 30, 30)
	mock.Scores["bad1@1.0.0"].PublishedAt = time.Now().Add(-240 * time.Hour)
	mock.AddScore("bad2", "2.0.0", 25, 25)
	mock.Scores["bad2@2.0.0"].PublishedAt = time.Now().Add(-240 * time.Hour)

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install bad1@1.0.0 bad2@2.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny, got %s", result.Decision)
	}
	// Reason should mention both packages
	if !strings.Contains(result.Reason, "bad1") || !strings.Contains(result.Reason, "bad2") {
		t.Errorf("expected reason to mention both packages, got %q", result.Reason)
	}
}

func TestEvaluate_PreActionFlags(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("react", api.VersionInfo{
		Version:     "18.2.0",
		PublishedAt: time.Now().Add(-2160 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 92},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "pnpm --filter web add react", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for pnpm --filter add, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEvaluate_GrayBandAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	// Package with gray band scores (between 50 and 70)
	mock.AddVersion("gray-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 60, Overall: 65},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install gray-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask for gray band package, got %s: %s", result.Decision, result.Reason)
	}
}
