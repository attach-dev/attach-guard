// Package cli implements the attach-guard CLI commands.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
	cmds := parser.ParseAll(rawCommand)
	if len(cmds) == 0 {
		// Parser could not fully classify the command. Check if it still
		// looks like a package install wrapped in an unrecognized prefix.
		// If so, block rather than silently allowing — fail closed.
		if parser.LooksLikeInstall(rawCommand) {
			return &api.EvaluationResult{
				Decision:        api.Deny,
				Reason:          "command looks like a package install but could not be fully parsed; blocking for safety",
				OriginalCommand: rawCommand,
			}, nil
		}
		// Not an install command — allow passthrough
		return &api.EvaluationResult{
			Decision:        api.Allow,
			Reason:          "not a guarded install command",
			OriginalCommand: rawCommand,
		}, nil
	}

	enabledCmds := make([]*api.ParsedCommand, 0, len(cmds))
	disabledPMs := make([]string, 0, len(cmds))
	seenDisabledPMs := make(map[string]bool)
	for _, cmd := range cmds {
		if !e.packageManagerEnabled(cmd.PackageManager) {
			if !seenDisabledPMs[cmd.PackageManager] {
				disabledPMs = append(disabledPMs, cmd.PackageManager)
				seenDisabledPMs[cmd.PackageManager] = true
			}
			continue
		}
		enabledCmds = append(enabledCmds, cmd)
	}
	if len(enabledCmds) == 0 {
		return &api.EvaluationResult{
			Decision:        api.Allow,
			Reason:          disabledPackageManagersReason(disabledPMs),
			OriginalCommand: rawCommand,
		}, nil
	}

	provAvailable := e.prov.IsAvailable(ctx)
	canRewriteShape := len(cmds) == 1 && len(enabledCmds) == 1 && rewriteEligible(rawCommand, enabledCmds[0])

	var packages []api.PackageEvaluation
	var overallDecision api.Decision = api.Allow
	var reasons []string
	selectedVersions := make(map[string]string)
	anyRewritten := false
	unsupportedByCmd := make(map[int]int)
	evaluatedByCmd := make(map[int]int)

	selector := versionselect.NewSelector(e.prov, e.engine, e.cfg)

	for cmdIdx, cmd := range enabledCmds {
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

			if result.UnsupportedSource {
				packages = append(packages, eval)
				unsupportedByCmd[cmdIdx]++
				reasons = append(reasons, fmt.Sprintf("%s: %s", pkg.Name, result.Reason))
				continue
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
			evaluatedByCmd[cmdIdx]++

			if pkg.Pinned {
				// Use the selector's policy decision for the pinned version
				overallDecision = worseDecision(overallDecision, result.Decision)
				if result.Decision != api.Allow {
					reasons = append(reasons, fmt.Sprintf("%s@%s: %s", pkg.Name, pkg.Version, result.Reason))
				}
				continue
			}

			// Unpinned — use version selection result
			if canRewriteShape {
				selectedVersions[pkg.Name] = v.Version
			}

			// Apply the selector's policy decision
			overallDecision = worseDecision(overallDecision, result.Decision)

			if result.WasRewritten {
				anyRewritten = true
				if !canRewriteShape || !e.engine.ShouldAutoRewrite(mode) {
					overallDecision = worseDecision(overallDecision, api.Ask)
				}
				if !canRewriteShape {
					reasons = append(reasons, fmt.Sprintf("%s: manual review required because the command could not be safely rewritten", pkg.Name))
				}
				reasons = append(reasons, fmt.Sprintf("latest version of %s does not pass policy; suggesting %s@%s", pkg.Name, pkg.Name, v.Version))
			} else if result.Decision == api.Ask {
				reasons = append(reasons, fmt.Sprintf("%s: scores are in review range", pkg.Name))
			}
		}
	}

	anyUnsupportedSource := false
	for cmdIdx, cmd := range enabledCmds {
		if unsupportedByCmd[cmdIdx] > 0 {
			anyUnsupportedSource = true
			if evaluatedByCmd[cmdIdx] > 0 {
				overallDecision = worseDecision(overallDecision, api.Ask)
				reasons = append(reasons, fmt.Sprintf("%s: command contains package sources that could not be evaluated; manual review required", cmd.PackageManager))
			}
		}
		if cmd.HasUnparsedArgs && len(cmd.Packages) > 0 {
			overallDecision = worseDecision(overallDecision, api.Ask)
			reasons = append(reasons, fmt.Sprintf("%s: command contains arguments that could not be evaluated; manual review required", cmd.PackageManager))
			break
		}
	}

	overallReason := strings.Join(reasons, "; ")

	evalResult := &api.EvaluationResult{
		Decision:        overallDecision,
		Reason:          overallReason,
		OriginalCommand: rawCommand,
		Packages:        packages,
	}

	if anyRewritten && canRewriteShape && !anyUnsupportedSource {
		evalResult.RewrittenCommand = rewrite.Command(enabledCmds[0], selectedVersions)
	}

	// Audit log
	provName := "unknown"
	if e.prov != nil {
		provName = e.prov.Name()
	}
	auditPM := enabledCmds[0].PackageManager
	for _, cmd := range enabledCmds[1:] {
		if cmd.PackageManager != auditPM {
			auditPM = "multiple"
			break
		}
	}
	_ = e.logger.Log(audit.Entry{
		PackageManager:   auditPM,
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

func (e *Evaluator) packageManagerEnabled(pm string) bool {
	switch pm {
	case "npm":
		return e.cfg.PackageManagers.NPM
	case "pnpm":
		return e.cfg.PackageManagers.PNPM
	case "pip", "pip3":
		return e.cfg.PackageManagers.Pip
	case "go":
		return e.cfg.PackageManagers.Go
	case "cargo":
		return e.cfg.PackageManagers.Cargo
	default:
		return true
	}
}

func disabledPackageManagersReason(disabledPMs []string) string {
	switch len(disabledPMs) {
	case 0:
		return "all detected package managers are disabled in config"
	case 1:
		return fmt.Sprintf("%s is not enabled in config", disabledPMs[0])
	default:
		return fmt.Sprintf("%s are not enabled in config", strings.Join(disabledPMs, ", "))
	}
}

func rewriteEligible(rawCommand string, cmd *api.ParsedCommand) bool {
	if cmd.HasUnparsedArgs {
		return false
	}
	tokens := parser.Tokenize(rawCommand)
	if len(tokens) == 0 || filepath.Base(tokens[0]) != cmd.PackageManager {
		return false
	}
	for _, tok := range tokens[1:] {
		switch tok {
		case "&&", "||", ";", "|", "&":
			return false
		}
	}
	return true
}
