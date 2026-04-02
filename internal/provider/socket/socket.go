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
	"strings"
	"time"
	"unicode"

	"github.com/attach-dev/attach-guard/pkg/api"
)

const (
	baseURL        = "https://api.socket.dev/v0"
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

// ListVersions fetches available versions from the ecosystem registry (newest
// first) and scores the top candidates via the Socket score endpoint.
func (p *Provider) ListVersions(ctx context.Context, ecosystem api.Ecosystem, name string) ([]api.VersionInfo, error) {
	ordered, err := p.listOrderedVersions(ctx, ecosystem, name)
	if err != nil {
		return nil, err
	}
	if len(ordered) == 0 {
		return nil, nil
	}

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

func (p *Provider) listOrderedVersions(ctx context.Context, ecosystem api.Ecosystem, name string) ([]orderedVersion, error) {
	switch ecosystem {
	case api.EcosystemNPM, api.EcosystemPNPM:
		return p.listOrderedVersionsNPM(ctx, name)
	case api.EcosystemPyPI:
		return p.listOrderedVersionsPyPI(ctx, name)
	case api.EcosystemGo:
		return p.listOrderedVersionsGo(ctx, name)
	case api.EcosystemCargo:
		return p.listOrderedVersionsCargo(ctx, name)
	default:
		return nil, fmt.Errorf("unsupported ecosystem %q", ecosystem)
	}
}

func (p *Provider) listOrderedVersionsNPM(ctx context.Context, name string) ([]orderedVersion, error) {
	registryURL := fmt.Sprintf("https://registry.npmjs.org/%s", name)
	body, err := p.doGetPublic(ctx, registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s from npm registry: %w", name, err)
	}

	var reg npmRegistryResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("parsing npm registry response for %s: %w", name, err)
	}
	return reg.orderedVersions(), nil
}

func (p *Provider) listOrderedVersionsPyPI(ctx context.Context, name string) ([]orderedVersion, error) {
	registryURL := fmt.Sprintf("https://pypi.org/pypi/%s/json", name)
	body, err := p.doGetPublic(ctx, registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s from pypi: %w", name, err)
	}

	var reg pypiRegistryResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("parsing pypi response for %s: %w", name, err)
	}

	var ordered []orderedVersion
	for version, files := range reg.Releases {
		var publishedAt time.Time
		deprecated := len(files) > 0
		for _, file := range files {
			if ts, err := time.Parse(time.RFC3339, file.UploadTimeISO8601); err == nil && ts.After(publishedAt) {
				publishedAt = ts
			}
			if !file.Yanked {
				deprecated = false
			}
		}
		ordered = append(ordered, orderedVersion{
			Version:     version,
			PublishedAt: publishedAt,
			Deprecated:  deprecated,
		})
	}

	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].PublishedAt.After(ordered[j].PublishedAt)
	})
	return ordered, nil
}

func (p *Provider) listOrderedVersionsGo(ctx context.Context, name string) ([]orderedVersion, error) {
	escaped := escapeModulePath(name)
	registryURL := fmt.Sprintf("https://proxy.golang.org/%s/@v/list", escaped)
	body, err := p.doGetPublic(ctx, registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s from go proxy: %w", name, err)
	}

	var ordered []orderedVersion
	for _, version := range strings.Fields(string(body)) {
		infoURL := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.info", escaped, version)
		infoBody, err := p.doGetPublic(ctx, infoURL)
		if err != nil {
			continue
		}
		var info goProxyVersionInfo
		if err := json.Unmarshal(infoBody, &info); err != nil {
			continue
		}
		ordered = append(ordered, orderedVersion{
			Version:     info.Version,
			PublishedAt: info.Time,
		})
	}

	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].PublishedAt.After(ordered[j].PublishedAt)
	})
	return ordered, nil
}

func (p *Provider) listOrderedVersionsCargo(ctx context.Context, name string) ([]orderedVersion, error) {
	registryURL := fmt.Sprintf("https://crates.io/api/v1/crates/%s", name)
	body, err := p.doGetPublicWithHeaders(ctx, registryURL, map[string]string{
		"User-Agent": "attach-guard",
	})
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s from crates.io: %w", name, err)
	}

	var reg cargoRegistryResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, fmt.Errorf("parsing crates.io response for %s: %w", name, err)
	}

	ordered := make([]orderedVersion, 0, len(reg.Versions))
	for _, version := range reg.Versions {
		ordered = append(ordered, orderedVersion{
			Version:     version.Num,
			PublishedAt: version.CreatedAt,
			Deprecated:  version.Yanked,
		})
	}

	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].PublishedAt.After(ordered[j].PublishedAt)
	})
	return ordered, nil
}

// doGetPublic makes a GET request without auth (for public registries).
func (p *Provider) doGetPublic(ctx context.Context, url string) ([]byte, error) {
	return p.doGetPublicWithHeaders(ctx, url, nil)
}

func (p *Provider) doGetPublicWithHeaders(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

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
	case api.EcosystemPyPI:
		return "pypi"
	case api.EcosystemGo:
		return "go"
	case api.EcosystemCargo:
		return "crates"
	default:
		return string(eco)
	}
}

func escapeModulePath(module string) string {
	var b strings.Builder
	for _, r := range module {
		if unicode.IsUpper(r) {
			b.WriteRune('!')
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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

type pypiRegistryResponse struct {
	Releases map[string][]pypiFileInfo `json:"releases"`
}

type pypiFileInfo struct {
	UploadTimeISO8601 string `json:"upload_time_iso_8601"`
	Yanked            bool   `json:"yanked"`
}

type goProxyVersionInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

type cargoRegistryResponse struct {
	Versions []cargoVersionInfo `json:"versions"`
}

type cargoVersionInfo struct {
	Num       string    `json:"num"`
	CreatedAt time.Time `json:"created_at"`
	Yanked    bool      `json:"yanked"`
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
