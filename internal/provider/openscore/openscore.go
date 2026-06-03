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

// Option customizes an Open Score provider.
type Option func(*Provider)

// WithHTTPClient overrides the default HTTP client. It is primarily useful for
// local tests that need to keep HTTP traffic in-process.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

type verdictRequest struct {
	Ecosystem api.Ecosystem `json:"ecosystem"`
	Name      string        `json:"name"`
	Version   string        `json:"version"`
}

type verdictResponse struct {
	Decision   string        `json:"decision"`
	Score      *int          `json:"score,omitempty"`
	Confidence string        `json:"confidence,omitempty"`
	Reasons    reasonList    `json:"reasons,omitempty"`
	SourceRefs sourceRefList `json:"source_refs,omitempty"`
}

type reasonList []string

func (l *reasonList) UnmarshalJSON(data []byte) error {
	values, err := decodeStringOrObjectList(data, "code")
	if err != nil {
		return err
	}
	*l = values
	return nil
}

type sourceRefList []string

func (l *sourceRefList) UnmarshalJSON(data []byte) error {
	values, err := decodeStringOrObjectList(data, "id", "url")
	if err != nil {
		return err
	}
	*l = values
	return nil
}

// New creates an Attach Open Score HTTP provider. A timeoutSeconds value of 0
// uses DefaultTimeoutSeconds; positive values are used as-is.
func New(endpoint string, timeoutSeconds int, opts ...Option) (*Provider, error) {
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

	prov := &Provider{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(prov)
		}
	}
	return prov, nil
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
		Ecosystem: openScoreRequestEcosystem(ecosystem),
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

	return versionInfoFromResponse(version, response), nil
}

func versionInfoFromResponse(version string, response verdictResponse) *api.VersionInfo {
	decision := api.ProviderVerdictDecision(strings.TrimSpace(response.Decision))
	switch decision {
	case api.ProviderVerdictAllow, api.ProviderVerdictAsk, api.ProviderVerdictDeny, api.ProviderVerdictUnknown:
		return &api.VersionInfo{
			Version: version,
			ProviderVerdict: &api.ProviderVerdict{
				Decision:   decision,
				RiskScore:  response.Score,
				Confidence: strings.TrimSpace(response.Confidence),
				Reasons:    []string(response.Reasons),
				SourceRefs: []string(response.SourceRefs),
			},
		}
	default:
		return unavailableVersion(version)
	}
}

// ListVersions is intentionally not implemented for the v0 HTTP verdict
// provider. It scores explicit package coordinates and does not resolve latest
// versions.
func (p *Provider) ListVersions(_ context.Context, _ api.Ecosystem, _ string) ([]api.VersionInfo, error) {
	return nil, provider.ErrUnsupportedSource
}

func openScoreRequestEcosystem(ecosystem api.Ecosystem) api.Ecosystem {
	switch ecosystem {
	case api.EcosystemPNPM:
		return api.EcosystemNPM
	default:
		return ecosystem
	}
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

func decodeStringOrObjectList(data []byte, objectKeys ...string) ([]string, error) {
	if strings.TrimSpace(string(data)) == "null" {
		return nil, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}

	values := make([]string, 0, len(items))
	for i, item := range items {
		var value string
		if err := json.Unmarshal(item, &value); err == nil {
			value = strings.TrimSpace(value)
			if value != "" {
				values = append(values, value)
			}
			continue
		}

		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return nil, fmt.Errorf("entry %d must be a string or object: %w", i, err)
		}

		projected := ""
		for _, key := range objectKeys {
			raw, ok := fields[key]
			if !ok {
				continue
			}
			if err := json.Unmarshal(raw, &projected); err != nil {
				return nil, fmt.Errorf("entry %d field %q must be a string: %w", i, key, err)
			}
			projected = strings.TrimSpace(projected)
			if projected != "" {
				break
			}
		}
		if projected == "" {
			return nil, fmt.Errorf("entry %d must include one of: %s", i, strings.Join(objectKeys, ", "))
		}
		values = append(values, projected)
	}
	return values, nil
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
