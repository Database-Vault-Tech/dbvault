// Package config stores CLI settings in ~/.config/dbvault/config.json.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	Server       string `json:"server"`
	Token        string `json:"token"`
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email,omitempty"`
}

// Path returns the config file location (DBVAULT_CONFIG overrides it).
func Path() (string, error) {
	if p := os.Getenv("DBVAULT_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dbvault", "config.json"), nil
}

// Load reads the config file and applies DBVAULT_SERVER / DBVAULT_TOKEN /
// DBVAULT_ORG environment overrides (handy in CI).
func Load() (*Config, error) {
	c := &Config{}
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	}
	if v := os.Getenv("DBVAULT_SERVER"); v != "" {
		c.Server = v
	}
	if v := os.Getenv("DBVAULT_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("DBVAULT_ORG"); v != "" {
		c.Organization = v
	}
	return c, nil
}

// Save writes the config with 0600 permissions (it contains an API token).
func (c *Config) Save() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}
