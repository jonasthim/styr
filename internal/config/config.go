// Package config loads the styr server configuration from a YAML file with
// STYR_* environment overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// OIDCProvider configures one OpenID Connect login option shown on the login
// page.
type OIDCProvider struct {
	Name         string   `yaml:"name"` // shown on the login button, e.g. "Authentik"
	Issuer       string   `yaml:"issuer"`
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"` // optional for public clients
	Scopes       []string `yaml:"scopes"`        // default ["openid","profile","email"]
}

// Config is the styr server configuration.
type Config struct {
	Env             string         `yaml:"env"`               // "prod" (default) | "dev"
	Listen          string         `yaml:"listen"`            // default "127.0.0.1:8080"
	BaseURL         string         `yaml:"base_url"`          // public URL, required in prod (OIDC redirect)
	DataDir         string         `yaml:"data_dir"`          // default /var/lib/styr (prod); ./data (dev)
	SecretKey       string         `yaml:"secret_key"`        // 32+ bytes, base64 or raw; encrypts tokens; required
	ClaudeBin       string         `yaml:"claude_bin"`        // default "claude"
	CodexBin        string         `yaml:"codex_bin"`         // default "codex"
	MaxOpenSessions int            `yaml:"max_open_sessions"` // default 4
	IdleTimeout     time.Duration  `yaml:"idle_timeout"`      // default 15m
	ApprovalTimeout time.Duration  `yaml:"approval_timeout"`  // default 30m, for unattended
	OIDC            []OIDCProvider `yaml:"oidc"`
	DevUser         string         `yaml:"dev_user"` // only honoured when Env=="dev"
}

var defaultOIDCScopes = []string{"openid", "profile", "email"}

// Default returns the configuration with all defaults applied and nothing
// loaded from a file or the environment.
func Default() Config {
	return Config{
		Env:             "prod",
		Listen:          "127.0.0.1:8080",
		ClaudeBin:       "claude",
		CodexBin:        "codex",
		MaxOpenSessions: 4,
		IdleTimeout:     15 * time.Minute,
		ApprovalTimeout: 30 * time.Minute,
	}
}

// Load builds a Config from Default, then the YAML file at path (a missing
// file is not an error), then STYR_* environment overrides, and validates
// the result.
func Load(path string) (Config, error) {
	c := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(b, &c); err != nil {
				return c, fmt.Errorf("parse %s: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist):
			// fall through to defaults + env
		default:
			return c, fmt.Errorf("read %s: %w", path, err)
		}
	}
	applyEnv(&c)

	if c.DataDir == "" {
		if c.Env == "dev" {
			c.DataDir = "./data"
		} else {
			c.DataDir = "/var/lib/styr"
		}
	}
	for i := range c.OIDC {
		if len(c.OIDC[i].Scopes) == 0 {
			c.OIDC[i].Scopes = append([]string(nil), defaultOIDCScopes...)
		}
	}

	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func applyEnv(c *Config) {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv("STYR_" + key); ok {
			*dst = v
		}
	}
	integer := func(key string, dst *int) {
		if v, ok := os.LookupEnv("STYR_" + key); ok {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	duration := func(key string, dst *time.Duration) {
		if v, ok := os.LookupEnv("STYR_" + key); ok {
			if d, err := time.ParseDuration(v); err == nil {
				*dst = d
			}
		}
	}
	str("ENV", &c.Env)
	str("LISTEN", &c.Listen)
	str("BASE_URL", &c.BaseURL)
	str("DATA_DIR", &c.DataDir)
	str("SECRET_KEY", &c.SecretKey)
	str("CLAUDE_BIN", &c.ClaudeBin)
	str("CODEX_BIN", &c.CodexBin)
	integer("MAX_OPEN_SESSIONS", &c.MaxOpenSessions)
	duration("IDLE_TIMEOUT", &c.IdleTimeout)
	duration("APPROVAL_TIMEOUT", &c.ApprovalTimeout)
	str("DEV_USER", &c.DevUser)

	if v, ok := os.LookupEnv("STYR_OIDC_CLIENT_SECRET"); ok && len(c.OIDC) > 0 {
		c.OIDC[0].ClientSecret = v
	}
}

// Validate returns an error when c is missing a required field or contains
// an inconsistent combination of fields.
func (c Config) Validate() error {
	if c.Env != "prod" && c.Env != "dev" {
		return fmt.Errorf("config: env must be %q or %q, got %q", "prod", "dev", c.Env)
	}
	if c.Listen == "" {
		return errors.New("config: listen is required")
	}
	if c.DataDir == "" {
		return errors.New("config: data_dir is required")
	}
	if len(c.SecretKey) < 32 {
		return errors.New("config: secret_key must be at least 32 bytes")
	}
	if c.MaxOpenSessions < 1 {
		return errors.New("config: max_open_sessions must be >= 1")
	}
	if c.Env == "prod" {
		if c.BaseURL == "" {
			return errors.New("config: base_url is required in prod")
		}
		if len(c.OIDC) == 0 {
			return errors.New("config: at least one oidc provider is required in prod")
		}
	}
	return nil
}

// DBPath returns the path to the sqlite database file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "styr.db") }

// UsersDir returns the path to the per-user data directory.
func (c Config) UsersDir() string { return filepath.Join(c.DataDir, "users") }

// WorkspacesDir returns the path to the workspaces directory.
func (c Config) WorkspacesDir() string { return filepath.Join(c.DataDir, "workspaces") }
