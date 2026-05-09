package openscore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	provideriface "github.com/attach-dev/attach-guard/internal/provider"
	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestNewDefaultTimeoutAndValidation(t *testing.T) {
	prov, err := New(" http://127.0.0.1:8757/v0/verdict ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if prov.httpClient.Timeout != DefaultTimeoutSeconds*time.Second {
		t.Fatalf("expected default timeout %s, got %s", DefaultTimeoutSeconds*time.Second, prov.httpClient.Timeout)
	}
	if prov.endpoint != "http://127.0.0.1:8757/v0/verdict" {
		t.Fatalf("expected normalized endpoint, got %q", prov.endpoint)
	}

	tests := []struct {
		name           string
		endpoint       string
		timeoutSeconds int
	}{
		{name: "missing endpoint", endpoint: "", timeoutSeconds: 0},
		{name: "malformed endpoint", endpoint: "http://[::1", timeoutSeconds: 0},
		{name: "non http scheme", endpoint: "file:///tmp/verdict", timeoutSeconds: 0},
		{name: "missing host", endpoint: "https:///verdict", timeoutSeconds: 0},
		{name: "negative timeout", endpoint: "https://example.test/verdict", timeoutSeconds: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.endpoint, tt.timeoutSeconds); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestGetPackageScoreSendsMinimalPayloadAndPreservesVerdict(t *testing.T) {
	seenRequest := false
	prov := newMockProvider(func(r *http.Request) (*http.Response, error) {
		seenRequest = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", got)
		}

		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		want := map[string]string{
			"ecosystem": "npm",
			"name":      "synthetic-pkg",
			"version":   "1.2.3",
		}
		if len(payload) != len(want) {
			t.Fatalf("expected only coordinate fields, got %+v", payload)
		}
		for k, v := range want {
			if payload[k] != v {
				t.Fatalf("expected payload[%q]=%q, got %q", k, v, payload[k])
			}
		}

		return jsonResponse(http.StatusOK, `{"decision":"DENY","score":91,"reasons":["synthetic-risk"],"source_refs":["osv:TEST-0001"]}`), nil
	})

	info, err := prov.GetPackageScore(context.Background(), api.EcosystemNPM, "synthetic-pkg", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if !seenRequest {
		t.Fatal("server did not receive request")
	}
	if info.Version != "1.2.3" {
		t.Fatalf("expected version preserved, got %q", info.Version)
	}
	if info.Score != (api.PackageScore{}) {
		t.Fatalf("expected PackageScore to remain zero, got %+v", info.Score)
	}
	if info.ProviderVerdict == nil {
		t.Fatal("expected provider verdict")
	}
	if info.ProviderVerdict.Decision != api.ProviderVerdictDeny {
		t.Fatalf("expected DENY verdict, got %s", info.ProviderVerdict.Decision)
	}
	if info.ProviderVerdict.RiskScore == nil || *info.ProviderVerdict.RiskScore != 91 {
		t.Fatalf("expected risk score 91, got %v", info.ProviderVerdict.RiskScore)
	}
	if got := strings.Join(info.ProviderVerdict.Reasons, ","); got != "synthetic-risk" {
		t.Fatalf("expected reason preserved, got %q", got)
	}
	if got := strings.Join(info.ProviderVerdict.SourceRefs, ","); got != "osv:TEST-0001" {
		t.Fatalf("expected source ref preserved, got %q", got)
	}
}

func TestGetPackageScoreAcceptsAttachOpenScoreV0VerdictShape(t *testing.T) {
	prov := newMockProvider(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"schema_version": "attach-open-score/v0",
			"package": {
				"ecosystem": "pypi",
				"name": "synthetic-vulnerable-package",
				"version": "2.0.0",
				"purl": "pkg:pypi/synthetic-vulnerable-package@2.0.0",
				"resolved": true
			},
			"decision": "DENY",
			"score": 96,
			"confidence": "HIGH",
			"reasons": [
				{
					"code": "KNOWN_VULNERABILITY_CRITICAL",
					"severity": "CRITICAL",
					"decision_effect": "DENY",
					"message": "Synthetic fixture.",
					"source_ref_ids": ["synthetic-ghsa-critical"]
				}
			],
			"source_refs": [
				{
					"id": "synthetic-ghsa-critical",
					"source": "synthetic-github-advisory-database",
					"source_id": "GHSA-synth-crit-0001",
					"url": "https://example.invalid/advisories/GHSA-synth-crit-0001",
					"retrieved_at": "2026-05-05T00:00:00Z",
					"ttl_seconds": 86400,
					"license_or_terms_url": "https://example.invalid/terms",
					"attribution": "Synthetic fixture.",
					"attribution_required": false,
					"redistribution": "allowed",
					"public_display": "allowed"
				}
			],
			"evaluated_at": "2026-05-05T00:00:00Z",
			"ttl_seconds": 86400,
			"limitations": ["Synthetic fixture."]
		}`), nil
	})

	info, err := prov.GetPackageScore(context.Background(), api.EcosystemPyPI, "synthetic-vulnerable-package", "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.ProviderVerdict == nil {
		t.Fatal("expected provider verdict")
	}
	if info.ProviderVerdict.Decision != api.ProviderVerdictDeny {
		t.Fatalf("expected DENY verdict, got %s", info.ProviderVerdict.Decision)
	}
	if info.ProviderVerdict.RiskScore == nil || *info.ProviderVerdict.RiskScore != 96 {
		t.Fatalf("expected risk score 96, got %v", info.ProviderVerdict.RiskScore)
	}
	if got := strings.Join(info.ProviderVerdict.Reasons, ","); got != "KNOWN_VULNERABILITY_CRITICAL" {
		t.Fatalf("expected reason code preserved, got %q", got)
	}
	if got := strings.Join(info.ProviderVerdict.SourceRefs, ","); got != "synthetic-ghsa-critical" {
		t.Fatalf("expected source ref ID preserved, got %q", got)
	}
}

func TestGetPackageScoreAcceptsUppercaseDecisions(t *testing.T) {
	for _, decision := range []api.ProviderVerdictDecision{
		api.ProviderVerdictAllow,
		api.ProviderVerdictAsk,
		api.ProviderVerdictDeny,
		api.ProviderVerdictUnknown,
	} {
		t.Run(string(decision), func(t *testing.T) {
			prov := newMockProvider(func(r *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"decision":"`+string(decision)+`"}`), nil
			})

			info, err := prov.GetPackageScore(context.Background(), api.EcosystemPyPI, "synthetic-pkg", "2.0.0")
			if err != nil {
				t.Fatal(err)
			}
			if info.ProviderVerdict == nil || info.ProviderVerdict.Decision != decision {
				t.Fatalf("expected %s verdict, got %+v", decision, info.ProviderVerdict)
			}
		})
	}
}

func TestGetPackageScoreFailuresReturnUnknownProviderUnavailable(t *testing.T) {
	tests := []struct {
		name string
		prov *Provider
	}{
		{
			name: "transport failure",
			prov: newMockProvider(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("synthetic transport failure")
			}),
		},
		{
			name: "timeout",
			prov: &Provider{
				endpoint: "http://open-score.test/v0/verdict",
				httpClient: &http.Client{
					Timeout: 5 * time.Millisecond,
					Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}),
				},
			},
		},
		{
			name: "non 2xx",
			prov: newMockProvider(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusServiceUnavailable, `unavailable`), nil
			}),
		},
		{
			name: "malformed json",
			prov: newMockProvider(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `not json`), nil
			}),
		},
		{
			name: "missing decision",
			prov: newMockProvider(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"score":42}`), nil
			}),
		},
		{
			name: "unknown decision",
			prov: newMockProvider(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"decision":"allow"}`), nil
			}),
		},
		{
			name: "oversized body",
			prov: newMockProvider(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, strings.Repeat("x", maxResponseBodyBytes+1)), nil
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := tt.prov.GetPackageScore(context.Background(), api.EcosystemNPM, "synthetic-pkg", "1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			assertUnavailableUnknown(t, info)
		})
	}
}

func TestListVersionsUnsupported(t *testing.T) {
	prov, err := New("http://127.0.0.1:8757/v0/verdict", 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = prov.ListVersions(context.Background(), api.EcosystemNPM, "synthetic-pkg")
	if !errors.Is(err, provideriface.ErrUnsupportedSource) {
		t.Fatalf("expected ErrUnsupportedSource, got %v", err)
	}
}

func TestRedirectsReturnUnavailableUnknown(t *testing.T) {
	prov, err := New("http://open-score.test/v0/verdict", 1)
	if err != nil {
		t.Fatal(err)
	}
	prov.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "redirect-target.test" {
			t.Fatal("open-score provider followed redirect to another endpoint")
		}
		resp := jsonResponse(http.StatusFound, "")
		resp.Header.Set("Location", "http://redirect-target.test/v0/verdict")
		return resp, nil
	})

	info, err := prov.GetPackageScore(context.Background(), api.EcosystemNPM, "synthetic-pkg", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	assertUnavailableUnknown(t, info)
}

func assertUnavailableUnknown(t *testing.T, info *api.VersionInfo) {
	t.Helper()
	if info == nil {
		t.Fatal("expected VersionInfo, got nil")
	}
	if info.ProviderVerdict == nil {
		t.Fatal("expected ProviderVerdict, got nil")
	}
	if info.ProviderVerdict.Decision != api.ProviderVerdictUnknown {
		t.Fatalf("expected UNKNOWN decision, got %s", info.ProviderVerdict.Decision)
	}
	if len(info.ProviderVerdict.Reasons) != 1 || info.ProviderVerdict.Reasons[0] != providerReason {
		t.Fatalf("expected provider-unavailable reason, got %+v", info.ProviderVerdict.Reasons)
	}
	if info.ProviderVerdict.RiskScore != nil {
		t.Fatalf("expected no risk score on provider failure, got %v", info.ProviderVerdict.RiskScore)
	}
	if info.Score != (api.PackageScore{}) {
		t.Fatalf("expected no PackageScore on provider failure, got %+v", info.Score)
	}
}

func newMockProvider(fn func(*http.Request) (*http.Response, error)) *Provider {
	return &Provider{
		endpoint: "http://open-score.test/v0/verdict",
		httpClient: &http.Client{
			Transport: roundTripFunc(fn),
		},
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
