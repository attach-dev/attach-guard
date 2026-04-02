// Package versionselect implements version selection for unpinned packages.
package versionselect

import (
	"context"
	"fmt"

	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/policy"
	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// Result holds the version selection outcome.
type Result struct {
	Selected     *api.VersionInfo
	WasRewritten bool
	AllFailed    bool
	Decision     api.Decision // the policy decision for the selected version
	Reason       string       // the policy reason for the selected version
}

// Selector picks the best acceptable version for an unpinned package.
type Selector struct {
	prov   provider.Provider
	engine *policy.Engine
	cfg    *config.Config
}

// NewSelector creates a version selector.
func NewSelector(prov provider.Provider, engine *policy.Engine, cfg *config.Config) *Selector {
	return &Selector{prov: prov, engine: engine, cfg: cfg}
}

// Select finds the best acceptable version for a package.
// If the package is pinned, it evaluates that version directly.
func (s *Selector) Select(ctx context.Context, pkg api.PackageRequest, mode api.Mode) (*Result, error) {
	if pkg.Pinned {
		info, err := s.prov.GetPackageScore(ctx, pkg.Ecosystem, pkg.Name, pkg.Version)
		if err != nil {
			// Version not found or provider error — deny for safety
			return &Result{
				AllFailed: true,
				Decision:  api.Deny,
				Reason:    fmt.Sprintf("could not score %s@%s: %v", pkg.Name, pkg.Version, err),
			}, nil
		}
		input := policy.Input{
			Ecosystem:         pkg.Ecosystem,
			Name:              pkg.Name,
			RequestedSpec:     pkg.Version,
			ResolvedVersion:   info.Version,
			Score:             info.Score,
			Alerts:            info.Alerts,
			PublishedAt:       info.PublishedAt,
			ProviderAvailable: true,
			Mode:              mode,
			Pinned:            true,
		}
		decision := s.engine.Evaluate(input)
		return &Result{Selected: info, WasRewritten: false, Decision: decision.Decision, Reason: decision.Reason}, nil
	}

	// Unpinned: fetch candidate versions
	versions, err := s.prov.ListVersions(ctx, pkg.Ecosystem, pkg.Name)
	if err != nil {
		return nil, fmt.Errorf("listing versions for %s: %w", pkg.Name, err)
	}

	if len(versions) == 0 {
		return &Result{AllFailed: true, Decision: api.Deny, Reason: "no versions found"}, nil
	}

	// Versions should be sorted newest-first from the provider.
	// First pass: find the newest version that gets Allow.
	// Second pass: if none found, find the newest version that gets Ask.
	var bestAsk *Result
	for i, v := range versions {
		if v.Deprecated {
			continue
		}

		input := policy.Input{
			Ecosystem:         pkg.Ecosystem,
			Name:              pkg.Name,
			RequestedSpec:     pkg.Version,
			ResolvedVersion:   v.Version,
			Score:             v.Score,
			Alerts:            v.Alerts,
			PublishedAt:       v.PublishedAt,
			ProviderAvailable: true,
			Mode:              mode,
			Pinned:            false,
		}

		decision := s.engine.Evaluate(input)
		if decision.Decision == api.Allow {
			return &Result{
				Selected:     &versions[i],
				WasRewritten: i > 0,
				Decision:     api.Allow,
				Reason:       decision.Reason,
			}, nil
		}
		if decision.Decision == api.Ask && bestAsk == nil {
			bestAsk = &Result{
				Selected:     &versions[i],
				WasRewritten: i > 0,
				Decision:     api.Ask,
				Reason:       decision.Reason,
			}
		}
	}

	// No Allow version found; fall back to best Ask version
	if bestAsk != nil {
		return bestAsk, nil
	}

	return &Result{AllFailed: true, Decision: api.Deny, Reason: "all candidate versions fail policy"}, nil
}
