package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/attach-dev/attach-guard/internal/audit"
	"github.com/attach-dev/attach-guard/internal/cli"
	"github.com/attach-dev/attach-guard/internal/config"
	openscoreprov "github.com/attach-dev/attach-guard/internal/provider/openscore"
	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestE2E_OpenScoreHTTPProviderAllowPreservesEvaluationAndAuditVerdict(t *testing.T) {
	eval, auditPath := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validateOpenScoreCoordinateRequest(t, r, api.EcosystemNPM, "safe-pkg", "1.0.0") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		writeOpenScoreObjectVerdict(t, w, api.ProviderVerdictAllow, 3, "HIGH", "low-risk-synthetic", "osv:synthetic-safe-0001")
	}))

	result, err := eval.Evaluate(context.Background(), "npm install safe-pkg@1.0.0", api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Allow {
		t.Fatalf("expected Allow for Open Score ALLOW verdict, got %s: %s", result.Decision, result.Reason)
	}
	if result.RewrittenCommand != "" {
		t.Fatalf("expected pinned command not to be rewritten, got %q", result.RewrittenCommand)
	}
	if len(result.Packages) != 1 {
		t.Fatalf("expected one package evaluation, got %d", len(result.Packages))
	}
	if result.Packages[0].SelectedVersion != "1.0.0" {
		t.Fatalf("expected selected version 1.0.0, got %q", result.Packages[0].SelectedVersion)
	}
	assertOpenScoreVerdict(t, result.Packages[0].ProviderVerdict, api.ProviderVerdictAllow, 3, "HIGH", "low-risk-synthetic", "osv:synthetic-safe-0001")
	assertNoRawOpenScoreSourceDump(t, result)

	entry := readOpenScoreAuditEntry(t, auditPath)
	if entry.Provider != "open-score" {
		t.Fatalf("expected audit provider open-score, got %q", entry.Provider)
	}
	if entry.Decision != api.Allow {
		t.Fatalf("expected audit decision allow, got %s", entry.Decision)
	}
	if len(entry.Packages) != 1 {
		t.Fatalf("expected one audit package, got %d", len(entry.Packages))
	}
	assertOpenScoreVerdict(t, entry.Packages[0].ProviderVerdict, api.ProviderVerdictAllow, 3, "HIGH", "low-risk-synthetic", "osv:synthetic-safe-0001")
	assertNoRawOpenScoreSourceDump(t, entry)
}

func TestE2E_OpenScoreHTTPProviderPreservesMultiEcosystemIdentityAndProvenance(t *testing.T) {
	tests := []struct {
		name             string
		command          string
		wantPM           string
		wantRequestEco   api.Ecosystem
		wantResultEco    api.Ecosystem
		wantPackageName  string
		wantVersion      string
		providerDecision api.ProviderVerdictDecision
		wantDecision     api.Decision
		riskScore        int
		confidence       string
		reason           string
		sourceRef        string
	}{
		{
			name:             "npm install",
			command:          "npm install npm-demo@1.0.0",
			wantPM:           "npm",
			wantRequestEco:   api.EcosystemNPM,
			wantResultEco:    api.EcosystemNPM,
			wantPackageName:  "npm-demo",
			wantVersion:      "1.0.0",
			providerDecision: api.ProviderVerdictAllow,
			wantDecision:     api.Allow,
			riskScore:        2,
			confidence:       "HIGH",
			reason:           "npm-low-risk-synthetic",
			sourceRef:        "osv:synthetic-npm-0001",
		},
		{
			name:             "pnpm add",
			command:          "pnpm add pnpm-demo@2.0.0",
			wantPM:           "pnpm",
			wantRequestEco:   api.EcosystemNPM,
			wantResultEco:    api.EcosystemPNPM,
			wantPackageName:  "pnpm-demo",
			wantVersion:      "2.0.0",
			providerDecision: api.ProviderVerdictAsk,
			wantDecision:     api.Ask,
			riskScore:        41,
			confidence:       "MEDIUM",
			reason:           "pnpm-review-synthetic",
			sourceRef:        "deps.dev:synthetic-pnpm-0001",
		},
		{
			name:             "pip install",
			command:          "pip install flask==3.0.0",
			wantPM:           "pip",
			wantRequestEco:   api.EcosystemPyPI,
			wantResultEco:    api.EcosystemPyPI,
			wantPackageName:  "flask",
			wantVersion:      "3.0.0",
			providerDecision: api.ProviderVerdictUnknown,
			wantDecision:     api.Ask,
			riskScore:        50,
			confidence:       "LOW",
			reason:           "pypi-insufficient-evidence-synthetic",
			sourceRef:        "osv:synthetic-pypi-0001",
		},
		{
			name:             "cargo install",
			command:          "cargo install ripgrep --version 14.0.0",
			wantPM:           "cargo",
			wantRequestEco:   api.EcosystemCargo,
			wantResultEco:    api.EcosystemCargo,
			wantPackageName:  "ripgrep",
			wantVersion:      "14.0.0",
			providerDecision: api.ProviderVerdictAllow,
			wantDecision:     api.Allow,
			riskScore:        7,
			confidence:       "HIGH",
			reason:           "cargo-low-risk-synthetic",
			sourceRef:        "registry:synthetic-cargo-0001",
		},
		{
			name:             "go install",
			command:          "go install golang.org/x/tools/cmd/godoc@v0.20.0",
			wantPM:           "go",
			wantRequestEco:   api.EcosystemGo,
			wantResultEco:    api.EcosystemGo,
			wantPackageName:  "golang.org/x/tools/cmd/godoc",
			wantVersion:      "v0.20.0",
			providerDecision: api.ProviderVerdictAsk,
			wantDecision:     api.Ask,
			riskScore:        37,
			confidence:       "MEDIUM",
			reason:           "go-review-synthetic",
			sourceRef:        "deps.dev:synthetic-go-0001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seenRequest := make(chan struct{}, 1)
			eval, auditPath := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case seenRequest <- struct{}{}:
				default:
				}
				if !validateOpenScoreCoordinateRequest(t, r, tt.wantRequestEco, tt.wantPackageName, tt.wantVersion) {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				writeOpenScoreObjectVerdict(t, w, tt.providerDecision, tt.riskScore, tt.confidence, tt.reason, tt.sourceRef)
			}))

			data, err := eval.EvaluateJSON(context.Background(), tt.command, api.ModeShell)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-seenRequest:
			default:
				t.Fatal("Open Score test provider did not receive a request")
			}

			var result api.EvaluationResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.Decision != tt.wantDecision {
				t.Fatalf("expected explain decision %s, got %s: %s", tt.wantDecision, result.Decision, result.Reason)
			}
			if len(result.Packages) != 1 {
				t.Fatalf("expected one explain package, got %d", len(result.Packages))
			}
			assertOpenScorePackage(t, result.Packages[0], tt.wantResultEco, tt.wantPackageName, tt.wantVersion)
			assertOpenScoreVerdict(t, result.Packages[0].ProviderVerdict, tt.providerDecision, tt.riskScore, tt.confidence, tt.reason, tt.sourceRef)
			assertNoRawOpenScoreSourceDump(t, result)

			entry := readOpenScoreAuditEntry(t, auditPath)
			if entry.PackageManager != tt.wantPM {
				t.Fatalf("expected audit package manager %q, got %q", tt.wantPM, entry.PackageManager)
			}
			if entry.Provider != "open-score" {
				t.Fatalf("expected audit provider open-score, got %q", entry.Provider)
			}
			if entry.Decision != tt.wantDecision {
				t.Fatalf("expected audit decision %s, got %s", tt.wantDecision, entry.Decision)
			}
			if len(entry.Packages) != 1 {
				t.Fatalf("expected one audit package, got %d", len(entry.Packages))
			}
			assertOpenScorePackage(t, entry.Packages[0], tt.wantResultEco, tt.wantPackageName, tt.wantVersion)
			assertOpenScoreVerdict(t, entry.Packages[0].ProviderVerdict, tt.providerDecision, tt.riskScore, tt.confidence, tt.reason, tt.sourceRef)
			assertNoRawOpenScoreSourceDump(t, entry)
		})
	}
}

func TestE2E_OpenScoreHTTPProviderDenyAndAskDriveLocalDecisions(t *testing.T) {
	tests := []struct {
		name         string
		pkg          string
		decision     api.ProviderVerdictDecision
		riskScore    int
		confidence   string
		reason       string
		sourceRef    string
		wantDecision api.Decision
	}{
		{
			name:         "deny",
			pkg:          "risky-pkg",
			decision:     api.ProviderVerdictDeny,
			riskScore:    94,
			confidence:   "HIGH",
			reason:       "deny-risk-synthetic",
			sourceRef:    "ghsa:synthetic-deny-0001",
			wantDecision: api.Deny,
		},
		{
			name:         "ask",
			pkg:          "review-pkg",
			decision:     api.ProviderVerdictAsk,
			riskScore:    47,
			confidence:   "MEDIUM",
			reason:       "manual-review-synthetic",
			sourceRef:    "deps.dev:synthetic-review-0001",
			wantDecision: api.Ask,
		},
		{
			name:         "unknown",
			pkg:          "unknown-pkg",
			decision:     api.ProviderVerdictUnknown,
			riskScore:    0,
			confidence:   "LOW",
			reason:       "insufficient-evidence-synthetic",
			sourceRef:    "osv:synthetic-unknown-0001",
			wantDecision: api.Ask,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, auditPath := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !validateOpenScoreCoordinateRequest(t, r, api.EcosystemNPM, tt.pkg, "1.0.0") {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				writeOpenScoreVerdict(t, w, tt.decision, tt.riskScore, tt.confidence, []string{tt.reason}, []string{tt.sourceRef})
			}))

			result, err := eval.Evaluate(context.Background(), "npm install "+tt.pkg+"@1.0.0", api.ModeShell)
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != tt.wantDecision {
				t.Fatalf("expected %s for Open Score %s verdict, got %s: %s", tt.wantDecision, tt.decision, result.Decision, result.Reason)
			}
			if !strings.Contains(result.Reason, tt.reason) {
				t.Fatalf("expected evaluator reason to preserve %q, got %q", tt.reason, result.Reason)
			}
			if len(result.Packages) != 1 {
				t.Fatalf("expected one package evaluation, got %d", len(result.Packages))
			}
			assertOpenScoreVerdict(t, result.Packages[0].ProviderVerdict, tt.decision, tt.riskScore, tt.confidence, tt.reason, tt.sourceRef)

			entry := readOpenScoreAuditEntry(t, auditPath)
			if entry.Decision != tt.wantDecision {
				t.Fatalf("expected audit decision %s, got %s", tt.wantDecision, entry.Decision)
			}
			if len(entry.Packages) != 1 {
				t.Fatalf("expected one audit package, got %d", len(entry.Packages))
			}
			assertOpenScoreVerdict(t, entry.Packages[0].ProviderVerdict, tt.decision, tt.riskScore, tt.confidence, tt.reason, tt.sourceRef)
		})
	}
}

func TestE2E_OpenScoreHTTPProviderFailuresAskLocallyWithProviderUnavailableVerdict(t *testing.T) {
	t.Run("outage", func(t *testing.T) {
		eval, _ := newClosedOpenScoreHTTPEvaluator(t)
		assertOpenScoreProviderFailureAsks(t, eval, "outage-pkg")
	})

	t.Run("non 2xx", func(t *testing.T) {
		eval, _ := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validateOpenScoreCoordinateRequest(t, r, api.EcosystemNPM, "status-pkg", "1.0.0") {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			http.Error(w, "synthetic unavailable", http.StatusServiceUnavailable)
		}))
		assertOpenScoreProviderFailureAsks(t, eval, "status-pkg")
	})

	t.Run("malformed response", func(t *testing.T) {
		eval, _ := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validateOpenScoreCoordinateRequest(t, r, api.EcosystemNPM, "malformed-pkg", "1.0.0") {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"decision":`))
		}))
		assertOpenScoreProviderFailureAsks(t, eval, "malformed-pkg")
	})

	t.Run("timeout", func(t *testing.T) {
		eval, _ := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validateOpenScoreCoordinateRequest(t, r, api.EcosystemNPM, "timeout-pkg", "1.0.0") {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			time.Sleep(2 * time.Second)
			writeOpenScoreVerdict(t, w, api.ProviderVerdictAllow, 1, "HIGH", []string{"late-synthetic"}, []string{"osv:late-synthetic"})
		}))
		assertOpenScoreProviderFailureAsks(t, eval, "timeout-pkg")
	})
}

func TestE2E_OpenScoreHTTPProviderFailuresAskAcrossPackageManagers(t *testing.T) {
	tests := []struct {
		name            string
		command         string
		wantRequestEco  api.Ecosystem
		wantPackageName string
		wantVersion     string
	}{
		{
			name:            "npm",
			command:         "npm install outage-npm@1.0.0",
			wantRequestEco:  api.EcosystemNPM,
			wantPackageName: "outage-npm",
			wantVersion:     "1.0.0",
		},
		{
			name:            "pnpm",
			command:         "pnpm add outage-pnpm@1.0.0",
			wantRequestEco:  api.EcosystemNPM,
			wantPackageName: "outage-pnpm",
			wantVersion:     "1.0.0",
		},
		{
			name:            "pip",
			command:         "pip install outage-pypi==1.0.0",
			wantRequestEco:  api.EcosystemPyPI,
			wantPackageName: "outage-pypi",
			wantVersion:     "1.0.0",
		},
		{
			name:            "cargo",
			command:         "cargo install outage-cargo --version 1.0.0",
			wantRequestEco:  api.EcosystemCargo,
			wantPackageName: "outage-cargo",
			wantVersion:     "1.0.0",
		},
		{
			name:            "go",
			command:         "go install example.com/outage/cmd/tool@v1.0.0",
			wantRequestEco:  api.EcosystemGo,
			wantPackageName: "example.com/outage/cmd/tool",
			wantVersion:     "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval, _ := newOpenScoreHTTPEvaluator(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !validateOpenScoreCoordinateRequest(t, r, tt.wantRequestEco, tt.wantPackageName, tt.wantVersion) {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				http.Error(w, "synthetic unavailable", http.StatusServiceUnavailable)
			}))
			assertOpenScoreProviderFailureCommandAsks(t, eval, tt.command)
		})
	}
}

func newOpenScoreHTTPEvaluator(t *testing.T, handler http.Handler) (*cli.Evaluator, string) {
	t.Helper()
	if server, ok := tryStartOpenScoreHTTPTestServer(handler); ok {
		t.Cleanup(server.Close)
		return newOpenScoreEvaluatorForEndpoint(t, server.URL)
	}

	client := openScoreHandlerClient(handler)
	return newOpenScoreEvaluatorForEndpoint(t, "http://127.0.0.1/v0/verdict", openscoreprov.WithHTTPClient(client))
}

func newClosedOpenScoreHTTPEvaluator(t *testing.T) (*cli.Evaluator, string) {
	t.Helper()
	if server, ok := tryStartOpenScoreHTTPTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("closed outage server should not receive requests")
	})); ok {
		endpoint := server.URL
		server.Close()
		return newOpenScoreEvaluatorForEndpoint(t, endpoint)
	}

	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("synthetic open-score outage")
		}),
	}
	return newOpenScoreEvaluatorForEndpoint(t, "http://127.0.0.1/v0/verdict", openscoreprov.WithHTTPClient(client))
}

func newOpenScoreEvaluatorForEndpoint(t *testing.T, endpoint string, opts ...openscoreprov.Option) (*cli.Evaluator, string) {
	t.Helper()

	timeoutSeconds := 1
	cfg := config.DefaultConfig()
	cfg.Provider.Kind = "open-score"
	cfg.Provider.Endpoint = endpoint
	cfg.Provider.TimeoutSeconds = &timeoutSeconds
	cfg.Logging.Path = filepath.Join(t.TempDir(), "audit.jsonl")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	prov, err := openscoreprov.New(cfg.Provider.Endpoint, *cfg.Provider.TimeoutSeconds, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return cli.NewEvaluator(cfg, prov), cfg.Logging.Path
}

func tryStartOpenScoreHTTPTestServer(handler http.Handler) (server *httptest.Server, ok bool) {
	defer func() {
		if recover() != nil {
			server = nil
			ok = false
		}
	}()
	server = httptest.NewServer(handler)
	return server, true
}

func openScoreHandlerClient(handler http.Handler) *http.Client {
	return &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			type handlerResult struct {
				resp *http.Response
				err  error
			}
			done := make(chan handlerResult, 1)
			go func() {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				done <- handlerResult{resp: recorder.Result()}
			}()

			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case result := <-done:
				return result.resp, result.err
			}
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func validateOpenScoreCoordinateRequest(t *testing.T, r *http.Request, wantEcosystem api.Ecosystem, wantName, wantVersion string) bool {
	t.Helper()

	ok := true
	if r.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", r.Method)
		ok = false
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Errorf("decode request body: %v", err)
		return false
	}

	want := map[string]string{
		"ecosystem": string(wantEcosystem),
		"name":      wantName,
		"version":   wantVersion,
	}
	if len(payload) != len(want) {
		t.Errorf("expected only package coordinate fields, got %+v", payload)
		ok = false
	}
	for key, wantValue := range want {
		if payload[key] != wantValue {
			t.Errorf("expected request %s=%q, got %q", key, wantValue, payload[key])
			ok = false
		}
	}
	return ok
}

func writeOpenScoreVerdict(t *testing.T, w http.ResponseWriter, decision api.ProviderVerdictDecision, riskScore int, confidence string, reasons, sourceRefs []string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	response := struct {
		Decision   api.ProviderVerdictDecision `json:"decision"`
		Score      int                         `json:"score"`
		Confidence string                      `json:"confidence,omitempty"`
		Reasons    []string                    `json:"reasons,omitempty"`
		SourceRefs []string                    `json:"source_refs,omitempty"`
	}{
		Decision:   decision,
		Score:      riskScore,
		Confidence: confidence,
		Reasons:    reasons,
		SourceRefs: sourceRefs,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func writeOpenScoreObjectVerdict(t *testing.T, w http.ResponseWriter, decision api.ProviderVerdictDecision, riskScore int, confidence, reason, sourceRef string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"schema_version": "attach-open-score/v0",
		"decision":       decision,
		"score":          riskScore,
		"confidence":     confidence,
		"reasons": []map[string]interface{}{
			{
				"code":           reason,
				"severity":       "LOW",
				"message":        "Synthetic fixture.",
				"source_ref_ids": []string{sourceRef},
			},
		},
		"source_refs": []map[string]interface{}{
			{
				"id":          sourceRef,
				"source":      "synthetic-upstream-source",
				"source_id":   "SYNTHETIC-UPSTREAM-0001",
				"url":         "https://example.invalid/synthetic",
				"attribution": "Synthetic fixture.",
			},
		},
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func assertOpenScoreVerdict(t *testing.T, verdict *api.ProviderVerdict, decision api.ProviderVerdictDecision, riskScore int, confidence, reason, sourceRef string) {
	t.Helper()

	if verdict == nil {
		t.Fatal("expected provider verdict")
	}
	if verdict.Decision != decision {
		t.Fatalf("expected verdict %s, got %s", decision, verdict.Decision)
	}
	if verdict.RiskScore == nil || *verdict.RiskScore != riskScore {
		t.Fatalf("expected risk score %d, got %v", riskScore, verdict.RiskScore)
	}
	if verdict.Confidence != confidence {
		t.Fatalf("expected confidence %q, got %q", confidence, verdict.Confidence)
	}
	if len(verdict.Reasons) != 1 || verdict.Reasons[0] != reason {
		t.Fatalf("expected reason %q, got %#v", reason, verdict.Reasons)
	}
	if len(verdict.SourceRefs) != 1 || verdict.SourceRefs[0] != sourceRef {
		t.Fatalf("expected source ref %q, got %#v", sourceRef, verdict.SourceRefs)
	}
}

func assertOpenScorePackage(t *testing.T, pkg api.PackageEvaluation, ecosystem api.Ecosystem, name, version string) {
	t.Helper()

	if pkg.Ecosystem != ecosystem {
		t.Fatalf("expected package ecosystem %q, got %q", ecosystem, pkg.Ecosystem)
	}
	if pkg.Name != name {
		t.Fatalf("expected package name %q, got %q", name, pkg.Name)
	}
	if pkg.SelectedVersion != version {
		t.Fatalf("expected selected version %q, got %q", version, pkg.SelectedVersion)
	}
}

func assertNoRawOpenScoreSourceDump(t *testing.T, value interface{}) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, unexpected := range []string{
		"synthetic-upstream-source",
		"SYNTHETIC-UPSTREAM-0001",
		"Synthetic fixture.",
		"source_ref_ids",
		"license_or_terms_url",
		"redistribution",
		"public_display",
	} {
		if strings.Contains(string(data), unexpected) {
			t.Fatalf("expected output to omit raw Open Score source object details %q, got %s", unexpected, data)
		}
	}
}

func assertOpenScoreProviderFailureAsks(t *testing.T, eval *cli.Evaluator, pkg string) {
	t.Helper()

	assertOpenScoreProviderFailureCommandAsks(t, eval, "npm install "+pkg+"@1.0.0")
}

func assertOpenScoreProviderFailureCommandAsks(t *testing.T, eval *cli.Evaluator, command string) {
	t.Helper()

	result, err := eval.Evaluate(context.Background(), command, api.ModeShell)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != api.Ask {
		t.Fatalf("expected local provider failure to Ask, got %s: %s", result.Decision, result.Reason)
	}
	if !strings.Contains(result.Reason, "risk provider is unavailable") {
		t.Fatalf("expected provider-unavailable policy reason, got %q", result.Reason)
	}
	if len(result.Packages) != 1 {
		t.Fatalf("expected one package evaluation, got %d", len(result.Packages))
	}

	verdict := result.Packages[0].ProviderVerdict
	if verdict == nil {
		t.Fatal("expected provider-unavailable verdict to be preserved")
	}
	if verdict.Decision != api.ProviderVerdictUnknown {
		t.Fatalf("expected UNKNOWN verdict, got %s", verdict.Decision)
	}
	if verdict.RiskScore != nil {
		t.Fatalf("expected no risk score on provider-unavailable verdict, got %v", verdict.RiskScore)
	}
	if verdict.Confidence != "" {
		t.Fatalf("expected no confidence on provider-unavailable verdict, got %q", verdict.Confidence)
	}
	if len(verdict.Reasons) != 1 || verdict.Reasons[0] != "provider-unavailable" {
		t.Fatalf("expected provider-unavailable reason, got %#v", verdict.Reasons)
	}
}

func readOpenScoreAuditEntry(t *testing.T, path string) audit.Entry {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(lines))
	}

	var entry audit.Entry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}
