package versionselect

import (
	"context"
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/policy"
	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestSelect_PinnedVersion(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	engine := policy.NewEngine(cfg)

	mock.AddScore("axios", "1.7.0", 92, 88)

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemNPM,
		Name:      "axios",
		Version:   "1.7.0",
		Pinned:    true,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if result.Selected == nil {
		t.Fatal("expected selected version")
	}
	if result.WasRewritten {
		t.Error("pinned version should not be rewritten")
	}
}

func TestSelect_UnpinnedAllowsLatest(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	engine := policy.NewEngine(cfg)

	mock.AddVersion("lodash", api.VersionInfo{
		Version:     "4.17.21",
		PublishedAt: time.Now().Add(-8760 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 92},
	})

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemNPM,
		Name:      "lodash",
		Pinned:    false,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if result.Selected.Version != "4.17.21" {
		t.Errorf("expected 4.17.21, got %s", result.Selected.Version)
	}
	if result.WasRewritten {
		t.Error("latest version should not be rewritten")
	}
	if result.Decision != api.Allow {
		t.Errorf("expected Allow, got %s", result.Decision)
	}
}

func TestSelect_UnpinnedFallsBackToOlder(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	engine := policy.NewEngine(cfg)

	// Latest is too new
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-1 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 85},
	})
	// Older is fine
	mock.AddVersion("new-pkg", api.VersionInfo{
		Version:     "1.9.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 92, Overall: 88},
	})

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemNPM,
		Name:      "new-pkg",
		Pinned:    false,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if result.Selected.Version != "1.9.0" {
		t.Errorf("expected 1.9.0, got %s", result.Selected.Version)
	}
	if !result.WasRewritten {
		t.Error("expected WasRewritten=true")
	}
}

func TestSelect_AllAskDoesNotFail(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	engine := policy.NewEngine(cfg)

	// All versions in gray band (score 60, between 50 and 70)
	mock.AddVersion("gray-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 60, Overall: 65},
	})
	mock.AddVersion("gray-pkg", api.VersionInfo{
		Version:     "0.9.0",
		PublishedAt: time.Now().Add(-2160 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 55, Overall: 60},
	})

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemNPM,
		Name:      "gray-pkg",
		Pinned:    false,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if result.AllFailed {
		t.Error("all-Ask versions should not return AllFailed")
	}
	if result.Selected == nil {
		t.Fatal("expected a selected version")
	}
	if result.Decision != api.Ask {
		t.Errorf("expected Ask decision, got %s", result.Decision)
	}
	if result.Selected.Version != "1.0.0" {
		t.Errorf("expected newest Ask version 1.0.0, got %s", result.Selected.Version)
	}
}

func TestSelect_AllDenyFails(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	engine := policy.NewEngine(cfg)

	// All versions have low scores (below gray band minimum of 50)
	mock.AddVersion("bad-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 20, Overall: 20},
	})

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemNPM,
		Name:      "bad-pkg",
		Pinned:    false,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if !result.AllFailed {
		t.Error("all-Deny versions should return AllFailed")
	}
}

func TestSelect_SkipsDeprecated(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	engine := policy.NewEngine(cfg)

	mock.AddVersion("dep-pkg", api.VersionInfo{
		Version:     "2.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 95, Overall: 92},
		Deprecated:  true,
	})
	mock.AddVersion("dep-pkg", api.VersionInfo{
		Version:     "1.0.0",
		PublishedAt: time.Now().Add(-720 * time.Hour),
		Score:       api.PackageScore{SupplyChain: 90, Overall: 88},
	})

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemNPM,
		Name:      "dep-pkg",
		Pinned:    false,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if result.Selected.Version != "1.0.0" {
		t.Errorf("expected 1.0.0 (skipping deprecated 2.0.0), got %s", result.Selected.Version)
	}
}

func TestSelect_UnsupportedSourceAllowsPassthrough(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.VersionsErr = provider.ErrUnsupportedSource
	engine := policy.NewEngine(cfg)

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemGo,
		Name:      "private.example.com/module",
		Pinned:    false,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if !result.UnsupportedSource {
		t.Fatal("expected unsupported source result")
	}
	if result.Decision != api.Allow {
		t.Fatalf("expected Allow, got %s", result.Decision)
	}
	if result.Selected != nil {
		t.Fatalf("expected no selected version, got %#v", result.Selected)
	}
}

func TestSelect_PinnedUnsupportedSourceAllowsPassthrough(t *testing.T) {
	cfg := config.DefaultConfig()
	mock := provider.NewMockProvider()
	mock.ScoreErr = provider.ErrUnsupportedSource
	engine := policy.NewEngine(cfg)

	s := NewSelector(mock, engine, cfg)
	result, err := s.Select(context.Background(), api.PackageRequest{
		Ecosystem: api.EcosystemGo,
		Name:      "private.example.com/module",
		Version:   "v1.2.3",
		Pinned:    true,
	}, api.ModeShell)

	if err != nil {
		t.Fatal(err)
	}
	if !result.UnsupportedSource {
		t.Fatal("expected unsupported source result")
	}
	if result.Decision != api.Allow {
		t.Fatalf("expected Allow, got %s", result.Decision)
	}
	if result.Selected != nil {
		t.Fatalf("expected no selected version, got %#v", result.Selected)
	}
}
