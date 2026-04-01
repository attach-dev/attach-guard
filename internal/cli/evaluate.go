// Package cli implements the attach-guard CLI commands.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/attach-dev/attach-guard/internal/audit"
	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/parser"
	"github.com/attach-dev/attach-guard/internal/policy"
	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/internal/rewrite"
	"github.com/attach-dev/attach-guard/internal/versionselect"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// Evaluator runs package evaluation against policy.
type Evaluator struct {
	cfg    *config.Config
	prov   provider.Provider
	engine *policy.Engine
	logger *audit.Logger
}

// NewEvaluator creates a new evaluator.
func NewEvaluator(cfg *config.Config, prov provider.Provider) *Evaluator {
	return &Evaluator{
		cfg:    cfg,
		prov:   prov,
		engine: policy.NewEngine(cfg),
		logger: audit.NewLogger(cfg.ResolveLogPath()),
	}
}

// Evaluate evaluates a raw command string and returns the result.
func (e *Evaluator) Evaluate(ctx context.Context, rawCommand string, mode api.Mode) (*api.EvaluationResult, error) {
	cmd := parser.Parse(rawCommand)
	if cmd == nil {
		// Not an install command — allow passthrough
		return &api.EvaluationResult{
			Decision:        api.Allow,
			Reason:          "not a guarded install command",
			OriginalCommand: rawCommand,
		}, nil
	}

	// Check if the package manager is enabled in config
	if (cmd.PackageManager == "npm" && !e.cfg.PackageManagers.NPM) ||
		(cmd.PackageManager == "pnpm" && !e.cfg.PackageManagers.PNPM) {
		return &api.EvaluationResult{
			Decision:        api.Allow,
			Reason:          fmt.Sprintf("%s is not enabled in config", cmd.PackageManager),
			OriginalCommand: rawCommand,
		}, nil
	}

	provAvailable := e.prov.IsAvailable(ctx)

	var packages []api.PackageEvaluation
	var overallDecision api.Decision = api.Allow
	var reasons []string
	selectedVersions := make(map[string]string)
	anyRewritten := false

	selector := versionselect.NewSelector(e.prov, e.engine, e.cfg)

	for _, pkg := range cmd.Packages {
		eval := api.PackageEvaluation{
			Ecosystem: pkg.Ecosystem,
			Name:      pkg.Name,
			Requested: pkg.Version,
		}

		if !provAvailable {
			// Handle provider unavailable
			decision := e.engine.Evaluate(policy.Input{
				Ecosystem:         pkg.Ecosystem,
				Name:              pkg.Name,
				ProviderAvailable: false,
				Mode:              mode,
			})
			eval.SelectedVersion = pkg.Version
			packages = append(packages, eval)
			overallDecision = worseDecision(overallDecision, decision.Decision)
			if decision.Decision != api.Allow {
				reasons = append(reasons, fmt.Sprintf("%s: %s", pkg.Name, decision.Reason))
			}
			continue
		}

		result, err := selector.Select(ctx, pkg, mode)
		if err != nil {
			return nil, fmt.Errorf("evaluating %s: %w", pkg.Name, err)
		}

		if result.AllFailed {
			eval.SelectedVersion = ""
			packages = append(packages, eval)
			overallDecision = api.Deny
			reasons = append(reasons, fmt.Sprintf("no acceptable version found for %s", pkg.Name))
			continue
		}

		v := result.Selected
		eval.SelectedVersion = v.Version
		eval.Score = v.Score
		eval.AgeHours = time.Since(v.PublishedAt).Hours()
		eval.Alerts = v.Alerts
		packages = append(packages, eval)

		if pkg.Pinned {
			// Use the selector's policy decision for the pinned version
			overallDecision = worseDecision(overallDecision, result.Decision)
			if result.Decision != api.Allow {
				reasons = append(reasons, fmt.Sprintf("%s@%s: does not pass policy", pkg.Name, pkg.Version))
			}
		} else {
			// Unpinned — use version selection result
			selectedVersions[pkg.Name] = v.Version

			// Apply the selector's policy decision
			overallDecision = worseDecision(overallDecision, result.Decision)

			if result.WasRewritten {
				anyRewritten = true
				if !e.engine.ShouldAutoRewrite(mode) {
					overallDecision = worseDecision(overallDecision, api.Ask)
				}
				reasons = append(reasons, fmt.Sprintf("latest version of %s does not pass policy; suggesting %s@%s", pkg.Name, pkg.Name, v.Version))
			} else if result.Decision == api.Ask {
				reasons = append(reasons, fmt.Sprintf("%s: scores are in review range", pkg.Name))
			}
		}
	}

	overallReason := strings.Join(reasons, "; ")

	evalResult := &api.EvaluationResult{
		Decision:        overallDecision,
		Reason:          overallReason,
		OriginalCommand: rawCommand,
		Packages:        packages,
	}

	if anyRewritten {
		evalResult.RewrittenCommand = rewrite.Command(cmd, selectedVersions)
	}

	// Audit log
	provName := "unknown"
	if e.prov != nil {
		provName = e.prov.Name()
	}
	_ = e.logger.Log(audit.Entry{
		PackageManager:   cmd.PackageManager,
		OriginalCommand:  rawCommand,
		RewrittenCommand: evalResult.RewrittenCommand,
		Decision:         overallDecision,
		Reason:           overallReason,
		Packages:         packages,
		Provider:         provName,
		Mode:             string(mode),
	})

	return evalResult, nil
}

// EvaluateJSON evaluates and returns JSON bytes.
func (e *Evaluator) EvaluateJSON(ctx context.Context, rawCommand string, mode api.Mode) ([]byte, error) {
	result, err := e.Evaluate(ctx, rawCommand, mode)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(result, "", "  ")
}

func worseDecision(a, b api.Decision) api.Decision {
	rank := map[api.Decision]int{
		api.Allow: 0,
		api.Ask:   1,
		api.Deny:  2,
	}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
