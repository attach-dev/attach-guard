// Package openscore implements the Attach Open Score HTTP provider.
package openscore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

const (
	// DefaultTimeoutSeconds is the bounded request timeout used when config
	// omits provider.timeout_seconds.
	DefaultTimeoutSeconds = 5

	maxResponseBodyBytes = 64 * 1024
	providerReason       = "provider-unavailable"
)

// Provider implements provider.Provider for Attach Open Score-compatible HTTP
// verdict endpoints.
type Provider struct {
	endpoint   string
	httpClient *http.Client
}

type verdictRequest struct {
	Ecosystem api.Ecosystem `json:"ecosystem"`
	Name      string        `json:"name"`
	Version   string        `json:"version"`
}

type verdictResponse struct {
	Decision   string   `json:"decision"`
	Score      *int     `json:"score,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
	SourceRefs []string `json:"source_refs,omitempty"`
}

// New creates an Attach Open Score HTTP provider. A timeoutSeconds value of 0
// uses DefaultTimeoutSeconds; positive values are used as-is.
func New(endpoint string, timeoutSeconds int) (*Provider, error) {
	endpoint, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if timeoutSeconds < 0 {
		return nil, fmt.Errorf("timeout_seconds must be positive")
	}
	if timeoutSeconds == 0 {
		timeoutSeconds = DefaultTimeoutSeconds
	}

	return &Provider{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "open-score"
}

// IsAvailable returns true for configured providers. Open Score v0 has no
// separate health contract, so per-package requests carry provider-unavailable
// failures as UNKNOWN verdicts.
func (p *Provider) IsAvailable(_ context.Context) bool {
	return true
}

// GetPackageScore fetches an Attach Open Score verdict for a package version.
func (p *Provider) GetPackageScore(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	payload := verdictRequest{
		Ecosystem: ecosystem,
		Name:      name,
		Version:   version,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return unavailableVersion(version), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, &body)
	if err != nil {
		return unavailableVersion(version), nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "attach-guard open-score")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return unavailableVersion(version), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unavailableVersion(version), nil
	}

	responseBody, err := readResponseBody(resp.Body)
	if err != nil {
		return unavailableVersion(version), nil
	}

	var response verdictResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return unavailableVersion(version), nil
	}

	decision := api.ProviderVerdictDecision(strings.TrimSpace(response.Decision))
	switch decision {
	case api.ProviderVerdictAllow, api.ProviderVerdictAsk, api.ProviderVerdictDeny, api.ProviderVerdictUnknown:
		return &api.VersionInfo{
			Version: version,
			ProviderVerdict: &api.ProviderVerdict{
				Decision:   decision,
				RiskScore:  response.Score,
				Reasons:    response.Reasons,
				SourceRefs: response.SourceRefs,
			},
		}, nil
	default:
		return unavailableVersion(version), nil
	}
}

// ListVersions is intentionally not implemented for the v0 HTTP verdict
// provider. It scores explicit package coordinates and does not resolve latest
// versions.
func (p *Provider) ListVersions(_ context.Context, _ api.Ecosystem, _ string) ([]api.VersionInfo, error) {
	return nil, provider.ErrUnsupportedSource
}

func validateEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("endpoint is required")
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("endpoint must be a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("endpoint must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("endpoint must include a host")
	}
	return u.String(), nil
}

func readResponseBody(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, maxResponseBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBodyBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxResponseBodyBytes)
	}
	return body, nil
}

func unavailableVersion(version string) *api.VersionInfo {
	return &api.VersionInfo{
		Version: version,
		ProviderVerdict: &api.ProviderVerdict{
			Decision: api.ProviderVerdictUnknown,
			Reasons:  []string{providerReason},
		},
	}
}
