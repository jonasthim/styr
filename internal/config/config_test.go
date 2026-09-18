package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultAppliesExpectedValues(t *testing.T) {
	c := Default()
	if c.Env != "prod" {
		t.Errorf("Env = %q, want %q", c.Env, "prod")
	}
	if c.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q, want %q", c.Listen, "127.0.0.1:8080")
	}
	if c.ClaudeBin != "claude" {
		t.Errorf("ClaudeBin = %q, want %q", c.ClaudeBin, "claude")
	}
	if c.MaxOpenSessions != 4 {
		t.Errorf("MaxOpenSessions = %d, want 4", c.MaxOpenSessions)
	}
	if c.IdleTimeout != 15*time.Minute {
		t.Errorf("IdleTimeout = %v, want 15m", c.IdleTimeout)
	}
	if c.ApprovalTimeout != 30*time.Minute {
		t.Errorf("ApprovalTimeout = %v, want 30m", c.ApprovalTimeout)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("STYR_SECRET_KEY", "a-secret-key-that-is-32-bytes-ok")
	t.Setenv("STYR_ENV", "dev")
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if c.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q, want default", c.Listen)
	}
}

func TestLoadDecodesYAMLFile(t *testing.T) {
	path := writeFile(t, `
env: dev
listen: 0.0.0.0:9090
secret_key: a-secret-key-that-is-32-bytes-ok
dev_user: dev@example.com
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Listen != "0.0.0.0:9090" {
		t.Errorf("Listen = %q, want 0.0.0.0:9090", c.Listen)
	}
	if c.DevUser != "dev@example.com" {
		t.Errorf("DevUser = %q, want dev@example.com", c.DevUser)
	}
}

func TestLoadDataDirDefaultDependsOnEnv(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"prod", "/var/lib/styr"},
		{"dev", "./data"},
	}
	for _, tc := range cases {
		path := writeFile(t, "env: "+tc.env+"\nsecret_key: a-secret-key-that-is-32-bytes-ok\nbase_url: https://styr.example.com\noidc:\n  - name: Authentik\n    issuer: https://auth.example.com\n    client_id: cid\n")
		c, err := Load(path)
		if err != nil {
			t.Fatalf("env=%s: Load() error = %v", tc.env, err)
		}
		if c.DataDir != tc.want {
			t.Errorf("env=%s: DataDir = %q, want %q", tc.env, c.DataDir, tc.want)
		}
	}
}

func TestLoadDataDirExplicitOverridesDefault(t *testing.T) {
	path := writeFile(t, "env: dev\nsecret_key: a-secret-key-that-is-32-bytes-ok\ndata_dir: /custom/dir\n")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.DataDir != "/custom/dir" {
		t.Errorf("DataDir = %q, want /custom/dir", c.DataDir)
	}
}

func TestEnvOverridesListenAndMaxOpenSessions(t *testing.T) {
	t.Setenv("STYR_LISTEN", "0.0.0.0:1234")
	t.Setenv("STYR_MAX_OPEN_SESSIONS", "9")
	t.Setenv("STYR_SECRET_KEY", "a-secret-key-that-is-32-bytes-ok")
	t.Setenv("STYR_ENV", "dev")
	c, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Listen != "0.0.0.0:1234" {
		t.Errorf("Listen = %q, want 0.0.0.0:1234", c.Listen)
	}
	if c.MaxOpenSessions != 9 {
		t.Errorf("MaxOpenSessions = %d, want 9", c.MaxOpenSessions)
	}
}

func TestEnvOverridesDurationsAndOther(t *testing.T) {
	t.Setenv("STYR_ENV", "dev")
	t.Setenv("STYR_BASE_URL", "https://example.com")
	t.Setenv("STYR_DATA_DIR", "/tmp/styr-data")
	t.Setenv("STYR_SECRET_KEY", "a-secret-key-that-is-32-bytes-ok")
	t.Setenv("STYR_CLAUDE_BIN", "/usr/bin/claude")
	t.Setenv("STYR_IDLE_TIMEOUT", "5m")
	t.Setenv("STYR_APPROVAL_TIMEOUT", "1h")
	t.Setenv("STYR_DEV_USER", "someone@example.com")
	c, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.DataDir != "/tmp/styr-data" {
		t.Errorf("DataDir = %q", c.DataDir)
	}
	if c.SecretKey != "a-secret-key-that-is-32-bytes-ok" {
		t.Errorf("SecretKey = %q", c.SecretKey)
	}
	if c.ClaudeBin != "/usr/bin/claude" {
		t.Errorf("ClaudeBin = %q", c.ClaudeBin)
	}
	if c.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want 5m", c.IdleTimeout)
	}
	if c.ApprovalTimeout != time.Hour {
		t.Errorf("ApprovalTimeout = %v, want 1h", c.ApprovalTimeout)
	}
	if c.DevUser != "someone@example.com" {
		t.Errorf("DevUser = %q", c.DevUser)
	}
}

func TestEnvOIDCClientSecretFillsProviderZero(t *testing.T) {
	path := writeFile(t, `
env: prod
base_url: https://styr.example.com
secret_key: a-secret-key-that-is-32-bytes-ok
oidc:
  - name: Authentik
    issuer: https://auth.example.com
    client_id: cid
`)
	t.Setenv("STYR_OIDC_CLIENT_SECRET", "the-secret")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(c.OIDC) != 1 || c.OIDC[0].ClientSecret != "the-secret" {
		t.Fatalf("OIDC = %+v, want provider 0 secret set", c.OIDC)
	}
}

func TestValidateFailsWhenSecretKeyTooShort(t *testing.T) {
	c := Default()
	c.Env = "dev"
	c.DevUser = "dev@example.com"
	c.DataDir = "./data"
	c.SecretKey = "too-short"
	if err := c.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for short secret key")
	}
}

func TestValidateFailsInProdWithoutBaseURL(t *testing.T) {
	c := Default()
	c.Env = "prod"
	c.DataDir = "/var/lib/styr"
	c.SecretKey = "a-secret-key-that-is-32-bytes-ok"
	c.OIDC = []OIDCProvider{{Name: "Authentik", Issuer: "https://auth.example.com", ClientID: "cid"}}
	if err := c.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for missing base_url in prod")
	}
}

func TestValidateFailsInProdWithoutOIDCProvider(t *testing.T) {
	c := Default()
	c.Env = "prod"
	c.DataDir = "/var/lib/styr"
	c.SecretKey = "a-secret-key-that-is-32-bytes-ok"
	c.BaseURL = "https://styr.example.com"
	if err := c.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for missing oidc provider in prod")
	}
}

func TestValidatePassesInProdWithBaseURLAndOIDC(t *testing.T) {
	c := Default()
	c.Env = "prod"
	c.DataDir = "/var/lib/styr"
	c.SecretKey = "a-secret-key-that-is-32-bytes-ok"
	c.BaseURL = "https://styr.example.com"
	c.OIDC = []OIDCProvider{{Name: "Authentik", Issuer: "https://auth.example.com", ClientID: "cid"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidatePassesInDevWithDevUserAndNoOIDC(t *testing.T) {
	c := Default()
	c.Env = "dev"
	c.DataDir = "./data"
	c.SecretKey = "a-secret-key-that-is-32-bytes-ok"
	c.DevUser = "dev@example.com"
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateRequiresSecretKey(t *testing.T) {
	c := Default()
	c.Env = "dev"
	c.DataDir = "./data"
	c.DevUser = "dev@example.com"
	if err := c.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error for missing secret key")
	}
}

func TestPathHelpers(t *testing.T) {
	c := Default()
	c.DataDir = "/var/lib/styr"
	if got, want := c.DBPath(), filepath.Join("/var/lib/styr", "styr.db"); got != want {
		t.Errorf("DBPath() = %q, want %q", got, want)
	}
	if got, want := c.UsersDir(), filepath.Join("/var/lib/styr", "users"); got != want {
		t.Errorf("UsersDir() = %q, want %q", got, want)
	}
	if got, want := c.WorkspacesDir(), filepath.Join("/var/lib/styr", "workspaces"); got != want {
		t.Errorf("WorkspacesDir() = %q, want %q", got, want)
	}
}

func TestOIDCProviderDefaultScopes(t *testing.T) {
	path := writeFile(t, `
env: prod
base_url: https://styr.example.com
secret_key: a-secret-key-that-is-32-bytes-ok
oidc:
  - name: Authentik
    issuer: https://auth.example.com
    client_id: cid
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{"openid", "profile", "email"}
	if len(c.OIDC) != 1 || len(c.OIDC[0].Scopes) != len(want) {
		t.Fatalf("Scopes = %+v, want %v", c.OIDC[0].Scopes, want)
	}
	for i, s := range want {
		if c.OIDC[0].Scopes[i] != s {
			t.Errorf("Scopes[%d] = %q, want %q", i, c.OIDC[0].Scopes[i], s)
		}
	}
}
