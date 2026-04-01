package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Provider.Kind != "socket" {
		t.Errorf("expected provider=socket, got %s", cfg.Provider.Kind)
	}
	if cfg.Policy.MinSupplyChainScore != 70 {
		t.Errorf("expected min_supply_chain_score=70, got %f", cfg.Policy.MinSupplyChainScore)
	}
	if cfg.Policy.MinimumPackageAgeHours != 48 {
		t.Errorf("expected minimum_package_age_hours=48, got %d", cfg.Policy.MinimumPackageAgeHours)
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
	if cfg.Provider.Kind != "socket" {
		t.Errorf("expected provider=socket, got %s", cfg.Provider.Kind)
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

func TestResolveLogPath(t *testing.T) {
	cfg := DefaultConfig()
	path := cfg.ResolveLogPath()

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".attach-guard", "audit.jsonl")

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}
