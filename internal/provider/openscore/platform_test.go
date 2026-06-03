package openscore

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestPlatformProviderMapsEvaluationEnvelope(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	prov, err := NewPlatform(
		"https://api.attach.test/v1/score/evaluations",
		0,
		"  plat-key  ",
		WithPlatformHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotAuth = r.Header.Get("Authorization")
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				return jsonResponse(http.StatusOK, `{
					"attach_result":{"open_score_decision":"ASK","platform_decision":"allow_with_warning","score":{"value":42,"scale":"0_to_100_riskier_is_higher"},"confidence":"MEDIUM"},
					"reasons":[{"code":"VERSION_TOO_NEW","severity":"MEDIUM","message":"x"}],
					"source_refs":[{"id":"src_1","url":"https://example.test"}]
				}`), nil
			}),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	info, err := prov.GetPackageScore(context.Background(), api.EcosystemNPM, "left-pad", "1.3.0")
	if err != nil {
		t.Fatal(err)
	}

	if gotAuth != "Bearer plat-key" {
		t.Fatalf("Authorization = %q, want trimmed Bearer plat-key", gotAuth)
	}

	target, _ := gotBody["target"].(map[string]any)
	if target["ecosystem"] != "npm" || target["name"] != "left-pad" || target["version"] != "1.3.0" {
		t.Fatalf("target payload = %#v", target)
	}
	opts, _ := gotBody["options"].(map[string]any)
	if opts["include_reasons"] != true || opts["include_source_refs"] != true {
		t.Fatalf("options payload = %#v", opts)
	}

	if info.ProviderVerdict == nil {
		t.Fatal("expected a provider verdict")
	}
	if info.ProviderVerdict.Decision != api.ProviderVerdictAsk {
		t.Fatalf("decision = %q, want ASK", info.ProviderVerdict.Decision)
	}
	if info.ProviderVerdict.RiskScore == nil || *info.ProviderVerdict.RiskScore != 42 {
		t.Fatalf("risk score = %v, want 42", info.ProviderVerdict.RiskScore)
	}
	if len(info.ProviderVerdict.Reasons) != 1 || info.ProviderVerdict.Reasons[0] != "VERSION_TOO_NEW" {
		t.Fatalf("reasons = %v", info.ProviderVerdict.Reasons)
	}
	if len(info.ProviderVerdict.SourceRefs) != 1 || info.ProviderVerdict.SourceRefs[0] != "src_1" {
		t.Fatalf("source_refs = %v", info.ProviderVerdict.SourceRefs)
	}
}

func TestPlatformProviderUnavailableOnNon2xx(t *testing.T) {
	prov, err := NewPlatform(
		"https://api.attach.test/v1/score/evaluations",
		1,
		"k",
		WithPlatformHTTPClient(&http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusUnauthorized, `{"error":{"code":"unauthenticated"}}`), nil
			}),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	info, err := prov.GetPackageScore(context.Background(), api.EcosystemNPM, "x", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if info.ProviderVerdict == nil || info.ProviderVerdict.Decision != api.ProviderVerdictUnknown {
		t.Fatalf("expected UNKNOWN provider-unavailable verdict, got %#v", info.ProviderVerdict)
	}
}

func TestNewPlatformValidatesEndpoint(t *testing.T) {
	if _, err := NewPlatform("", 0, "k"); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
	if _, err := NewPlatform("ftp://nope", 0, "k"); err == nil {
		t.Fatal("expected error for non-http endpoint")
	}
}
