// Package platform reads and verifies Attach Platform setup state.
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the local Attach Platform CLI config written by `attach setup`.
type Config struct {
	APIURL           string `json:"api_url"`
	APIKey           string `json:"api_key"`
	DefaultNamespace string `json:"default_namespace,omitempty"`
}

// DefaultConfigPath returns the Attach Platform CLI config path.
func DefaultConfigPath() string {
	if override := strings.TrimSpace(os.Getenv("ATTACH_CONFIG_PATH")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".attach", "config.json")
	}
	return filepath.Join(home, ".attach", "config.json")
}

// Load reads a local Attach Platform config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.APIURL = strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.APIURL == "" || cfg.APIKey == "" {
		return nil, fmt.Errorf("%s is missing api_url or api_key", path)
	}
	if err := validateAPIURL(cfg.APIURL); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Verify checks whether the configured Attach Platform credential is accepted.
func Verify(ctx context.Context, client *http.Client, cfg *Config) error {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.APIURL+"/v1/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "attach-guard platform-preflight")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("credential rejected by Attach Platform (%d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Attach Platform returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func validateAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("api_url is not valid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("api_url must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("api_url must include a host")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) && os.Getenv("ATTACH_ALLOW_HTTP") != "1" {
		return fmt.Errorf("api_url uses insecure HTTP for a non-local host")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
