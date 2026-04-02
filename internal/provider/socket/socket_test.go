package socket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestProvider(rt roundTripFunc) *Provider {
	return &Provider{
		apiToken: "test-token",
		httpClient: &http.Client{
			Transport: rt,
			Timeout:   defaultTimeout,
		},
	}
}

func newHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

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

func TestPurlEcosystem(t *testing.T) {
	tests := []struct {
		eco  api.Ecosystem
		want string
	}{
		{api.EcosystemPyPI, "pypi"},
		{api.EcosystemGo, "golang"},
		{api.EcosystemCargo, "cargo"},
	}

	for _, tt := range tests {
		if got := purlEcosystem(tt.eco); got != tt.want {
			t.Errorf("purlEcosystem(%q) = %q, want %q", tt.eco, got, tt.want)
		}
	}
}

func TestEscapeModulePath(t *testing.T) {
	if got, want := escapeModulePath("github.com/Azure/azure-sdk-for-go"), "github.com/!azure/azure-sdk-for-go"; got != want {
		t.Fatalf("escapeModulePath() = %q, want %q", got, want)
	}
}

func TestOrderPyPIVersionsPrefersStableVersionPrecedence(t *testing.T) {
	now := time.Now()
	ordered := orderPyPIVersions([]orderedVersion{
		{Version: "1.5.9", PublishedAt: now},
		{Version: "2.0.0", PublishedAt: now.Add(-2 * time.Hour)},
		{Version: "2.1.0rc1", PublishedAt: now.Add(time.Hour)},
	})

	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2 stable releases", len(ordered))
	}
	if ordered[0].Version != "2.0.0" || ordered[1].Version != "1.5.9" {
		t.Fatalf("ordered versions = %#v, want [2.0.0 1.5.9]", ordered)
	}
}

func TestOrderGoVersionsPrefersTaggedReleases(t *testing.T) {
	now := time.Now()
	ordered := orderGoVersions([]orderedVersion{
		{Version: "v1.2.0-rc.1", PublishedAt: now.Add(time.Hour)},
		{Version: "v1.1.9", PublishedAt: now},
		{Version: "v1.2.0", PublishedAt: now.Add(-2 * time.Hour)},
		{Version: "v1.3.0-0.20240401010203-deadbeefcafe", PublishedAt: now.Add(2 * time.Hour)},
	})

	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2 release versions", len(ordered))
	}
	if ordered[0].Version != "v1.2.0" || ordered[1].Version != "v1.1.9" {
		t.Fatalf("ordered versions = %#v, want [v1.2.0 v1.1.9]", ordered)
	}
}

func TestOrderCargoVersionsPrefersStableSemver(t *testing.T) {
	now := time.Now()
	ordered := orderCargoVersions([]orderedVersion{
		{Version: "0.9.9", PublishedAt: now},
		{Version: "1.0.0", PublishedAt: now.Add(-2 * time.Hour)},
		{Version: "1.1.0-rc.1", PublishedAt: now.Add(time.Hour)},
	})

	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2 stable versions", len(ordered))
	}
	if ordered[0].Version != "1.0.0" || ordered[1].Version != "0.9.9" {
		t.Fatalf("ordered versions = %#v, want [1.0.0 0.9.9]", ordered)
	}
}

func TestOrderedPyPIReleasesSkipsDeletedVersions(t *testing.T) {
	now := time.Now()
	ordered := orderedPyPIReleases(map[string][]pypiFileInfo{
		"1.0.0": {},
		"0.9.0": {
			{UploadTimeISO8601: now.Add(-time.Hour).Format(time.RFC3339)},
		},
	})

	if len(ordered) != 1 {
		t.Fatalf("len(ordered) = %d, want 1", len(ordered))
	}
	if ordered[0].Version != "0.9.0" {
		t.Fatalf("ordered version = %q, want 0.9.0", ordered[0].Version)
	}
}

func TestListOrderedVersionsPyPI_ParsesRegistryResponse(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "pypi.org" {
			t.Fatalf("unexpected host %q", req.URL.Host)
		}
		return newHTTPResponse(http.StatusOK, `{
			"releases": {
				"1.0.0": [{"upload_time_iso_8601":"2024-01-01T00:00:00"}],
				"1.1.0": [{"upload_time_iso_8601":"2024-02-01T00:00:00Z"}]
			}
		}`), nil
	})

	ordered, err := prov.listOrderedVersionsPyPI(context.Background(), "demo")
	if err != nil {
		t.Fatalf("listOrderedVersionsPyPI() error = %v", err)
	}
	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2", len(ordered))
	}
	if ordered[0].Version != "1.1.0" || ordered[1].Version != "1.0.0" {
		t.Fatalf("ordered versions = %#v, want [1.1.0 1.0.0]", ordered)
	}
}

func TestListOrderedVersionsGo_ParsesProxyResponses(t *testing.T) {
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPROXY", "https://proxy.golang.org,direct")

	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/example.com/mod/@v/list":
			return newHTTPResponse(http.StatusOK, "v1.0.0\nv1.1.0\n"), nil
		case "/example.com/mod/@v/v1.0.0.info":
			return newHTTPResponse(http.StatusOK, `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`), nil
		case "/example.com/mod/@v/v1.1.0.info":
			return newHTTPResponse(http.StatusOK, `{"Version":"v1.1.0","Time":"2024-02-01T00:00:00Z"}`), nil
		default:
			t.Fatalf("unexpected path %q", req.URL.Path)
			return nil, nil
		}
	})

	ordered, err := prov.listOrderedVersionsGo(context.Background(), "example.com/mod")
	if err != nil {
		t.Fatalf("listOrderedVersionsGo() error = %v", err)
	}
	if len(ordered) != 2 {
		t.Fatalf("len(ordered) = %d, want 2", len(ordered))
	}
	if ordered[0].Version != "v1.1.0" || ordered[1].Version != "v1.0.0" {
		t.Fatalf("ordered versions = %#v, want [v1.1.0 v1.0.0]", ordered)
	}
}

func TestListOrderedVersionsGo_LimitsInfoFetchesToCandidateCap(t *testing.T) {
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPROXY", "https://proxy.golang.org,direct")

	var (
		listBody    strings.Builder
		infoFetches int32
	)
	for i := 0; i < 15; i++ {
		if i > 0 {
			listBody.WriteByte('\n')
		}
		fmt.Fprintf(&listBody, "v1.0.%d", i)
	}

	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/example.com/mod/@v/list":
			return newHTTPResponse(http.StatusOK, listBody.String()), nil
		default:
			if strings.HasPrefix(req.URL.Path, "/example.com/mod/@v/") && strings.HasSuffix(req.URL.Path, ".info") {
				atomic.AddInt32(&infoFetches, 1)
				version := strings.TrimPrefix(req.URL.Path, "/example.com/mod/@v/")
				version = strings.TrimSuffix(version, ".info")
				return newHTTPResponse(http.StatusOK, fmt.Sprintf(`{"Version":"%s","Time":"2024-01-01T00:00:00Z"}`, version)), nil
			}
			t.Fatalf("unexpected path %q", req.URL.Path)
			return nil, nil
		}
	})

	ordered, err := prov.listOrderedVersionsGo(context.Background(), "example.com/mod")
	if err != nil {
		t.Fatalf("listOrderedVersionsGo() error = %v", err)
	}
	if len(ordered) != maxCandidates {
		t.Fatalf("len(ordered) = %d, want %d", len(ordered), maxCandidates)
	}
	if got := atomic.LoadInt32(&infoFetches); got != int32(maxCandidates) {
		t.Fatalf("info fetches = %d, want %d", got, maxCandidates)
	}
}

func TestListOrderedVersionsGo_TreatsProxy404AsUnsupportedSource(t *testing.T) {
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPROXY", "https://proxy.golang.org,direct")

	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/private.example.com/mod/@v/list" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return newHTTPResponse(http.StatusNotFound, "not found"), nil
	})

	_, err := prov.listOrderedVersionsGo(context.Background(), "private.example.com/mod")
	if !errors.Is(err, provider.ErrUnsupportedSource) {
		t.Fatalf("listOrderedVersionsGo() error = %v, want ErrUnsupportedSource", err)
	}
}

func TestGetScoreByPurl_ParsesNDJSON(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/v0/purl" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}

		var payload purlRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if len(payload.Components) != 1 || payload.Components[0].PURL != "pkg:pypi/litellm@1.82.8" {
			t.Fatalf("components = %#v, want single litellm purl", payload.Components)
		}

		return newHTTPResponse(http.StatusOK, strings.Join([]string{
			`{"inputPurl":"pkg:pypi/litellm@1.82.8","version":"1.82.8","score":{"supplyChain":0.73,"overall":0.81},"alerts":[{"type":"malware","severity":"high","category":"supply-chain"}]}`,
			`{"_type":"summary"}`,
		}, "\n")), nil
	})

	info, err := prov.getScoreByPurl(context.Background(), api.EcosystemPyPI, "litellm", "1.82.8")
	if err != nil {
		t.Fatalf("getScoreByPurl() error = %v", err)
	}
	if info.Version != "1.82.8" {
		t.Fatalf("Version = %q, want 1.82.8", info.Version)
	}
	if info.Score.SupplyChain != 73 || info.Score.Overall != 81 {
		t.Fatalf("score = %+v, want supply=73 overall=81", info.Score)
	}
	if len(info.Alerts) != 1 || info.Alerts[0].Title != "malware" {
		t.Fatalf("alerts = %#v, want single malware alert", info.Alerts)
	}
}

func TestGetScoreByPurl_AggregatesWorstCaseAcrossArtifacts(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v0/purl" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return newHTTPResponse(http.StatusOK, strings.Join([]string{
			`{"inputPurl":"pkg:pypi/litellm@1.82.8","version":"1.82.8","release":"sdist","score":{"supplyChain":0.73,"overall":0.81},"alerts":[{"type":"malware","severity":"high","category":"supply-chain"}]}`,
			`{"inputPurl":"pkg:pypi/litellm@1.82.8","version":"1.82.8","release":"wheel","score":{"supplyChain":0.20,"overall":0.40},"alerts":[{"type":"native-code","severity":"medium","category":"quality"},{"type":"malware","severity":"high","category":"supply-chain"}]}`,
		}, "\n")), nil
	})

	info, err := prov.getScoreByPurl(context.Background(), api.EcosystemPyPI, "litellm", "1.82.8")
	if err != nil {
		t.Fatalf("getScoreByPurl() error = %v", err)
	}
	if info.Score.SupplyChain != 20 || info.Score.Overall != 40 {
		t.Fatalf("score = %+v, want supply=20 overall=40", info.Score)
	}
	if len(info.Alerts) != 2 {
		t.Fatalf("alerts = %#v, want 2 deduped alerts", info.Alerts)
	}
}

func TestGetScoreByPurl_MissingArtifactReturnsUnsupportedSource(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v0/purl" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return newHTTPResponse(http.StatusOK, `{"_type":"purlError","message":"not found"}`), nil
	})

	_, err := prov.getScoreByPurl(context.Background(), api.EcosystemGo, "private.example.com/mod", "v1.2.3")
	if !errors.Is(err, provider.ErrUnsupportedSource) {
		t.Fatalf("getScoreByPurl() error = %v, want ErrUnsupportedSource", err)
	}
}

func TestGetPackageScorePyPI_UsesPurl(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/v0/purl" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		return newHTTPResponse(http.StatusOK, `{"inputPurl":"pkg:pypi/litellm@1.82.8","version":"1.82.8","score":{"supplyChain":0.73,"overall":0.81}}`), nil
	})

	info, err := prov.GetPackageScore(context.Background(), api.EcosystemPyPI, "litellm", "1.82.8")
	if err != nil {
		t.Fatalf("GetPackageScore() error = %v", err)
	}
	if info.Score.Overall != 81 {
		t.Fatalf("Overall = %v, want 81", info.Score.Overall)
	}
}

func TestGetPackageScorePyPI_PurlScopeError(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		return newHTTPResponse(http.StatusForbidden, "missing scope packages:list"), nil
	})

	_, err := prov.GetPackageScore(context.Background(), api.EcosystemPyPI, "litellm", "1.82.8")
	if err == nil || !strings.Contains(err.Error(), "packages:list") {
		t.Fatalf("GetPackageScore() error = %v, want packages:list scope hint", err)
	}
}

func TestListVersionsPyPI_UsesBatchPurlAndCapsCandidates(t *testing.T) {
	var postCalls int
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Host == "pypi.org":
			var releases strings.Builder
			releases.WriteString(`{"releases":{`)
			for i := 0; i < 12; i++ {
				if i > 0 {
					releases.WriteByte(',')
				}
				fmt.Fprintf(&releases, `"1.0.%d":[{"upload_time_iso_8601":"2024-01-%02dT00:00:00Z"}]`, i, i+1)
			}
			releases.WriteString(`}}`)
			return newHTTPResponse(http.StatusOK, releases.String()), nil
		case req.Method == http.MethodPost && req.URL.Path == "/v0/purl":
			postCalls++
			var payload purlRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decoding request body: %v", err)
			}
			if len(payload.Components) != maxCandidates {
				t.Fatalf("len(components) = %d, want %d", len(payload.Components), maxCandidates)
			}

			lines := make([]string, 0, len(payload.Components))
			for _, component := range payload.Components {
				version := strings.TrimPrefix(component.PURL, "pkg:pypi/demo@")
				lines = append(lines, fmt.Sprintf(`{"inputPurl":"%s","version":"%s","score":{"supplyChain":0.9,"overall":0.8}}`, component.PURL, version))
			}
			return newHTTPResponse(http.StatusOK, strings.Join(lines, "\n")), nil
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})

	versions, err := prov.ListVersions(context.Background(), api.EcosystemPyPI, "demo")
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	if len(versions) != maxCandidates {
		t.Fatalf("len(versions) = %d, want %d", len(versions), maxCandidates)
	}
	if postCalls != 1 {
		t.Fatalf("post calls = %d, want 1", postCalls)
	}
}

func TestBatchScoreByPurl_MissingVersionReturnsUnsupportedSource(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v0/purl" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return newHTTPResponse(http.StatusOK, `{"inputPurl":"pkg:cargo/serde@1.0.200","version":"1.0.200","score":{"supplyChain":0.9,"overall":0.8}}`), nil
	})

	_, err := prov.batchScoreByPurl(context.Background(), api.EcosystemCargo, "serde", []orderedVersion{
		{Version: "1.0.200"},
		{Version: "1.0.201"},
	})
	if !errors.Is(err, provider.ErrUnsupportedSource) {
		t.Fatalf("batchScoreByPurl() error = %v, want ErrUnsupportedSource", err)
	}
}

func TestGetPackageScoreGo_TreatsMissingPurlResultAsUnsupportedSource(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v0/purl" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return newHTTPResponse(http.StatusOK, `{"_type":"summary"}`), nil
	})

	_, err := prov.GetPackageScore(context.Background(), api.EcosystemGo, "private.example.com/mod", "v1.2.3")
	if !errors.Is(err, provider.ErrUnsupportedSource) {
		t.Fatalf("GetPackageScore() error = %v, want ErrUnsupportedSource", err)
	}
}

func TestListOrderedVersionsCargo_SetsUserAgent(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("User-Agent"); got != cratesUserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, cratesUserAgent)
		}
		return newHTTPResponse(http.StatusOK, `{
			"versions": [
				{"num":"1.0.0","created_at":"2024-01-01T00:00:00Z","yanked":false}
			]
		}`), nil
	})

	ordered, err := prov.listOrderedVersionsCargo(context.Background(), "demo")
	if err != nil {
		t.Fatalf("listOrderedVersionsCargo() error = %v", err)
	}
	if len(ordered) != 1 || ordered[0].Version != "1.0.0" {
		t.Fatalf("ordered versions = %#v, want [1.0.0]", ordered)
	}
}

func TestGoModuleUsesPublicSources_DefaultProxy(t *testing.T) {
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPROXY", "https://proxy.golang.org,direct")

	if !goModuleUsesPublicSources("golang.org/x/net") {
		t.Fatal("expected public go module to use public sources")
	}
}

func TestGoModuleUsesPublicSources_PrivatePattern(t *testing.T) {
	t.Setenv("GOPRIVATE", "private.example.com,*.corp.example.com")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPROXY", "https://proxy.golang.org,direct")

	if goModuleUsesPublicSources("private.example.com/team/module") {
		t.Fatal("expected GOPRIVATE module to bypass public sources")
	}
	if goModuleUsesPublicSources("api.corp.example.com/team/module") {
		t.Fatal("expected glob-matched GOPRIVATE module to bypass public sources")
	}
}

func TestGoModuleUsesPublicSources_CustomProxy(t *testing.T) {
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPROXY", "https://proxy.corp.example.com,https://proxy.golang.org,direct")

	if goModuleUsesPublicSources("golang.org/x/net") {
		t.Fatal("expected custom GOPROXY to disable public-source evaluation")
	}
}
