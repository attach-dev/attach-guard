package provider

import (
	"context"
	"fmt"

	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
)

// MockProvider is a test provider with configurable responses.
type MockProvider struct {
	ProviderName string
	Available    bool
	Scores       map[string]*api.VersionInfo        // key: "name@version"
	Versions     map[string][]api.VersionInfo        // key: "name"
	ScoreErr     error
	VersionsErr  error
}

// NewMockProvider creates a mock provider with default settings.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		ProviderName: "mock",
		Available:    true,
		Scores:       make(map[string]*api.VersionInfo),
		Versions:     make(map[string][]api.VersionInfo),
	}
}

func (m *MockProvider) Name() string { return m.ProviderName }

func (m *MockProvider) IsAvailable(_ context.Context) bool { return m.Available }

func (m *MockProvider) GetPackageScore(_ context.Context, _ api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	if m.ScoreErr != nil {
		return nil, m.ScoreErr
	}
	key := name + "@" + version
	if info, ok := m.Scores[key]; ok {
		return info, nil
	}
	return nil, fmt.Errorf("no mock data for %s", key)
}

func (m *MockProvider) ListVersions(_ context.Context, _ api.Ecosystem, name string) ([]api.VersionInfo, error) {
	if m.VersionsErr != nil {
		return nil, m.VersionsErr
	}
	if versions, ok := m.Versions[name]; ok {
		return versions, nil
	}
	return nil, fmt.Errorf("no mock versions for %s", name)
}

// AddScore adds a mock score entry.
func (m *MockProvider) AddScore(name, version string, supplyChain, overall float64) {
	m.Scores[name+"@"+version] = &api.VersionInfo{
		Version: version,
		Score: api.PackageScore{
			SupplyChain: supplyChain,
			Overall:     overall,
		},
	}
}

// AddVersion adds a version to the version list for a package.
func (m *MockProvider) AddVersion(name string, info api.VersionInfo) {
	m.Versions[name] = append(m.Versions[name], info)
}
