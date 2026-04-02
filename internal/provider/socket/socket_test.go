package socket

import (
	"bytes"
	"context"
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

func TestGetPackageScoreGo_TreatsSocket404AsUnsupportedSource(t *testing.T) {
	prov := newTestProvider(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v0/go/private.example.com/mod/v1.2.3/score" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return newHTTPResponse(http.StatusNotFound, "not found"), nil
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
