package cli

import (
	"context"
	"encoding/json"
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

func TestEvaluate_PinnedOpenScoreVerdictDeny(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	riskScore := 95

	mock.Scores["risky-pkg@1.0.0"] = &api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision:   api.ProviderVerdictDeny,
			RiskScore:  &riskScore,
			Confidence: "HIGH",
			Reasons:    []string{"high-risk-synthetic"},
			SourceRefs: []string{
				"osv:GHSA-0000-0000-0000",
				"deps.dev:npm/risky-pkg/1.0.0",
			},
		},
	}

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install risky-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Errorf("expected Deny from Open Score verdict, got %s: %s", result.Decision, result.Reason)
	}
	if len(result.Packages) != 1 || result.Packages[0].ProviderVerdict == nil {
		t.Fatalf("expected package evaluation to preserve provider verdict, got %+v", result.Packages)
	}
	if got := result.Packages[0].ProviderVerdict.SourceRefs; len(got) != 2 || got[0] != "osv:GHSA-0000-0000-0000" {
		t.Fatalf("expected provider source refs to be preserved for audit/explain output, got %#v", got)
	}
	if got := result.Packages[0].ProviderVerdict.Confidence; got != "HIGH" {
		t.Fatalf("expected provider confidence to be preserved for audit/explain output, got %q", got)
	}
}

func TestEvaluateJSON_PreservesOpenScoreVerdictConfidenceAndSourceRefs(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	riskScore := 42

	mock.Scores["review-pkg@1.0.0"] = &api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision:   api.ProviderVerdictAsk,
			RiskScore:  &riskScore,
			Confidence: "MEDIUM",
			Reasons:    []string{"manual-review-synthetic"},
			SourceRefs: []string{"deps.dev:npm/review-pkg/1.0.0"},
		},
	}

	eval := NewEvaluator(cfg, mock)
	data, err := eval.EvaluateJSON(context.Background(), "npm install review-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"confidence": "MEDIUM"`) {
		t.Fatalf("expected evaluation JSON to include confidence, got %s", data)
	}

	var result api.EvaluationResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Packages) != 1 || result.Packages[0].ProviderVerdict == nil {
		t.Fatalf("expected evaluation JSON to preserve provider verdict, got %+v", result.Packages)
	}
	verdict := result.Packages[0].ProviderVerdict
	if verdict.Decision != api.ProviderVerdictAsk {
		t.Fatalf("expected ASK verdict, got %s", verdict.Decision)
	}
	if verdict.RiskScore == nil || *verdict.RiskScore != riskScore {
		t.Fatalf("expected risk score %d, got %v", riskScore, verdict.RiskScore)
	}
	if verdict.Confidence != "MEDIUM" {
		t.Fatalf("expected confidence MEDIUM, got %q", verdict.Confidence)
	}
	if len(verdict.Reasons) != 1 || verdict.Reasons[0] != "manual-review-synthetic" {
		t.Fatalf("expected reason preserved, got %#v", verdict.Reasons)
	}
	if len(verdict.SourceRefs) != 1 || verdict.SourceRefs[0] != "deps.dev:npm/review-pkg/1.0.0" {
		t.Fatalf("expected source refs preserved, got %#v", verdict.SourceRefs)
	}
}

func TestEvaluate_PinnedOpenScoreUnknownLocalAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.Scores["unknown-pkg@1.0.0"] = &api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictUnknown,
			Reasons:  []string{"insufficient-evidence-synthetic"},
		},
	}

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install unknown-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask from local UNKNOWN verdict, got %s: %s", result.Decision, result.Reason)
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
		"strace pip --proxy http://proxy.example install flask",
		"strace pip -i https://custom.example/simple install flask",
		"strace cargo --color always add serde",
		"strace cargo --mystery value install ripgrep",
		"strace uv --project /tmp pip install requests",
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

func TestEvaluate_LocalRecognizedButNotGuardedCommandsAllow(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	tests := []string{
		"pip install .",
		"pip install dist/pkg.whl",
		"pip install file:///tmp/pkg.whl",
		"go get ./...",
		"go install ./...",
		"go install .",
		"cargo add --path ./local-crate",
		"cargo install --path ./local-crate",
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

func TestEvaluate_LocalFindLinksForcesManualReview(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	tests := []string{
		"pip install --find-links ./dist flask",
		"PIP_FIND_LINKS=./dist pip install flask",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Ask {
			t.Fatalf("expected Ask for %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if len(result.Packages) != 0 {
			t.Fatalf("expected no evaluated packages for %q, got %#v", cmd, result.Packages)
		}
		if result.RewrittenCommand != "" {
			t.Fatalf("expected no rewrite for %q, got %q", cmd, result.RewrittenCommand)
		}
		if !strings.Contains(result.Reason, "non-local arguments") {
			t.Fatalf("expected manual-review reason for %q, got %q", cmd, result.Reason)
		}
	}
}

func TestEvaluate_NonLocalUnparsedCommandsAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.AddVersion("ripgrep", api.VersionInfo{
		Version:     "14.0.0",
		PublishedAt: time.Now().Add(-48 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 91},
	})
	mock.AddVersion("fd-find", api.VersionInfo{
		Version:     "8.7.1",
		PublishedAt: time.Now().Add(-48 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 94, Overall: 90},
	})

	tests := []string{
		"pip --proxy http://proxy.example install flask",
		"pip install --find-links ./dist flask",
		"PIP_FIND_LINKS=./dist pip install flask",
		"pip install -r requirements.txt",
		"pip install https://github.com/user/repo/archive/main.tar.gz",
		"pip install git+https://github.com/user/repo.git",
		"pip install 'requests>=2.0'",
		"pip install requests[security]",
		"pip install requests --index-url https://custom.pypi.org/simple",
		"pip install requests --index-url=https://custom.pypi.org/simple",
		"pip install requests --extra-index-url https://custom.pypi.org/simple",
		"pip install --requirement=requirements.txt",
		"PIP_INDEX_URL=file:///tmp/simple pip install requests",
		"PIP_INDEX_URL=https://private.example/simple pip install requests",
		"cargo add --git https://github.com/user/repo",
		"cargo add serde --registry internal",
		"cargo add serde --registry=internal",
		"cargo add serde@1.0.200",
		"cargo install --git https://github.com/user/repo",
		"cargo install ripgrep fd-find --version 1.2.3",
		"cargo --mystery value install ripgrep",
		"go get golang.org/x/net@upgrade",
		"GOPRIVATE=private.example.com go get private.example.com/mod",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Ask {
			t.Errorf("expected Ask for %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != "" {
			t.Errorf("expected no rewrite for %q, got %q", cmd, result.RewrittenCommand)
		}
	}
}

func TestEvaluate_DynamicCommandSubstitutionAsksEvenWhenNPMDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PackageManagers.NPM = false
	cfg.PackageManagers.PNPM = true
	eval := NewEvaluator(cfg, provider.NewMockProvider())

	result, err := eval.Evaluate(context.Background(), "$(printf 'pnpm add leftpad')", api.ModeShell)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("Decision = %s, want Ask: %s", result.Decision, result.Reason)
	}
}

func TestEvaluate_CommonBooleanFlagsStillEvaluatePackages(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("flask", api.VersionInfo{
		Version:     "3.0.0",
		PublishedAt: time.Now().Add(-48 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})
	mock.AddVersion("serde", api.VersionInfo{
		Version:     "1.0.200",
		PublishedAt: time.Now().Add(-48 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 94, Overall: 90},
	})
	mock.AddVersion("ripgrep", api.VersionInfo{
		Version:     "14.0.0",
		PublishedAt: time.Now().Add(-48 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 91},
	})

	tests := []string{
		"pip install --upgrade flask",
		"pip install flask --target=/tmp",
		"cargo --color always add serde",
		"cargo --color=always add serde",
		"cargo add --optional serde",
		"cargo --color always install ripgrep",
		"cargo --color=always install ripgrep",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Allow {
			t.Fatalf("expected Allow for %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if len(result.Packages) != 1 {
			t.Fatalf("expected one evaluated package for %q, got %d", cmd, len(result.Packages))
		}
	}
}

func TestEvaluate_UnsupportedGoSourcesAsk(t *testing.T) {
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
		if result.Decision != api.Ask {
			t.Fatalf("%s: expected Ask, got %s: %s", tt.name, result.Decision, result.Reason)
		}
		if result.RewrittenCommand != "" {
			t.Fatalf("%s: expected no rewrite, got %q", tt.name, result.RewrittenCommand)
		}
		if !strings.Contains(result.Reason, "not supported for public-registry evaluation") {
			t.Fatalf("%s: expected unsupported-source reason, got %q", tt.name, result.Reason)
		}
	}
}

func TestEvaluate_MixedLocalAndParsedArgsAllow(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

	mock.AddVersion("flask", api.VersionInfo{
		Version:     "3.0.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "pip install . flask", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Fatalf("expected Allow for mixed local/parsed command, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "" {
		t.Fatalf("expected no rewritten command, got %q", result.RewrittenCommand)
	}
}

func TestEvaluate_MixedNonLocalAndParsedArgsForceAsk(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

	mock.AddVersion("flask", api.VersionInfo{
		Version:     "3.0.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "pip install flask 'requests>=2.0'", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("expected Ask for mixed non-local/parsed command, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "" {
		t.Fatalf("expected no rewritten command, got %q", result.RewrittenCommand)
	}
	if !strings.Contains(result.Reason, "non-local arguments") {
		t.Fatalf("expected non-local manual review reason, got %q", result.Reason)
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
	mock.AddVersion("golang.org/x/tools/cmd/godoc", api.VersionInfo{
		Version:     "v0.21.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 88},
	})
	mock.AddVersion("golang.org/x/tools/cmd/godoc", api.VersionInfo{
		Version:     "v0.20.0",
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
	mock.AddVersion("ripgrep", api.VersionInfo{
		Version:     "14.1.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 88},
	})
	mock.AddVersion("ripgrep", api.VersionInfo{
		Version:     "14.0.0",
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
		{"go install golang.org/x/tools/cmd/godoc", "go install golang.org/x/tools/cmd/godoc@v0.20.0"},
		{"cargo install ripgrep", "cargo install ripgrep@=14.0.0"},
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

func TestEvaluate_UVPipValueTakingGlobalFlagsStillEvaluatePackages(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("requests", api.VersionInfo{
		Version:     "2.31.0",
		PublishedAt: time.Now().Add(-240 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 90},
	})

	tests := []string{
		"uv --project /tmp pip install requests",
		"uv --directory=/tmp pip install requests",
		"uv -p 3.13 pip install requests",
		"uv pip install -p 3.13 requests",
	}

	eval := NewEvaluator(cfg, mock)
	for _, cmd := range tests {
		result, err := eval.Evaluate(context.Background(), cmd, api.ModeShell)
		if err != nil {
			t.Fatalf("Evaluate(%q) returned error: %v", cmd, err)
		}
		if result.Decision != api.Allow {
			t.Fatalf("expected Allow for %q, got %s: %s", cmd, result.Decision, result.Reason)
		}
		if len(result.Packages) != 1 || result.Packages[0].Name != "requests" {
			t.Fatalf("expected requests to be evaluated for %q, got %#v", cmd, result.Packages)
		}
		if result.RewrittenCommand != "" {
			t.Fatalf("expected no rewrite for %q, got %q", cmd, result.RewrittenCommand)
		}
	}
}

func TestEvaluate_UVPipInstallNeedsManualReviewWhenRewriteRequired(t *testing.T) {
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

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "uv --project /tmp pip install requests", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("expected Ask, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "" {
		t.Fatalf("expected no rewrite, got %q", result.RewrittenCommand)
	}
	if !strings.Contains(result.Reason, "could not be safely rewritten") {
		t.Fatalf("expected safe-rewrite reason, got %q", result.Reason)
	}
}

func TestEvaluate_GoRewritePreservesFlagPosition(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Policy.AutoRewriteUnpinned.Local = true
	mock := provider.NewMockProvider()

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

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "go get -u golang.org/x/net", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Fatalf("expected Allow, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "go get -u golang.org/x/net@v0.25.0" {
		t.Fatalf("expected go flags before package in rewrite, got %q", result.RewrittenCommand)
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

func TestEvaluate_UnpinnedOpenScoreAskPreservesVerdictReason(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("review-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictAsk,
			Reasons:  []string{"review-synthetic"},
		},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install review-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("expected Ask for Open Score ASK verdict, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "Attach Open Score verdict ASK") || !strings.Contains(result.Reason, "review-synthetic") {
		t.Fatalf("expected Open Score ASK reason to be preserved, got %q", result.Reason)
	}
}

func TestEvaluate_UnpinnedOpenScoreUnknownPreservesVerdictReason(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("unknown-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictUnknown,
			Reasons:  []string{"insufficient-evidence-synthetic"},
		},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install unknown-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("expected Ask for Open Score UNKNOWN verdict, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "Attach Open Score verdict UNKNOWN") || !strings.Contains(result.Reason, "insufficient-evidence-synthetic") {
		t.Fatalf("expected Open Score UNKNOWN reason to be preserved, got %q", result.Reason)
	}
}

func TestEvaluate_UnpinnedOpenScoreAskThenOlderAllowPreservesRejectedReason(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("review-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictAsk,
			Reasons:  []string{"review-synthetic"},
		},
	})
	mock.AddVersion("review-pkg", api.VersionInfo{
		Version:     "1.9.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictAllow,
		},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install review-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("expected Ask for rewritten unpinned Open Score result, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "review-synthetic") || !strings.Contains(result.Reason, "suggesting review-pkg@1.9.0") {
		t.Fatalf("expected rejected Open Score reason and suggestion to be preserved, got %q", result.Reason)
	}
}

func TestEvaluate_UnpinnedOpenScoreAllDenyPreservesRejectedReason(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()

	mock.AddVersion("bad-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 95},
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictDeny,
			Reasons:  []string{"deny-synthetic"},
			SourceRefs: []string{
				"ghsa:GHSA-1111-2222-3333",
			},
		},
	})

	eval := NewEvaluator(cfg, mock)
	result, err := eval.Evaluate(context.Background(), "npm install bad-pkg", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Deny {
		t.Fatalf("expected Deny for all-deny Open Score result, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "deny-synthetic") {
		t.Fatalf("expected rejected Open Score deny reason to be preserved, got %q", result.Reason)
	}
	if len(result.Packages) != 1 || result.Packages[0].ProviderVerdict == nil {
		t.Fatalf("expected all-failed package evaluation to preserve rejected provider verdict, got %+v", result.Packages)
	}
	if got := result.Packages[0].ProviderVerdict.SourceRefs; len(got) != 1 || got[0] != "ghsa:GHSA-1111-2222-3333" {
		t.Fatalf("expected all-failed source refs to be preserved for audit output, got %#v", got)
	}
}
