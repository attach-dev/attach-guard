// Package config handles configuration loading and merging for attach-guard.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration.
type Config struct {
	Provider        ProviderConfig `yaml:"provider"`
	Policy          PolicyConfig   `yaml:"policy"`
	PackageManagers PMConfig       `yaml:"package_managers"`
	Logging         LoggingConfig  `yaml:"logging"`
}

// ProviderConfig configures the risk provider.
type ProviderConfig struct {
	Kind           string `yaml:"kind"`
	APITokenEnv    string `yaml:"api_token_env"`
	Endpoint       string `yaml:"endpoint,omitempty"`
	Command        string `yaml:"command,omitempty"`
	TimeoutSeconds *int   `yaml:"timeout_seconds,omitempty"`
}

// PolicyConfig holds policy thresholds and behavior.
type PolicyConfig struct {
	DenyKnownMalware       bool                      `yaml:"deny_known_malware"`
	MinSupplyChainScore    float64                   `yaml:"min_supply_chain_score"`
	MinOverallScore        float64                   `yaml:"min_overall_score"`
	GrayBandMinSupplyChain float64                   `yaml:"gray_band_min_supply_chain_score"`
	MinimumPackageAgeHours int                       `yaml:"minimum_package_age_hours"`
	ProviderUnavailable    ProviderUnavailableConfig `yaml:"provider_unavailable_behavior"`
	UnknownBehavior        ProviderUnavailableConfig `yaml:"unknown_behavior"`
	AutoRewriteUnpinned    AutoRewriteConfig         `yaml:"auto_rewrite_unpinned"`
	Allowlist              []string                  `yaml:"allowlist"`
	Denylist               []string                  `yaml:"denylist"`
}

// ProviderUnavailableConfig defines behavior when the provider is down.
type ProviderUnavailableConfig struct {
	Local string `yaml:"local"` // allow, ask, or deny
	CI    string `yaml:"ci"`    // allow, ask, or deny
}

// AutoRewriteConfig defines whether auto-rewrite is allowed.
type AutoRewriteConfig struct {
	Local bool `yaml:"local"`
	CI    bool `yaml:"ci"`
}

// PMConfig enables/disables package managers.
type PMConfig struct {
	NPM   bool `yaml:"npm"`
	PNPM  bool `yaml:"pnpm"`
	Pip   bool `yaml:"pip"`
	Go    bool `yaml:"go"`
	Cargo bool `yaml:"cargo"`
}

// LoggingConfig configures audit logging.
type LoggingConfig struct {
	Path string `yaml:"path"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Provider: ProviderConfig{
			Kind:        "open-score",
			Command:     "attach-open-score",
			APITokenEnv: "ATTACH_OPEN_SCORE_API_TOKEN",
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
			UnknownBehavior: ProviderUnavailableConfig{
				Local: "ask",
				CI:    "deny",
			},
			AutoRewriteUnpinned: AutoRewriteConfig{
				Local: false,
				CI:    false,
			},
		},
		PackageManagers: PMConfig{
			NPM:   true,
			PNPM:  true,
			Pip:   true,
			Go:    true,
			Cargo: true,
		},
		Logging: LoggingConfig{
			Path: "~/.attach-guard/audit.jsonl",
		},
	}
}

// Load loads configuration from the default locations and merges them.
// Config is loaded in order (later overrides earlier):
//  1. Built-in defaults
//  2. Plugin-bundled config (if ATTACH_GUARD_PLUGIN_CONFIG_DIR is set)
//  3. User-global config (~/.attach-guard/config.yaml)
//  4. Project-local config (.attach-guard/config.yaml)
//  5. Environment variable overrides
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Plugin-bundled config (set by bootstrap.sh in plugin mode)
	if pluginDir := os.Getenv("ATTACH_GUARD_PLUGIN_CONFIG_DIR"); pluginDir != "" {
		pluginPath := filepath.Join(pluginDir, "config.yaml")
		if err := mergeFromFile(cfg, pluginPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading plugin config %s: %w", pluginPath, err)
		}
	}

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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadFromFile loads configuration from a specific file path.
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()
	if err := mergeFromFile(cfg, path); err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks configuration values that need cross-field validation.
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}
	if c.Provider.Kind != "open-score" && c.Provider.Kind != "platform" {
		return nil
	}

	endpoint := strings.TrimSpace(c.Provider.Endpoint)
	// The hosted platform provider always needs an endpoint; open-score may fall
	// back to the local command when no endpoint is configured.
	if c.Provider.Kind == "platform" && endpoint == "" {
		return fmt.Errorf("provider.endpoint is required when provider.kind is platform")
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("provider.endpoint must be a valid URL when provider.kind is %s: %w", c.Provider.Kind, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("provider.endpoint must use http or https when provider.kind is %s", c.Provider.Kind)
		}
		if u.Host == "" {
			return fmt.Errorf("provider.endpoint must include a host when provider.kind is %s", c.Provider.Kind)
		}
	}

	if c.Provider.TimeoutSeconds != nil && *c.Provider.TimeoutSeconds <= 0 {
		return fmt.Errorf("provider.timeout_seconds must be positive when provider.kind is %s", c.Provider.Kind)
	}

	return nil
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
	if v := os.Getenv("ATTACH_GUARD_LOG_PATH"); v != "" {
		cfg.Logging.Path = v
	}
	if v := os.Getenv("ATTACH_GUARD_PROVIDER"); v != "" {
		cfg.Provider.Kind = v
	}
	if v := os.Getenv("ATTACH_OPEN_SCORE_ENDPOINT"); v != "" {
		cfg.Provider.Kind = "open-score"
		cfg.Provider.Endpoint = v
		cfg.Provider.APITokenEnv = "ATTACH_OPEN_SCORE_API_TOKEN"
	}
	if v := os.Getenv("ATTACH_OPEN_SCORE_BIN"); v != "" {
		cfg.Provider.Kind = "open-score"
		cfg.Provider.Command = v
	}
}

// WriteDefault writes the default config to the given path.
func WriteDefault(path string) error {
	return Write(path, DefaultConfig())
}

// Write marshals cfg to YAML and writes it to path (0600, parent dir 0700).
func Write(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// PlatformConfig returns a config that uses the hosted Attach Platform score
// edge with a per-user API key read from apiTokenEnv.
func PlatformConfig(endpoint, apiTokenEnv string) *Config {
	cfg := DefaultConfig()
	cfg.Provider = ProviderConfig{
		Kind:        "platform",
		Endpoint:    endpoint,
		APITokenEnv: apiTokenEnv,
	}
	return cfg
}
