// Package socket implements the Socket.dev risk provider adapter.
package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/attach-dev/attach-guard/pkg/api"
)

const (
	baseURL       = "https://api.socket.dev/v0"
	defaultTimeout = 10 * time.Second
)

// Provider implements the provider.Provider interface for Socket.dev.
type Provider struct {
	apiToken   string
	httpClient *http.Client
}

// New creates a new Socket provider using the given env var for the API token.
func New(tokenEnvVar string) (*Provider, error) {
	token := os.Getenv(tokenEnvVar)
	if token == "" {
		return nil, fmt.Errorf("Socket API token not found in environment variable %s", tokenEnvVar)
	}

	return &Provider{
		apiToken: token,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "socket"
}

// IsAvailable checks if the Socket API is reachable using the zero-cost quota endpoint.
func (p *Provider) IsAvailable(ctx context.Context) bool {
	url := fmt.Sprintf("%s/quota", baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// GetPackageScore fetches score data for a specific package version from Socket.
func (p *Provider) GetPackageScore(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	eco := socketEcosystem(ecosystem)
	url := fmt.Sprintf("%s/%s/%s/%s/score", baseURL, eco, name, version)

	body, err := p.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching score for %s@%s: %w", name, version, err)
	}

	var resp scoreResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing score response for %s@%s: %w", name, version, err)
	}

	info := &api.VersionInfo{
		Version: version,
		Score: api.PackageScore{
			SupplyChain: resp.SupplyChainRisk.Score * 100,
			Overall:     resp.DepScore * 100,
		},
	}

	if resp.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, resp.PublishedAt); err == nil {
			info.PublishedAt = t
		}
	}

	for _, issue := range resp.Issues {
		info.Alerts = append(info.Alerts, api.PackageAlert{
			Severity: issue.Severity,
			Title:    issue.Title,
			Category: issue.Category,
		})
	}

	return info, nil
}

// maxCandidates is the number of recent versions to score via the Socket API.
const maxCandidates = 10

// ListVersions fetches available versions from the npm registry (newest first)
// and scores the top candidates via the Socket score endpoint.
func (p *Provider) ListVersions(ctx context.Context, ecosystem api.Ecosystem, name string) ([]api.VersionInfo, error) {
	// Fetch version list from the npm registry
	registryURL := fmt.Sprintf("https://registry.npmjs.org/%s", name)
	body, err := p.doGetPublic(ctx, registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s from npm registry: %w", name, err)
	}

	var reg npmRegistryResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("parsing npm registry response for %s: %w", name, err)
	}

	// Build ordered version list from dist-tags.latest backwards through time
	ordered := reg.orderedVersions()
	if len(ordered) == 0 {
		return nil, nil
	}

	// Score the top N candidates via Socket
	limit := maxCandidates
	if len(ordered) < limit {
		limit = len(ordered)
	}

	eco := socketEcosystem(ecosystem)
	var versions []api.VersionInfo
	for _, entry := range ordered[:limit] {
		info := api.VersionInfo{
			Version:    entry.Version,
			Deprecated: entry.Deprecated,
		}
		if !entry.PublishedAt.IsZero() {
			info.PublishedAt = entry.PublishedAt
		}

		// Fetch score from Socket — skip on error (version still included with zero scores)
		scoreURL := fmt.Sprintf("%s/%s/%s/%s/score", baseURL, eco, name, entry.Version)
		scoreBody, err := p.doGet(ctx, scoreURL)
		if err == nil {
			var sr scoreResponse
			if json.Unmarshal(scoreBody, &sr) == nil {
				info.Score = api.PackageScore{
					SupplyChain: sr.SupplyChainRisk.Score * 100,
					Overall:     sr.DepScore * 100,
				}
			}
		}

		versions = append(versions, info)
	}

	return versions, nil
}

// doGetPublic makes a GET request without auth (for public registries).
func (p *Provider) doGetPublic(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (p *Provider) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func socketEcosystem(eco api.Ecosystem) string {
	switch eco {
	case api.EcosystemNPM, api.EcosystemPNPM:
		return "npm"
	default:
		return string(eco)
	}
}

// Socket API response types — internal only
type scoreResponse struct {
	SupplyChainRisk struct {
		Score float64 `json:"score"`
	} `json:"supplyChainRisk"`
	DepScore    float64       `json:"depscore"`
	PublishedAt string        `json:"publishedAt"`
	Issues      []issueEntry  `json:"issues"`
}

type issueEntry struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Category string `json:"category"`
}

// npm registry response types
type npmRegistryResponse struct {
	DistTags map[string]string                `json:"dist-tags"`
	Time     map[string]string                `json:"time"`
	Versions map[string]npmRegistryVersionInfo `json:"versions"`
}

type npmRegistryVersionInfo struct {
	Version    string `json:"version"`
	Deprecated any    `json:"deprecated"` // string or bool
}

type orderedVersion struct {
	Version     string
	PublishedAt time.Time
	Deprecated  bool
}

// orderedVersions returns versions sorted newest-first using publish times.
func (r *npmRegistryResponse) orderedVersions() []orderedVersion {
	type entry struct {
		version     string
		publishedAt time.Time
		deprecated  bool
	}

	var entries []entry
	for ver, info := range r.Versions {
		dep := false
		if info.Deprecated != nil {
			switch v := info.Deprecated.(type) {
			case bool:
				dep = v
			case string:
				dep = v != ""
			}
		}
		var t time.Time
		if ts, ok := r.Time[ver]; ok {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				t = parsed
			}
		}
		entries = append(entries, entry{version: ver, publishedAt: t, deprecated: dep})
	}

	// Sort newest first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].publishedAt.After(entries[j].publishedAt)
	})

	result := make([]orderedVersion, len(entries))
	for i, e := range entries {
		result[i] = orderedVersion{Version: e.version, PublishedAt: e.publishedAt, Deprecated: e.deprecated}
	}
	return result
}
