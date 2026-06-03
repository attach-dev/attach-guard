package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Provider.Kind != "open-score" {
		t.Errorf("expected provider=open-score, got %s", cfg.Provider.Kind)
	}
	if cfg.Provider.Command != "attach-open-score" {
		t.Errorf("expected open-score command attach-open-score, got %q", cfg.Provider.Command)
	}
	if cfg.Policy.MinSupplyChainScore != 70 {
		t.Errorf("expected min_supply_chain_score=70, got %f", cfg.Policy.MinSupplyChainScore)
	}
	if cfg.Policy.MinimumPackageAgeHours != 48 {
		t.Errorf("expected minimum_package_age_hours=48, got %d", cfg.Policy.MinimumPackageAgeHours)
	}
	if cfg.Policy.UnknownBehavior.Local != "ask" || cfg.Policy.UnknownBehavior.CI != "deny" {
		t.Errorf("expected unknown_behavior local=ask ci=deny, got %+v", cfg.Policy.UnknownBehavior)
	}
	if !cfg.PackageManagers.NPM {
		t.Error("expected npm enabled")
	}
	if !cfg.PackageManagers.PNPM {
		t.Error("expected pnpm enabled")
	}
}

func TestWriteAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Policy.DenyKnownMalware != true {
		t.Error("expected deny_known_malware=true")
	}
}

func TestLoadFromFileMergesUnknownBehaviorAndPreservesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("policy:\n  unknown_behavior:\n    local: allow\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Policy.UnknownBehavior.Local != "allow" {
		t.Fatalf("expected unknown_behavior.local=allow, got %q", cfg.Policy.UnknownBehavior.Local)
	}
	if cfg.Policy.UnknownBehavior.CI != "deny" {
		t.Fatalf("expected partial config to preserve unknown_behavior.ci default deny, got %q", cfg.Policy.UnknownBehavior.CI)
	}
	if cfg.Policy.ProviderUnavailable.Local != "ask" || cfg.Policy.ProviderUnavailable.CI != "deny" {
		t.Fatalf("expected provider_unavailable defaults preserved, got %+v", cfg.Policy.ProviderUnavailable)
	}
}

func TestLoadFromFileOpenScoreProviderConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("provider:\n  kind: open-score\n  endpoint: http://127.0.0.1:8757/v0/verdict\n  timeout_seconds: 3\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Provider.Kind != "open-score" {
		t.Fatalf("expected provider kind open-score, got %q", cfg.Provider.Kind)
	}
	if cfg.Provider.Endpoint != "http://127.0.0.1:8757/v0/verdict" {
		t.Fatalf("expected endpoint to load, got %q", cfg.Provider.Endpoint)
	}
	if cfg.Provider.TimeoutSeconds == nil || *cfg.Provider.TimeoutSeconds != 3 {
		t.Fatalf("expected timeout_seconds=3, got %v", cfg.Provider.TimeoutSeconds)
	}
}

func TestLoadFromFileOpenScoreValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "malformed endpoint",
			config:  "provider:\n  kind: open-score\n  endpoint: http://[::1\n",
			wantErr: "provider.endpoint must be a valid URL",
		},
		{
			name:    "non http scheme",
			config:  "provider:\n  kind: open-score\n  endpoint: file:///tmp/verdict\n",
			wantErr: "provider.endpoint must use http or https",
		},
		{
			name:    "missing host",
			config:  "provider:\n  kind: open-score\n  endpoint: http:///verdict\n",
			wantErr: "provider.endpoint must include a host",
		},
		{
			name:    "zero timeout",
			config:  "provider:\n  kind: open-score\n  endpoint: http://127.0.0.1:8757/v0/verdict\n  timeout_seconds: 0\n",
			wantErr: "provider.timeout_seconds must be positive",
		},
		{
			name:    "negative timeout",
			config:  "provider:\n  kind: open-score\n  endpoint: http://127.0.0.1:8757/v0/verdict\n  timeout_seconds: -1\n",
			wantErr: "provider.timeout_seconds must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadFromFile(path)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestPluginConfigDir(t *testing.T) {
	dir := t.TempDir()

	// Write a plugin config with a custom score threshold
	pluginCfg := []byte("policy:\n  min_supply_chain_score: 42\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), pluginCfg, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ATTACH_GUARD_PLUGIN_CONFIG_DIR", dir)
	// Point HOME to an empty dir so user-global config doesn't interfere
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Policy.MinSupplyChainScore != 42 {
		t.Errorf("expected plugin config min_supply_chain_score=42, got %f", cfg.Policy.MinSupplyChainScore)
	}
	// Other defaults should be preserved
	if cfg.Provider.Kind != "open-score" {
		t.Errorf("expected provider=open-score, got %s", cfg.Provider.Kind)
	}
}

func TestBundledPluginConfigMatchesDefaults(t *testing.T) {
	// Ensure the bundled plugin/config/config.yaml stays in sync with DefaultConfig().
	// If this test fails, update the bundled YAML or DefaultConfig() so they match.
	bundledPath := filepath.Join("..", "..", "plugin", "config", "config.yaml")
	bundled, err := LoadFromFile(bundledPath)
	if err != nil {
		t.Skipf("bundled plugin config not found (expected in repo root): %v", err)
	}

	defaults := DefaultConfig()

	if bundled.Provider != defaults.Provider {
		t.Errorf("provider mismatch:\n  bundled: %+v\n  default: %+v", bundled.Provider, defaults.Provider)
	}
	if bundled.Policy.DenyKnownMalware != defaults.Policy.DenyKnownMalware ||
		bundled.Policy.MinSupplyChainScore != defaults.Policy.MinSupplyChainScore ||
		bundled.Policy.MinOverallScore != defaults.Policy.MinOverallScore ||
		bundled.Policy.GrayBandMinSupplyChain != defaults.Policy.GrayBandMinSupplyChain ||
		bundled.Policy.MinimumPackageAgeHours != defaults.Policy.MinimumPackageAgeHours {
		t.Errorf("policy thresholds mismatch:\n  bundled: %+v\n  default: %+v", bundled.Policy, defaults.Policy)
	}
	if bundled.Policy.ProviderUnavailable != defaults.Policy.ProviderUnavailable {
		t.Errorf("provider_unavailable_behavior mismatch:\n  bundled: %+v\n  default: %+v",
			bundled.Policy.ProviderUnavailable, defaults.Policy.ProviderUnavailable)
	}
	if bundled.Policy.UnknownBehavior != defaults.Policy.UnknownBehavior {
		t.Errorf("unknown_behavior mismatch:\n  bundled: %+v\n  default: %+v",
			bundled.Policy.UnknownBehavior, defaults.Policy.UnknownBehavior)
	}
	if bundled.PackageManagers != defaults.PackageManagers {
		t.Errorf("package_managers mismatch:\n  bundled: %+v\n  default: %+v",
			bundled.PackageManagers, defaults.PackageManagers)
	}
	if bundled.Logging != defaults.Logging {
		t.Errorf("logging mismatch:\n  bundled: %+v\n  default: %+v", bundled.Logging, defaults.Logging)
	}
}

func TestEnvOverrides(t *testing.T) {
	os.Setenv("ATTACH_GUARD_LOG_PATH", "/tmp/test-audit.jsonl")
	defer os.Unsetenv("ATTACH_GUARD_LOG_PATH")

	cfg := DefaultConfig()
	applyEnvOverrides(cfg)

	if cfg.Logging.Path != "/tmp/test-audit.jsonl" {
		t.Errorf("expected log path from env, got %s", cfg.Logging.Path)
	}
}

func TestOpenScoreEnvOverrides(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.APITokenEnv = "SOCKET_API_TOKEN"
	t.Setenv("ATTACH_OPEN_SCORE_ENDPOINT", "http://127.0.0.1:8757/v0/verdict")
	t.Setenv("ATTACH_OPEN_SCORE_BIN", "/tmp/attach-open-score")

	applyEnvOverrides(cfg)

	if cfg.Provider.Kind != "open-score" {
		t.Fatalf("expected provider open-score, got %q", cfg.Provider.Kind)
	}
	if cfg.Provider.Endpoint != "http://127.0.0.1:8757/v0/verdict" {
		t.Fatalf("endpoint override = %q", cfg.Provider.Endpoint)
	}
	if cfg.Provider.APITokenEnv != "ATTACH_OPEN_SCORE_API_TOKEN" {
		t.Fatalf("api token env override = %q", cfg.Provider.APITokenEnv)
	}
	if cfg.Provider.Command != "/tmp/attach-open-score" {
		t.Fatalf("command override = %q", cfg.Provider.Command)
	}
}

func TestResolveLogPath(t *testing.T) {
	cfg := DefaultConfig()
	path := cfg.ResolveLogPath()

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".attach-guard", "audit.jsonl")

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}
