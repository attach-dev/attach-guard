package socket

import (
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestSocketEcosystem(t *testing.T) {
	tests := []struct {
		eco  api.Ecosystem
		want string
	}{
		{api.EcosystemNPM, "npm"},
		{api.EcosystemPNPM, "npm"},
		{api.EcosystemPyPI, "pypi"},
		{api.EcosystemGo, "go"},
		{api.EcosystemCargo, "crates"},
	}

	for _, tt := range tests {
		if got := socketEcosystem(tt.eco); got != tt.want {
			t.Errorf("socketEcosystem(%q) = %q, want %q", tt.eco, got, tt.want)
		}
	}
}

func TestEscapeModulePath(t *testing.T) {
	if got, want := escapeModulePath("github.com/Azure/azure-sdk-for-go"), "github.com/!azure/azure-sdk-for-go"; got != want {
		t.Fatalf("escapeModulePath() = %q, want %q", got, want)
	}
}

func TestOrderPyPIVersionsPrefersStableVersionPrecedence(t *testing.T) {
	now := time.Now()
	ordered := orderPyPIVersions([]orderedVersion{
		{Version: "1.5.9", PublishedAt: now},
		{Version: "2.0.0", PublishedAt: now.Add(-2 * time.Hour)},
		{Version: "2.1.0rc1", PublishedAt: now.Add(time.Hour)},
	})

	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2 stable releases", len(ordered))
	}
	if ordered[0].Version != "2.0.0" || ordered[1].Version != "1.5.9" {
		t.Fatalf("ordered versions = %#v, want [2.0.0 1.5.9]", ordered)
	}
}

func TestOrderGoVersionsPrefersTaggedReleases(t *testing.T) {
	now := time.Now()
	ordered := orderGoVersions([]orderedVersion{
		{Version: "v1.2.0-rc.1", PublishedAt: now.Add(time.Hour)},
		{Version: "v1.1.9", PublishedAt: now},
		{Version: "v1.2.0", PublishedAt: now.Add(-2 * time.Hour)},
		{Version: "v1.3.0-0.20240401010203-deadbeefcafe", PublishedAt: now.Add(2 * time.Hour)},
	})

	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2 release versions", len(ordered))
	}
	if ordered[0].Version != "v1.2.0" || ordered[1].Version != "v1.1.9" {
		t.Fatalf("ordered versions = %#v, want [v1.2.0 v1.1.9]", ordered)
	}
}

func TestOrderCargoVersionsPrefersStableSemver(t *testing.T) {
	now := time.Now()
	ordered := orderCargoVersions([]orderedVersion{
		{Version: "0.9.9", PublishedAt: now},
		{Version: "1.0.0", PublishedAt: now.Add(-2 * time.Hour)},
		{Version: "1.1.0-rc.1", PublishedAt: now.Add(time.Hour)},
	})

	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2 stable versions", len(ordered))
	}
	if ordered[0].Version != "1.0.0" || ordered[1].Version != "0.9.9" {
		t.Fatalf("ordered versions = %#v, want [1.0.0 0.9.9]", ordered)
	}
}
