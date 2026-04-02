// Package provider defines the provider interface for package risk intelligence.
package provider

import (
	"context"
	"errors"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// ErrUnsupportedSource indicates the package source cannot be evaluated using
// the provider's public-registry lookup path.
var ErrUnsupportedSource = errors.New("unsupported package source")

// Provider is the interface that risk intelligence providers must implement.
type Provider interface {
	// Name returns the provider name.
	Name() string

	// GetPackageScore returns score data for a specific package version.
	GetPackageScore(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error)

	// ListVersions returns available versions for a package, newest first.
	ListVersions(ctx context.Context, ecosystem api.Ecosystem, name string) ([]api.VersionInfo, error)

	// IsAvailable checks whether the provider is reachable.
	IsAvailable(ctx context.Context) bool
}
