// Package socket implements the Socket.dev risk provider adapter.
package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// IsAvailable checks if the Socket API is reachable.
func (p *Provider) IsAvailable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
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
	url := fmt.Sprintf("%s/npm/%s/score?version=%s", baseURL, name, version)
	if eco != "npm" {
		url = fmt.Sprintf("%s/%s/%s/score?version=%s", baseURL, eco, name, version)
	}

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
			Overall:     resp.Overall.Score * 100,
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

// ListVersions fetches available versions for a package from Socket.
func (p *Provider) ListVersions(ctx context.Context, ecosystem api.Ecosystem, name string) ([]api.VersionInfo, error) {
	eco := socketEcosystem(ecosystem)
	url := fmt.Sprintf("%s/%s/%s/versions", baseURL, eco, name)

	body, err := p.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s: %w", name, err)
	}

	var resp versionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing versions response for %s: %w", name, err)
	}

	var versions []api.VersionInfo
	for _, v := range resp.Versions {
		info := api.VersionInfo{
			Version:    v.Version,
			Deprecated: v.Deprecated,
			Score: api.PackageScore{
				SupplyChain: v.Score.SupplyChainRisk * 100,
				Overall:     v.Score.Overall * 100,
			},
		}
		if v.PublishedAt != "" {
			if t, err := time.Parse(time.RFC3339, v.PublishedAt); err == nil {
				info.PublishedAt = t
			}
		}
		versions = append(versions, info)
	}

	return versions, nil
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
	Overall struct {
		Score float64 `json:"score"`
	} `json:"overall"`
	PublishedAt string        `json:"publishedAt"`
	Issues      []issueEntry  `json:"issues"`
}

type issueEntry struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Category string `json:"category"`
}

type versionsResponse struct {
	Versions []versionEntry `json:"versions"`
}

type versionEntry struct {
	Version     string `json:"version"`
	PublishedAt string `json:"publishedAt"`
	Deprecated  bool   `json:"deprecated"`
	Score       struct {
		SupplyChainRisk float64 `json:"supplyChainRisk"`
		Overall         float64 `json:"overall"`
	} `json:"score"`
}
