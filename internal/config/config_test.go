package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Mode != "ask" {
		t.Errorf("expected mode=ask, got %s", cfg.Mode)
	}
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

	if cfg.Mode != "ask" {
		t.Errorf("expected mode=ask after load, got %s", cfg.Mode)
	}
	if cfg.Policy.DenyKnownMalware != true {
		t.Error("expected deny_known_malware=true")
	}
}

func TestEnvOverrides(t *testing.T) {
	os.Setenv("ATTACH_GUARD_MODE", "enforce")
	defer os.Unsetenv("ATTACH_GUARD_MODE")

	cfg := DefaultConfig()
	applyEnvOverrides(cfg)

	if cfg.Mode != "enforce" {
		t.Errorf("expected mode=enforce from env, got %s", cfg.Mode)
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
