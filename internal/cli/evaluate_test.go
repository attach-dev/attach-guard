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

func TestEvaluate_SuspiciousUnparsedInstall(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	eval := NewEvaluator(cfg, mock)

	// An unknown wrapper around npm install should be denied, not allowed
	suspicious := []string{
		"strace npm install axios",
		"nohup npm install axios",
		"some-wrapper npm install lodash",
		"strace bash -c 'npm install axios'",
		"nohup bash -lc 'npm install lodash'",
	}
	for _, cmd := range suspicious {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatal(err)
		}
		if result.Decision != api.Deny {
			t.Errorf("expected Deny for suspicious unparsed install %q, got %s", cmd, result.Decision)
		}
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

func TestEvaluate_MixedPackageManagers_EnabledSegmentStillEvaluated(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PackageManagers.NPM = false
	cfg.PackageManagers.PNPM = true
	cfg.Policy.Denylist = []string{"evil-pkg"}
	mock := provider.NewMockProvider()
	mock.AddScore("evil-pkg", "1.0.0", 90, 90)
	mock.Scores["evil-pkg@1.0.0"].PublishedAt = time.Now().Add(-240 * time.Hour)

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install safe-pkg && pnpm add evil-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for enabled pnpm segment, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "evil-pkg") {
		t.Errorf("expected reason to mention evil-pkg, got %q", result.Reason)
	}
}

func TestEvaluate_ChainedInsideBashC_EvaluatesAllInnerSegments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.Allowlist = []string{"safe-pkg"}
	cfg.Policy.Denylist = []string{"evil-pkg"}
	mock := provider.NewMockProvider()
	mock.AddScore("safe-pkg", "1.0.0", 90, 90)
	mock.Scores["safe-pkg@1.0.0"].PublishedAt = time.Now().Add(-240 * time.Hour)
	mock.AddScore("evil-pkg", "1.0.0", 90, 90)
	mock.Scores["evil-pkg@1.0.0"].PublishedAt = time.Now().Add(-240 * time.Hour)

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "bash -c 'npm install safe-pkg@1.0.0 && npm install evil-pkg@1.0.0'", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny for later denied install inside bash -c, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "evil-pkg") {
		t.Errorf("expected reason to mention evil-pkg, got %q", result.Reason)
	}
}

func TestEvaluate_WrappedLaterSegmentInsideBashCDoesNotFailClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.AddScore("lodash", "4.17.21", 92, 88)
	mock.Scores["lodash@4.17.21"].PublishedAt = time.Now().Add(-240 * time.Hour)

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "bash -c 'echo hi && env npm install lodash@4.17.21'", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow for wrapped later segment inside bash -c, got %s: %s", result.Decision, result.Reason)
	}
}

func TestEvaluate_BackgroundedInstallSegmentIsEvaluated(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.AddScore("lodash", "4.17.21", 92, 88)
	mock.Scores["lodash@4.17.21"].PublishedAt = time.Now().Add(-240 * time.Hour)

	tests := []string{
		"echo hi & npm install lodash@4.17.21",
		"bash -c 'echo hi & npm install lodash@4.17.21'",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Allow {
			t.Errorf("expected Allow for backgrounded install %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
	}
}

func TestEvaluate_CommandNeedingRewriteButNotSafelyRewritableAsks(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 85},
	})
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "1.9.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	tests := []string{
		"echo hello && npm install new-pkg",
		"env NODE_ENV=production npm install new-pkg",
		"sudo npm install new-pkg",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Ask {
			t.Errorf("expected Ask for non-rewritable command %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != "" {
			t.Errorf("expected no rewritten command for %q, got %q", cmd, result.RewrittenCommand)
		}
		if !strings.Contains(result.Reason, "could not be safely rewritten") {
			t.Errorf("expected reason about safe rewrite for %q, got %q", cmd, result.Reason)
		}
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

func TestEvaluate_RecognizedButNotGuardedCommandsAllow(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	tests := []string{
		"pip install .",
		"pip install -r requirements.txt",
		"pip install https://github.com/user/repo/archive/main.tar.gz",
		"pip install requests>=2.0",
		"pip install requests[security]",
		"pip install requests --index-url https://custom.pypi.org/simple",
		"pip install requests --extra-index-url https://custom.pypi.org/simple",
		"go get ./...",
		"cargo add --git https://github.com/user/repo",
		"cargo add --path ./local-crate",
		"cargo add serde --registry internal",
		"cargo add serde@1.0.200",
		"python -m pip install requests",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Allow {
			t.Errorf("expected Allow for %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != "" {
			t.Errorf("expected no rewrite for %q, got %q", cmd, result.RewrittenCommand)
		}
	}
}

func TestEvaluate_UnsupportedGoSourcesAllowPassthrough(t *testing.T) {
	cfg := config.DefaultConfig()

	tests := []struct {
		name    string
		command string
		setup   func(*provider.MockProvider)
	}{
		{
			name:    "unpinned private module",
			command: "go get private.example.com/module",
			setup: func(mock *provider.MockProvider) {
				mock.VersionsErr = provider.ErrUnsupportedSource
			},
		},
		{
			name:    "pinned private module",
			command: "go get private.example.com/module@v1.2.3",
			setup: func(mock *provider.MockProvider) {
				mock.ScoreErr = provider.ErrUnsupportedSource
			},
		},
	}

	for _, tt := range tests {
		mock := provider.NewMockProvider()
		tt.setup(mock)
		eval := NewEvaluator(cfg, mock)

		result, err := eval.Evaluate(context.Background(), tt.command, api.ModeShell)
		if err != nil {
			t.Fatalf("%s: Evaluate(%q) returned error: %v", tt.name, tt.command, err)
		}
		if result.Decision != api.Allow {
			t.Fatalf("%s: expected Allow, got %s: %s", tt.name, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != "" {
			t.Fatalf("%s: expected no rewrite, got %q", tt.name, result.RewrittenCommand)
		}
		if !strings.Contains(result.Reason, "not supported for public-registry evaluation") {
			t.Fatalf("%s: expected unsupported-source reason, got %q", tt.name, result.Reason)
		}
	}
}

func TestEvaluate_MixedParsedAndUnparsedArgsForceAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

	mock.AddVersion("flask", api.VersionInfo{
		Version:     "3.0.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	tests := []string{
		"pip install . flask",
		"pip install flask requests>=2.0",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Ask {
			t.Errorf("expected Ask for mixed parsed/unparsed command %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != "" {
			t.Errorf("expected no rewritten command for %q, got %q", cmd, result.RewrittenCommand)
		}
		if !strings.Contains(result.Reason, "could not be evaluated") {
			t.Errorf("expected manual review reason for %q, got %q", cmd, result.Reason)
		}
	}
}

func TestEvaluate_NewPackageManagersCanRewrite(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

	mock.AddVersion("requests", api.VersionInfo{
		Version:     "2.32.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 88},
	})
	mock.AddVersion("requests", api.VersionInfo{
		Version:     "2.31.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 90},
	})
	mock.AddVersion("golang.org/x/net", api.VersionInfo{
		Version:     "v0.26.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 88},
	})
	mock.AddVersion("golang.org/x/net", api.VersionInfo{
		Version:     "v0.25.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 90},
	})
	mock.AddVersion("serde", api.VersionInfo{
		Version:     "1.0.201",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 88},
	})
	mock.AddVersion("serde", api.VersionInfo{
		Version:     "1.0.200",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 90},
	})

	tests := []struct {
		command  string
		expected string
	}{
		{"pip install requests", "pip install requests==2.31.0"},
		{"go get golang.org/x/net", "go get golang.org/x/net@v0.25.0"},
		{"cargo add serde", "cargo add serde@=1.0.200"},
	}

	eval := NewEvaluator(cfg, mock)
	for _, tt := range tests {
		result, err := eval.Evaluate(context.Background(), tt.command, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", tt.command, err)
		}
		if result.Decision != api.Allow {
			t.Errorf("expected Allow for %q, got %s: %s", tt.command, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != tt.expected {
			t.Errorf("expected rewritten command %q, got %q", tt.expected, result.RewrittenCommand)
		}
	}
}

func TestEvaluate_DisabledNewPackageManagersAllowPassthrough(t *testing.T) {
	tests := []struct {
		command string
		disable func(*config.Config)
	}{
		{"pip install requests", func(cfg *config.Config) { cfg.PackageManagers.Pip = false }},
		{"go get golang.org/x/net", func(cfg *config.Config) { cfg.PackageManagers.Go = false }},
		{"cargo add serde", func(cfg *config.Config) { cfg.PackageManagers.Cargo = false }},
	}

	for _, tt := range tests {
		cfg := config.DefaultConfig()
		tt.disable(cfg)
		mock := provider.NewMockProvider()
		eval := NewEvaluator(cfg, mock)

		result, err := eval.Evaluate(context.Background(), tt.command, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", tt.command, err)
		}
		if result.Decision != api.Allow {
			t.Errorf("expected Allow for disabled PM on %q, got %s", tt.command, result.Decision)
		}
		if !strings.Contains(result.Reason, "not enabled") {
			t.Errorf("expected disabled PM reason for %q, got %q", tt.command, result.Reason)
		}
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
