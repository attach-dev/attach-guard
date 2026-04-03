// Package socket implements the Socket.dev risk provider adapter.
package socket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

const (
	baseURL                = "https://api.socket.dev/v0"
	defaultTimeout         = 10 * time.Second
	cratesUserAgent        = "attach-guard (https://github.com/attach-dev/attach-guard)"
	goInfoFetchConcurrency = 8
)

// Provider implements the provider.Provider interface for Socket.dev.
type Provider struct {
	apiToken   string
	httpClient *http.Client
}

type httpStatusError struct {
	statusCode int
	body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("registry returned status %d: %s", e.statusCode, e.body)
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
	if ecosystem == api.EcosystemGo && !goModuleUsesPublicSources(name) {
		return nil, provider.ErrUnsupportedSource
	}

	switch ecosystem {
	case api.EcosystemNPM, api.EcosystemPNPM:
		return p.getScoreBySocketEndpoint(ctx, ecosystem, name, version)
	case api.EcosystemPyPI, api.EcosystemGo, api.EcosystemCargo:
		return p.getScoreByPurl(ctx, ecosystem, name, version)
	default:
		return nil, fmt.Errorf("unsupported ecosystem %q", ecosystem)
	}
}

func (p *Provider) getScoreBySocketEndpoint(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	eco := socketEcosystem(ecosystem)
	url := fmt.Sprintf("%s/%s/%s/%s/score", baseURL, eco, name, version)

	body, err := p.doGet(ctx, url)
	if err != nil {
		if ecosystem == api.EcosystemGo {
			var statusErr *httpStatusError
			if errors.As(err, &statusErr) && (statusErr.statusCode == http.StatusNotFound || statusErr.statusCode == http.StatusGone) {
				return nil, provider.ErrUnsupportedSource
			}
		}
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

func (p *Provider) getScoreByPurl(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	// Socket's /v0/purl endpoint is a temporary compatibility shim for
	// non-npm ecosystems. Keep the request/parse logic isolated so it can be
	// swapped out when Socket ships a replacement scoring endpoint.
	purl, err := buildPurl(ecosystem, name, version)
	if err != nil {
		return nil, err
	}

	body, err := p.doPost(ctx, fmt.Sprintf("%s/purl", baseURL), purlRequest{
		Components: []purlComponent{{PURL: purl}},
	})
	if err != nil {
		return nil, wrapPurlRequestError(fmt.Sprintf("fetching purl score for %s@%s", name, version), err)
	}

	infos, err := parsePurlResponse(body, map[string]string{purl: version})
	if err != nil {
		return nil, fmt.Errorf("parsing purl score response for %s@%s: %w", name, version, err)
	}

	info, ok := infos[version]
	if !ok {
		return nil, provider.ErrUnsupportedSource
	}

	meta, err := p.lookupVersionMetadata(ctx, ecosystem, name, version)
	if err != nil {
		return nil, err
	}
	if !meta.PublishedAt.IsZero() {
		info.PublishedAt = meta.PublishedAt
	}
	info.Deprecated = meta.Deprecated

	return info, nil
}

// maxCandidates is the number of recent versions to score via the Socket API.
const maxCandidates = 10

// ListVersions fetches available versions from the ecosystem registry in the
// same precedence order the package manager would consider for an unpinned
// install, then scores the top candidates via the Socket score endpoint.
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

	if ecosystem != api.EcosystemNPM && ecosystem != api.EcosystemPNPM {
		scored, err := p.batchScoreByPurl(ctx, ecosystem, name, ordered[:limit])
		if err != nil {
			// Preserve ordered candidates with zero-score placeholders when
			// purl scoring fails, matching the existing npm fallback behavior.
			scored = map[string]*api.VersionInfo{}
		}

		versions := make([]api.VersionInfo, 0, limit)
		for _, entry := range ordered[:limit] {
			info, ok := scored[entry.Version]
			if !ok {
				info = &api.VersionInfo{Version: entry.Version}
			}
			if info.PublishedAt.IsZero() && !entry.PublishedAt.IsZero() {
				info.PublishedAt = entry.PublishedAt
			}
			info.Deprecated = entry.Deprecated
			versions = append(versions, *info)
		}
		return versions, nil
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

func (p *Provider) batchScoreByPurl(ctx context.Context, ecosystem api.Ecosystem, name string, versions []orderedVersion) (map[string]*api.VersionInfo, error) {
	if len(versions) == 0 {
		return map[string]*api.VersionInfo{}, nil
	}

	components := make([]purlComponent, 0, len(versions))
	requested := make(map[string]string, len(versions))
	for _, version := range versions {
		purl, err := buildPurl(ecosystem, name, version.Version)
		if err != nil {
			return nil, err
		}
		components = append(components, purlComponent{PURL: purl})
		requested[purl] = version.Version
	}

	body, err := p.doPost(ctx, fmt.Sprintf("%s/purl", baseURL), purlRequest{Components: components})
	if err != nil {
		return nil, wrapPurlRequestError(fmt.Sprintf("fetching purl scores for %s", name), err)
	}

	infos, err := parsePurlResponse(body, requested)
	if err != nil {
		return nil, fmt.Errorf("parsing purl batch response for %s: %w", name, err)
	}

	return infos, nil
}

func (p *Provider) lookupVersionMetadata(ctx context.Context, ecosystem api.Ecosystem, name, version string) (orderedVersion, error) {
	switch ecosystem {
	case api.EcosystemPyPI:
		return p.lookupVersionMetadataPyPI(ctx, name, version)
	case api.EcosystemGo:
		return p.lookupVersionMetadataGo(ctx, name, version)
	case api.EcosystemCargo:
		return p.lookupVersionMetadataCargo(ctx, name, version)
	default:
		return orderedVersion{}, fmt.Errorf("unsupported ecosystem %q", ecosystem)
	}
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

	return orderedPyPIReleases(reg.Releases), nil
}

func (p *Provider) lookupVersionMetadataPyPI(ctx context.Context, name, version string) (orderedVersion, error) {
	registryURL := fmt.Sprintf("https://pypi.org/pypi/%s/json", name)
	body, err := p.doGetPublic(ctx, registryURL)
	if err != nil {
		return orderedVersion{}, fmt.Errorf("fetching metadata for %s@%s from pypi: %w", name, version, err)
	}

	var reg pypiRegistryResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return orderedVersion{}, fmt.Errorf("parsing pypi response for %s: %w", name, err)
	}

	files, ok := reg.Releases[version]
	if !ok || len(files) == 0 {
		return orderedVersion{}, provider.ErrUnsupportedSource
	}

	var publishedAt time.Time
	deprecated := true
	for _, file := range files {
		if ts, ok := parsePyPITimestamp(file.UploadTimeISO8601); ok && ts.After(publishedAt) {
			publishedAt = ts
		}
		if !file.Yanked {
			deprecated = false
		}
	}

	return orderedVersion{
		Version:     version,
		PublishedAt: publishedAt,
		Deprecated:  deprecated,
	}, nil
}

func (p *Provider) listOrderedVersionsGo(ctx context.Context, name string) ([]orderedVersion, error) {
	if !goModuleUsesPublicSources(name) {
		return nil, provider.ErrUnsupportedSource
	}

	escaped := escapeModulePath(name)
	registryURL := fmt.Sprintf("https://proxy.golang.org/%s/@v/list", escaped)
	body, err := p.doGetPublic(ctx, registryURL)
	if err != nil {
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && (statusErr.statusCode == http.StatusNotFound || statusErr.statusCode == http.StatusGone) {
			return nil, provider.ErrUnsupportedSource
		}
		return nil, fmt.Errorf("fetching versions for %s from go proxy: %w", name, err)
	}

	candidates := make([]orderedVersion, 0, len(strings.Fields(string(body))))
	for _, version := range strings.Fields(string(body)) {
		candidates = append(candidates, orderedVersion{Version: version})
	}
	// The list response does not include timestamps, so we pre-cap using
	// semver-aware ordering on the raw version strings. PublishedAt is only
	// populated after the per-version .info fetches below.
	candidates = orderGoVersions(candidates)
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	ordered := make([]orderedVersion, len(candidates))
	found := make([]bool, len(candidates))
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, goInfoFetchConcurrency)
	)
	for idx, candidate := range candidates {
		idx := idx
		version := candidate.Version
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			infoURL := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.info", escaped, version)
			infoBody, err := p.doGetPublic(ctx, infoURL)
			if err != nil {
				return
			}
			var info goProxyVersionInfo
			if err := json.Unmarshal(infoBody, &info); err != nil {
				return
			}

			mu.Lock()
			ordered[idx] = orderedVersion{
				Version:     info.Version,
				PublishedAt: info.Time,
			}
			found[idx] = true
			mu.Unlock()
		}()
	}
	wg.Wait()

	filtered := ordered[:0]
	for i, ok := range found {
		if ok {
			filtered = append(filtered, ordered[i])
		}
	}

	return filtered, nil
}

func (p *Provider) lookupVersionMetadataGo(ctx context.Context, name, version string) (orderedVersion, error) {
	if !goModuleUsesPublicSources(name) {
		return orderedVersion{}, provider.ErrUnsupportedSource
	}

	escaped := escapeModulePath(name)
	infoURL := fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.info", escaped, version)
	infoBody, err := p.doGetPublic(ctx, infoURL)
	if err != nil {
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && (statusErr.statusCode == http.StatusNotFound || statusErr.statusCode == http.StatusGone) {
			return orderedVersion{}, provider.ErrUnsupportedSource
		}
		return orderedVersion{}, fmt.Errorf("fetching metadata for %s@%s from go proxy: %w", name, version, err)
	}

	var info goProxyVersionInfo
	if err := json.Unmarshal(infoBody, &info); err != nil {
		return orderedVersion{}, fmt.Errorf("parsing go proxy metadata for %s@%s: %w", name, version, err)
	}

	return orderedVersion{
		Version:     info.Version,
		PublishedAt: info.Time,
	}, nil
}

func (p *Provider) listOrderedVersionsCargo(ctx context.Context, name string) ([]orderedVersion, error) {
	registryURL := fmt.Sprintf("https://crates.io/api/v1/crates/%s", name)
	body, err := p.doGetPublicWithHeaders(ctx, registryURL, map[string]string{
		"User-Agent": cratesUserAgent,
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

	return orderCargoVersions(ordered), nil
}

func (p *Provider) lookupVersionMetadataCargo(ctx context.Context, name, version string) (orderedVersion, error) {
	registryURL := fmt.Sprintf("https://crates.io/api/v1/crates/%s", name)
	body, err := p.doGetPublicWithHeaders(ctx, registryURL, map[string]string{
		"User-Agent": cratesUserAgent,
	})
	if err != nil {
		return orderedVersion{}, fmt.Errorf("fetching metadata for %s@%s from crates.io: %w", name, version, err)
	}

	var reg cargoRegistryResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return orderedVersion{}, fmt.Errorf("parsing crates.io response for %s: %w", name, err)
	}

	for _, candidate := range reg.Versions {
		if candidate.Num != version {
			continue
		}
		return orderedVersion{
			Version:     candidate.Num,
			PublishedAt: candidate.CreatedAt,
			Deprecated:  candidate.Yanked,
		}, nil
	}

	return orderedVersion{}, provider.ErrUnsupportedSource
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
		return nil, &httpStatusError{statusCode: resp.StatusCode, body: string(body)}
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
		return nil, &httpStatusError{statusCode: resp.StatusCode, body: string(body)}
	}

	return body, nil
}

func (p *Provider) doPost(ctx context.Context, url string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson, application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{statusCode: resp.StatusCode, body: string(respBody)}
	}

	return respBody, nil
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

func purlEcosystem(eco api.Ecosystem) string {
	switch eco {
	case api.EcosystemPyPI:
		return "pypi"
	case api.EcosystemGo:
		return "golang"
	case api.EcosystemCargo:
		return "cargo"
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
	DepScore    float64      `json:"depscore"`
	PublishedAt string       `json:"publishedAt"`
	Issues      []issueEntry `json:"issues"`
}

type issueEntry struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Category string `json:"category"`
}

type purlRequest struct {
	Components []purlComponent `json:"components"`
}

type purlComponent struct {
	PURL string `json:"purl"`
}

type purlLineType struct {
	Type string `json:"_type"`
}

type purlArtifact struct {
	Type      string      `json:"type"`
	Name      string      `json:"name"`
	Version   string      `json:"version"`
	Release   string      `json:"release"`
	Score     purlScore   `json:"score"`
	Alerts    []purlAlert `json:"alerts"`
	InputPurl string      `json:"inputPurl"`
}

type purlScore struct {
	SupplyChain   float64 `json:"supplyChain"`
	Overall       float64 `json:"overall"`
	Quality       float64 `json:"quality"`
	Maintenance   float64 `json:"maintenance"`
	Vulnerability float64 `json:"vulnerability"`
	License       float64 `json:"license"`
}

type purlAlert struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
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
	DistTags map[string]string                 `json:"dist-tags"`
	Time     map[string]string                 `json:"time"`
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

type semverVersion struct {
	major      int
	minor      int
	patch      int
	prerelease []semverIdentifier
}

type semverIdentifier struct {
	numeric bool
	number  int
	text    string
}

type pep440Version struct {
	epoch    int
	release  []int
	prePhase int
	preNum   int
	hasPre   bool
	postNum  int
	hasPost  bool
	devNum   int
	hasDev   bool
}

var (
	pep440ReleasePattern = regexp.MustCompile(`^(\d+(?:\.\d+)*)`)
	pep440PrePattern     = regexp.MustCompile(`^(?:[._-]?)(a|b|rc|c|alpha|beta|pre|preview)(\d*)`)
	pep440PostPattern    = regexp.MustCompile(`^(?:[._-]?)(post|rev|r)(\d*)|^-(\d+)`)
	pep440DevPattern     = regexp.MustCompile(`^(?:[._-]?dev)(\d*)`)
	pep503NamePattern    = regexp.MustCompile(`[-_.]+`)
)

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

func orderedPyPIReleases(releases map[string][]pypiFileInfo) []orderedVersion {
	var ordered []orderedVersion
	for version, files := range releases {
		if len(files) == 0 {
			continue
		}

		var publishedAt time.Time
		deprecated := true
		for _, file := range files {
			if ts, ok := parsePyPITimestamp(file.UploadTimeISO8601); ok && ts.After(publishedAt) {
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

	return orderPyPIVersions(ordered)
}

func buildPurl(ecosystem api.Ecosystem, name, version string) (string, error) {
	switch ecosystem {
	case api.EcosystemPyPI, api.EcosystemGo, api.EcosystemCargo:
	default:
		return "", fmt.Errorf("unsupported purl ecosystem %q", ecosystem)
	}

	if ecosystem == api.EcosystemPyPI {
		name = normalizePyPIName(name)
	}

	eco := purlEcosystem(ecosystem)
	return fmt.Sprintf("pkg:%s/%s@%s", eco, name, version), nil
}

func normalizePyPIName(name string) string {
	return pep503NamePattern.ReplaceAllString(strings.ToLower(name), "-")
}

func wrapPurlRequestError(action string, err error) error {
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) && (statusErr.statusCode == http.StatusUnauthorized || statusErr.statusCode == http.StatusForbidden) {
		return fmt.Errorf("%s: Socket /v0/purl requires a token with packages:list scope: %w", action, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func parsePurlResponse(body []byte, requestedByPurl map[string]string) (map[string]*api.VersionInfo, error) {
	aggregated := make(map[string]*api.VersionInfo, len(requestedByPurl))
	seenAlerts := make(map[string]map[string]struct{}, len(requestedByPurl))

	lines, err := purlResponseLines(body)
	if err != nil {
		return nil, err
	}

	for _, line := range lines {
		var meta purlLineType
		if err := json.Unmarshal(line, &meta); err != nil {
			return nil, err
		}
		if meta.Type == "summary" || meta.Type == "purlError" {
			continue
		}

		var artifact purlArtifact
		if err := json.Unmarshal(line, &artifact); err != nil {
			return nil, err
		}

		version, ok := purlArtifactVersion(artifact, requestedByPurl)
		if !ok {
			continue
		}

		info := aggregated[version]
		if info == nil {
			info = &api.VersionInfo{
				Version: version,
				Score: api.PackageScore{
					SupplyChain: artifact.Score.SupplyChain * 100,
					Overall:     artifact.Score.Overall * 100,
				},
			}
			aggregated[version] = info
			seenAlerts[version] = make(map[string]struct{})
		} else {
			info.Score.SupplyChain = minFloat(info.Score.SupplyChain, artifact.Score.SupplyChain*100)
			info.Score.Overall = minFloat(info.Score.Overall, artifact.Score.Overall*100)
		}

		for _, alert := range artifact.Alerts {
			mapped := mapPurlAlert(alert)
			key := strings.Join([]string{mapped.Title, mapped.Severity, mapped.Category}, "|")
			if _, ok := seenAlerts[version][key]; ok {
				continue
			}
			seenAlerts[version][key] = struct{}{}
			info.Alerts = append(info.Alerts, mapped)
		}
	}

	return aggregated, nil
}

func purlResponseLines(body []byte) ([][]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	switch trimmed[0] {
	case '[':
		var entries []json.RawMessage
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, err
		}
		return rawMessagesToLines(entries), nil
	case '{':
		if lines, ok, err := tryParseStructuredPurlJSON(trimmed); ok || err != nil {
			return lines, err
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	var lines [][]byte
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), line...))
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func tryParseStructuredPurlJSON(body []byte) ([][]byte, bool, error) {
	var container struct {
		Results    []json.RawMessage `json:"results"`
		Components []json.RawMessage `json:"components"`
		Items      []json.RawMessage `json:"items"`
		Artifacts  []json.RawMessage `json:"artifacts"`
	}
	if err := json.Unmarshal(body, &container); err != nil {
		if bytes.ContainsRune(body, '\n') {
			return nil, false, nil
		}
		return nil, false, err
	}

	switch {
	case len(container.Results) > 0:
		return rawMessagesToLines(container.Results), true, nil
	case len(container.Components) > 0:
		return rawMessagesToLines(container.Components), true, nil
	case len(container.Items) > 0:
		return rawMessagesToLines(container.Items), true, nil
	case len(container.Artifacts) > 0:
		return rawMessagesToLines(container.Artifacts), true, nil
	default:
		return [][]byte{append([]byte(nil), body...)}, true, nil
	}
}

func rawMessagesToLines(messages []json.RawMessage) [][]byte {
	lines := make([][]byte, 0, len(messages))
	for _, message := range messages {
		line := bytes.TrimSpace(message)
		if len(line) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), line...))
	}
	return lines
}

func purlArtifactVersion(artifact purlArtifact, requestedByPurl map[string]string) (string, bool) {
	if artifact.InputPurl != "" {
		version, ok := requestedByPurl[artifact.InputPurl]
		return version, ok
	}
	if artifact.Version == "" {
		return "", false
	}
	for _, requestedVersion := range requestedByPurl {
		if artifact.Version == requestedVersion {
			return artifact.Version, true
		}
	}
	return "", false
}

func minFloat(left, right float64) float64 {
	if right < left {
		return right
	}
	return left
}

func mapPurlAlert(alert purlAlert) api.PackageAlert {
	category := alert.Category
	if strings.EqualFold(alert.Type, "malware") || strings.EqualFold(alert.Category, "malware") {
		category = "malware"
	}

	return api.PackageAlert{
		Severity: alert.Severity,
		Title:    alert.Type,
		Category: category,
	}
}

func goModuleUsesPublicSources(module string) bool {
	if matchesGoModulePatternList(os.Getenv("GOPRIVATE"), module) {
		return false
	}
	if matchesGoModulePatternList(os.Getenv("GONOPROXY"), module) {
		return false
	}
	return goProxyUsesPublicRegistry(os.Getenv("GOPROXY"))
}

func goProxyUsesPublicRegistry(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}

	// Be conservative: only treat public fallback as supported when the first
	// effective GOPROXY entry is the public proxy. If a private/custom proxy is
	// first, go will consult it before proxy.golang.org, so public scoring would
	// not reliably reflect the fetched artifact.
	for _, entry := range splitGoProxyEntries(raw) {
		switch entry {
		case "", "direct", "off":
			return false
		case "https://proxy.golang.org", "https://proxy.golang.org/", "http://proxy.golang.org", "http://proxy.golang.org/":
			return true
		default:
			return false
		}
	}

	return false
}

func parsePyPITimestamp(raw string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func splitGoProxyEntries(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '|'
	})
}

func matchesGoModulePatternList(raw, module string) bool {
	for _, pattern := range strings.Split(raw, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for _, prefix := range goModulePrefixes(module) {
			matched, err := path.Match(pattern, prefix)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

func goModulePrefixes(module string) []string {
	parts := strings.Split(module, "/")
	prefixes := make([]string, 0, len(parts))
	for i := range parts {
		prefixes = append(prefixes, strings.Join(parts[:i+1], "/"))
	}
	return prefixes
}

func orderPyPIVersions(versions []orderedVersion) []orderedVersion {
	stable := make([]orderedVersion, 0, len(versions))
	prerelease := make([]orderedVersion, 0, len(versions))
	fallback := make([]orderedVersion, 0, len(versions))

	for _, version := range versions {
		parsed, ok := parsePEP440Version(version.Version)
		if !ok {
			fallback = append(fallback, version)
			continue
		}
		if isPEP440Prerelease(parsed) {
			prerelease = append(prerelease, version)
			continue
		}
		stable = append(stable, version)
	}

	switch {
	case len(stable) > 0:
		sort.Slice(stable, func(i, j int) bool {
			left, _ := parsePEP440Version(stable[i].Version)
			right, _ := parsePEP440Version(stable[j].Version)
			return comparePEP440Versions(left, right) > 0
		})
		return stable
	case len(prerelease) > 0:
		sort.Slice(prerelease, func(i, j int) bool {
			left, _ := parsePEP440Version(prerelease[i].Version)
			right, _ := parsePEP440Version(prerelease[j].Version)
			return comparePEP440Versions(left, right) > 0
		})
		return prerelease
	default:
		sortByPublishedAt(fallback)
		return fallback
	}
}

func orderGoVersions(versions []orderedVersion) []orderedVersion {
	releases := make([]orderedVersion, 0, len(versions))
	prereleases := make([]orderedVersion, 0, len(versions))
	pseudos := make([]orderedVersion, 0, len(versions))
	fallback := make([]orderedVersion, 0, len(versions))

	for _, version := range versions {
		parsed, ok := parseSemverVersion(version.Version, true)
		if !ok {
			fallback = append(fallback, version)
			continue
		}
		if isGoPseudoVersion(version.Version) {
			pseudos = append(pseudos, version)
			continue
		}
		if len(parsed.prerelease) > 0 {
			prereleases = append(prereleases, version)
			continue
		}
		releases = append(releases, version)
	}

	switch {
	case len(releases) > 0:
		sort.Slice(releases, func(i, j int) bool {
			left, _ := parseSemverVersion(releases[i].Version, true)
			right, _ := parseSemverVersion(releases[j].Version, true)
			return compareSemverVersions(left, right) > 0
		})
		return releases
	case len(prereleases) > 0:
		sort.Slice(prereleases, func(i, j int) bool {
			left, _ := parseSemverVersion(prereleases[i].Version, true)
			right, _ := parseSemverVersion(prereleases[j].Version, true)
			return compareSemverVersions(left, right) > 0
		})
		return prereleases
	case len(pseudos) > 0:
		sortByPublishedAt(pseudos)
		return pseudos
	default:
		sortByPublishedAt(fallback)
		return fallback
	}
}

func orderCargoVersions(versions []orderedVersion) []orderedVersion {
	stable := make([]orderedVersion, 0, len(versions))
	prerelease := make([]orderedVersion, 0, len(versions))
	fallback := make([]orderedVersion, 0, len(versions))

	for _, version := range versions {
		parsed, ok := parseSemverVersion(version.Version, false)
		if !ok {
			fallback = append(fallback, version)
			continue
		}
		if len(parsed.prerelease) > 0 {
			prerelease = append(prerelease, version)
			continue
		}
		stable = append(stable, version)
	}

	switch {
	case len(stable) > 0:
		sort.Slice(stable, func(i, j int) bool {
			left, _ := parseSemverVersion(stable[i].Version, false)
			right, _ := parseSemverVersion(stable[j].Version, false)
			return compareSemverVersions(left, right) > 0
		})
		return stable
	case len(prerelease) > 0:
		sort.Slice(prerelease, func(i, j int) bool {
			left, _ := parseSemverVersion(prerelease[i].Version, false)
			right, _ := parseSemverVersion(prerelease[j].Version, false)
			return compareSemverVersions(left, right) > 0
		})
		return prerelease
	default:
		sortByPublishedAt(fallback)
		return fallback
	}
}

func sortByPublishedAt(versions []orderedVersion) {
	sort.Slice(versions, func(i, j int) bool {
		if !versions[i].PublishedAt.Equal(versions[j].PublishedAt) {
			return versions[i].PublishedAt.After(versions[j].PublishedAt)
		}
		return versions[i].Version > versions[j].Version
	})
}

func parseSemverVersion(version string, requireV bool) (semverVersion, bool) {
	trimmed := version
	if requireV {
		if !strings.HasPrefix(trimmed, "v") {
			return semverVersion{}, false
		}
		trimmed = trimmed[1:]
	} else if strings.HasPrefix(trimmed, "v") {
		return semverVersion{}, false
	}

	if buildIdx := strings.IndexByte(trimmed, '+'); buildIdx >= 0 {
		trimmed = trimmed[:buildIdx]
	}

	core := trimmed
	pre := ""
	if dashIdx := strings.IndexByte(trimmed, '-'); dashIdx >= 0 {
		core = trimmed[:dashIdx]
		pre = trimmed[dashIdx+1:]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semverVersion{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semverVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semverVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semverVersion{}, false
	}

	parsed := semverVersion{major: major, minor: minor, patch: patch}
	if pre == "" {
		return parsed, true
	}

	idents := strings.Split(pre, ".")
	parsed.prerelease = make([]semverIdentifier, 0, len(idents))
	for _, ident := range idents {
		if ident == "" {
			return semverVersion{}, false
		}
		if allDigits(ident) {
			number, err := strconv.Atoi(ident)
			if err != nil {
				return semverVersion{}, false
			}
			parsed.prerelease = append(parsed.prerelease, semverIdentifier{
				numeric: true,
				number:  number,
				text:    ident,
			})
			continue
		}
		parsed.prerelease = append(parsed.prerelease, semverIdentifier{text: ident})
	}

	return parsed, true
}

func compareSemverVersions(left, right semverVersion) int {
	if left.major != right.major {
		return compareInts(left.major, right.major)
	}
	if left.minor != right.minor {
		return compareInts(left.minor, right.minor)
	}
	if left.patch != right.patch {
		return compareInts(left.patch, right.patch)
	}
	if len(left.prerelease) == 0 && len(right.prerelease) == 0 {
		return 0
	}
	if len(left.prerelease) == 0 {
		return 1
	}
	if len(right.prerelease) == 0 {
		return -1
	}

	limit := len(left.prerelease)
	if len(right.prerelease) < limit {
		limit = len(right.prerelease)
	}
	for i := 0; i < limit; i++ {
		if cmp := compareSemverIdentifier(left.prerelease[i], right.prerelease[i]); cmp != 0 {
			return cmp
		}
	}
	return compareInts(len(left.prerelease), len(right.prerelease))
}

func compareSemverIdentifier(left, right semverIdentifier) int {
	switch {
	case left.numeric && right.numeric:
		return compareInts(left.number, right.number)
	case left.numeric:
		return -1
	case right.numeric:
		return 1
	default:
		return strings.Compare(left.text, right.text)
	}
}

func isGoPseudoVersion(version string) bool {
	trimmed := strings.TrimPrefix(version, "v")
	if buildIdx := strings.IndexByte(trimmed, '+'); buildIdx >= 0 {
		trimmed = trimmed[:buildIdx]
	}
	parts := strings.Split(trimmed, "-")
	if len(parts) < 3 {
		return false
	}

	timestampPart := parts[len(parts)-2]
	if dot := strings.LastIndexByte(timestampPart, '.'); dot >= 0 {
		timestampPart = timestampPart[dot+1:]
	}
	hashPart := parts[len(parts)-1]
	return len(timestampPart) == 14 && allDigits(timestampPart) && isLowerHex(hashPart)
}

func parsePEP440Version(version string) (pep440Version, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(version))
	if localIdx := strings.IndexByte(trimmed, '+'); localIdx >= 0 {
		trimmed = trimmed[:localIdx]
	}
	trimmed = strings.TrimPrefix(trimmed, "v")

	parsed := pep440Version{}
	if bangIdx := strings.IndexByte(trimmed, '!'); bangIdx >= 0 {
		epoch, err := strconv.Atoi(trimmed[:bangIdx])
		if err != nil {
			return pep440Version{}, false
		}
		parsed.epoch = epoch
		trimmed = trimmed[bangIdx+1:]
	}

	releaseMatch := pep440ReleasePattern.FindStringSubmatch(trimmed)
	if releaseMatch == nil {
		return pep440Version{}, false
	}
	for _, part := range strings.Split(releaseMatch[1], ".") {
		value, err := strconv.Atoi(part)
		if err != nil {
			return pep440Version{}, false
		}
		parsed.release = append(parsed.release, value)
	}
	trimmed = trimmed[len(releaseMatch[0]):]

	if preMatch := pep440PrePattern.FindStringSubmatch(trimmed); preMatch != nil {
		parsed.hasPre = true
		parsed.prePhase = pep440PrePhase(preMatch[1])
		parsed.preNum = parseOptionalInt(preMatch[2])
		trimmed = trimmed[len(preMatch[0]):]
	}

	if postMatch := pep440PostPattern.FindStringSubmatch(trimmed); postMatch != nil {
		parsed.hasPost = true
		if postMatch[3] != "" {
			parsed.postNum = parseOptionalInt(postMatch[3])
		} else {
			parsed.postNum = parseOptionalInt(postMatch[2])
		}
		trimmed = trimmed[len(postMatch[0]):]
	}

	if devMatch := pep440DevPattern.FindStringSubmatch(trimmed); devMatch != nil {
		parsed.hasDev = true
		parsed.devNum = parseOptionalInt(devMatch[1])
		trimmed = trimmed[len(devMatch[0]):]
	}

	return parsed, trimmed == ""
}

func comparePEP440Versions(left, right pep440Version) int {
	if left.epoch != right.epoch {
		return compareInts(left.epoch, right.epoch)
	}
	if cmp := compareIntSlices(left.release, right.release); cmp != 0 {
		return cmp
	}

	leftRank := pep440Rank(left)
	rightRank := pep440Rank(right)
	if leftRank != rightRank {
		return compareInts(leftRank, rightRank)
	}

	switch leftRank {
	case 0:
		return compareInts(left.devNum, right.devNum)
	case 1, 2, 3:
		if left.preNum != right.preNum {
			return compareInts(left.preNum, right.preNum)
		}
		return comparePEP440PreSuffix(left, right)
	case 5:
		if left.postNum != right.postNum {
			return compareInts(left.postNum, right.postNum)
		}
		return comparePEP440PostSuffix(left, right)
	default:
		return 0
	}
}

func comparePEP440PreSuffix(left, right pep440Version) int {
	leftRank := pep440PreSuffixRank(left)
	rightRank := pep440PreSuffixRank(right)
	if leftRank != rightRank {
		return compareInts(leftRank, rightRank)
	}
	switch leftRank {
	case 0:
		return compareInts(left.devNum, right.devNum)
	case 2:
		if left.postNum != right.postNum {
			return compareInts(left.postNum, right.postNum)
		}
		return compareInts(left.devNum, right.devNum)
	case 3:
		return compareInts(left.postNum, right.postNum)
	default:
		return 0
	}
}

func comparePEP440PostSuffix(left, right pep440Version) int {
	leftRank := pep440PostSuffixRank(left)
	rightRank := pep440PostSuffixRank(right)
	if leftRank != rightRank {
		return compareInts(leftRank, rightRank)
	}
	if leftRank == 0 {
		return compareInts(left.devNum, right.devNum)
	}
	return 0
}

func pep440Rank(version pep440Version) int {
	switch {
	case !version.hasPre && !version.hasPost && version.hasDev:
		return 0
	case version.hasPre:
		return version.prePhase
	case version.hasPost:
		return 5
	default:
		return 4
	}
}

func pep440PrePhase(raw string) int {
	switch raw {
	case "a", "alpha":
		return 1
	case "b", "beta":
		return 2
	default:
		return 3
	}
}

func pep440PreSuffixRank(version pep440Version) int {
	switch {
	case !version.hasPost && version.hasDev:
		return 0
	case !version.hasPost:
		return 1
	case version.hasDev:
		return 2
	default:
		return 3
	}
}

func pep440PostSuffixRank(version pep440Version) int {
	if version.hasDev {
		return 0
	}
	return 1
}

func isPEP440Prerelease(version pep440Version) bool {
	return version.hasPre || version.hasDev
}

func compareIntSlices(left, right []int) int {
	limit := len(left)
	if len(right) > limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		leftValue := 0
		if i < len(left) {
			leftValue = left[i]
		}
		rightValue := 0
		if i < len(right) {
			rightValue = right[i]
		}
		if leftValue != rightValue {
			return compareInts(leftValue, rightValue)
		}
	}
	return 0
}

func compareInts(left, right int) int {
	switch {
	case left > right:
		return 1
	case left < right:
		return -1
	default:
		return 0
	}
}

func parseOptionalInt(raw string) int {
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func allDigits(raw string) bool {
	if raw == "" {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isLowerHex(raw string) bool {
	if raw == "" {
		return false
	}
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
