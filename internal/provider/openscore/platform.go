package openscore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// PlatformProvider talks to the Attach Platform hosted score edge
// (POST /v1/score/evaluations), authenticating with a per-user platform API key.
// It adapts the platform evaluation envelope into the same VersionInfo shape the
// other Open Score providers produce, so policy/enforcement is unchanged.
type PlatformProvider struct {
	endpoint   string
	authToken  string
	httpClient *http.Client
}

// PlatformOption customizes a PlatformProvider.
type PlatformOption func(*PlatformProvider)

// WithPlatformHTTPClient overrides the default HTTP client (test seam).
func WithPlatformHTTPClient(client *http.Client) PlatformOption {
	return func(p *PlatformProvider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

// NewPlatform creates a hosted platform score provider. A timeoutSeconds value of
// 0 uses DefaultTimeoutSeconds. The token is sent as a Bearer credential.
func NewPlatform(endpoint string, timeoutSeconds int, token string, opts ...PlatformOption) (*PlatformProvider, error) {
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

	prov := &PlatformProvider{
		endpoint:  endpoint,
		authToken: strings.TrimSpace(token),
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

// Name returns the provider name. The underlying scoring method is Attach Open
// Score regardless of the (local/http/platform) transport.
func (p *PlatformProvider) Name() string { return "open-score" }

// IsAvailable returns true for configured providers; per-package failures carry
// provider-unavailable verdicts as UNKNOWN.
func (p *PlatformProvider) IsAvailable(_ context.Context) bool { return true }

type platformTarget struct {
	Ecosystem api.Ecosystem `json:"ecosystem"`
	Name      string        `json:"name"`
	Version   string        `json:"version"`
}

type platformOptionsPayload struct {
	IncludeReasons    bool `json:"include_reasons"`
	IncludeSourceRefs bool `json:"include_source_refs"`
}

type platformRequest struct {
	Target  platformTarget         `json:"target"`
	Options platformOptionsPayload `json:"options"`
}

type platformResponse struct {
	AttachResult struct {
		OpenScoreDecision string `json:"open_score_decision"`
		Score             *struct {
			Value int `json:"value"`
		} `json:"score,omitempty"`
		Confidence string `json:"confidence,omitempty"`
	} `json:"attach_result"`
	Reasons    reasonList    `json:"reasons,omitempty"`
	SourceRefs sourceRefList `json:"source_refs,omitempty"`
}

// GetPackageScore fetches a hosted verdict for a package version.
func (p *PlatformProvider) GetPackageScore(ctx context.Context, ecosystem api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	payload := platformRequest{
		Target: platformTarget{
			Ecosystem: openScoreRequestEcosystem(ecosystem),
			Name:      name,
			Version:   version,
		},
		// Reasons/source refs are opt-in on the platform edge; request them so
		// verdicts carry the same detail as the direct providers.
		Options: platformOptionsPayload{IncludeReasons: true, IncludeSourceRefs: true},
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
	req.Header.Set("User-Agent", "attach-guard platform-score")
	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
	}

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

	var parsed platformResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return unavailableVersion(version), nil
	}

	// Adapt the platform envelope to the shared verdict shape. The Open Score
	// decision (ALLOW/ASK/DENY/UNKNOWN) maps 1:1 to the provider verdict.
	adapted := verdictResponse{
		Decision:   parsed.AttachResult.OpenScoreDecision,
		Confidence: parsed.AttachResult.Confidence,
		Reasons:    parsed.Reasons,
		SourceRefs: parsed.SourceRefs,
	}
	if parsed.AttachResult.Score != nil {
		value := parsed.AttachResult.Score.Value
		adapted.Score = &value
	}

	return versionInfoFromResponse(version, adapted), nil
}

// ListVersions is not implemented for the hosted platform provider.
func (p *PlatformProvider) ListVersions(_ context.Context, _ api.Ecosystem, _ string) ([]api.VersionInfo, error) {
	return nil, provider.ErrUnsupportedSource
}
