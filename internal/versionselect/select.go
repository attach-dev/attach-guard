// Package versionselect implements version selection for unpinned packages.
package versionselect

import (
	"context"
	"fmt"

	"github.com/hammadtq/attach-dev/attach-guard/internal/config"
	"github.com/hammadtq/attach-dev/attach-guard/internal/policy"
	"github.com/hammadtq/attach-dev/attach-guard/internal/provider"
	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
)

// Result holds the version selection outcome.
type Result struct {
	Selected    *api.VersionInfo
	WasRewritten bool
	AllFailed   bool
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
			return nil, fmt.Errorf("evaluating pinned version %s@%s: %w", pkg.Name, pkg.Version, err)
		}
		return &Result{Selected: info, WasRewritten: false}, nil
	}

	// Unpinned: fetch candidate versions
	versions, err := s.prov.ListVersions(ctx, pkg.Ecosystem, pkg.Name)
	if err != nil {
		return nil, fmt.Errorf("listing versions for %s: %w", pkg.Name, err)
	}

	if len(versions) == 0 {
		return &Result{AllFailed: true}, nil
	}

	// Versions should be sorted newest-first from the provider
	// Try each version from newest to oldest
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
				WasRewritten: i > 0, // rewritten if not the latest
			}, nil
		}
	}

	return &Result{AllFailed: true}, nil
}
