package cli

import (
	"context"
	"testing"
	"time"

	"github.com/hammadtq/attach-dev/attach-guard/internal/config"
	"github.com/hammadtq/attach-dev/attach-guard/internal/provider"
	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
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
