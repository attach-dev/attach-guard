// Package config handles configuration loading and merging for attach-guard.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration.
type Config struct {
	Mode            string          `yaml:"mode"`
	Provider        ProviderConfig  `yaml:"provider"`
	Policy          PolicyConfig    `yaml:"policy"`
	PackageManagers PMConfig        `yaml:"package_managers"`
	Logging         LoggingConfig   `yaml:"logging"`
}

// ProviderConfig configures the risk provider.
type ProviderConfig struct {
	Kind        string `yaml:"kind"`
	APITokenEnv string `yaml:"api_token_env"`
}

// PolicyConfig holds policy thresholds and behavior.
type PolicyConfig struct {
	DenyKnownMalware         bool                      `yaml:"deny_known_malware"`
	MinSupplyChainScore      float64                   `yaml:"min_supply_chain_score"`
	MinOverallScore          float64                   `yaml:"min_overall_score"`
	GrayBandMinSupplyChain   float64                   `yaml:"gray_band_min_supply_chain_score"`
	MinimumPackageAgeHours   int                       `yaml:"minimum_package_age_hours"`
	ProviderUnavailable      ProviderUnavailableConfig `yaml:"provider_unavailable_behavior"`
	AutoRewriteUnpinned      AutoRewriteConfig         `yaml:"auto_rewrite_unpinned"`
	Allowlist                []string                  `yaml:"allowlist"`
	Denylist                 []string                  `yaml:"denylist"`
}

// ProviderUnavailableConfig defines behavior when the provider is down.
type ProviderUnavailableConfig struct {
	Local string `yaml:"local"` // ask or deny
	CI    string `yaml:"ci"`    // ask or deny
}

// AutoRewriteConfig defines whether auto-rewrite is allowed.
type AutoRewriteConfig struct {
	Local bool `yaml:"local"`
	CI    bool `yaml:"ci"`
}

// PMConfig enables/disables package managers.
type PMConfig struct {
	NPM  bool `yaml:"npm"`
	PNPM bool `yaml:"pnpm"`
}

// LoggingConfig configures audit logging.
type LoggingConfig struct {
	Path string `yaml:"path"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Mode: "ask",
		Provider: ProviderConfig{
			Kind:        "socket",
			APITokenEnv: "SOCKET_API_TOKEN",
		},
		Policy: PolicyConfig{
			DenyKnownMalware:       true,
			MinSupplyChainScore:    70,
			MinOverallScore:        70,
			GrayBandMinSupplyChain: 50,
			MinimumPackageAgeHours: 48,
			ProviderUnavailable: ProviderUnavailableConfig{
				Local: "ask",
				CI:    "deny",
			},
			AutoRewriteUnpinned: AutoRewriteConfig{
				Local: false,
				CI:    false,
			},
		},
		PackageManagers: PMConfig{
			NPM:  true,
			PNPM: true,
		},
		Logging: LoggingConfig{
			Path: "~/.attach-guard/audit.jsonl",
		},
	}
}

// Load loads configuration from the default locations and merges them.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, nil
	}

	// User-global config
	globalPath := filepath.Join(home, ".attach-guard", "config.yaml")
	if err := mergeFromFile(cfg, globalPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading global config %s: %w", globalPath, err)
	}

	// Project-local config
	localPath := filepath.Join(".attach-guard", "config.yaml")
	if err := mergeFromFile(cfg, localPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading local config %s: %w", localPath, err)
	}

	// Environment variable overrides
	applyEnvOverrides(cfg)

	return cfg, nil
}

// LoadFromFile loads configuration from a specific file path.
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()
	if err := mergeFromFile(cfg, path); err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	return cfg, nil
}

// ResolveLogPath expands ~ in the log path.
func (c *Config) ResolveLogPath() string {
	p := c.Logging.Path
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	return p
}

func mergeFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("ATTACH_GUARD_MODE"); v != "" {
		cfg.Mode = v
	}
	if v := os.Getenv("ATTACH_GUARD_LOG_PATH"); v != "" {
		cfg.Logging.Path = v
	}
	if v := os.Getenv("ATTACH_GUARD_PROVIDER"); v != "" {
		cfg.Provider.Kind = v
	}
}

// WriteDefault writes the default config to the given path.
func WriteDefault(path string) error {
	cfg := DefaultConfig()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
