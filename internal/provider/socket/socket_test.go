package socket

import (
	"testing"

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
