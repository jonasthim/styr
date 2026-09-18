package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonasthim/styr/internal/config"
)

// writeFakeClaude writes an executable stub named "claude" into t.TempDir()
// that answers --version with exit 0, and returns its path.
func writeFakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/usr/bin/env bash\necho 'fake-claude 2.1.276'\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func baseTestConfig(t *testing.T, claudeBin string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Env = "dev"
	cfg.DataDir = t.TempDir()
	cfg.SecretKey = "this-is-a-32-plus-byte-secret-key!!"
	cfg.ClaudeBin = claudeBin
	return cfg
}

func findCheck(t *testing.T, checks []check, name string) check {
	t.Helper()
	for _, c := range checks {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no check named %q", name)
	return check{}
}

func TestDoctorChecks_ClaudeBinaryFound(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "claude binary found and --version runs")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_ClaudeBinaryMissing(t *testing.T) {
	cfg := baseTestConfig(t, filepath.Join(t.TempDir(), "does-not-exist"))
	c := findCheck(t, doctorChecks(cfg), "claude binary found and --version runs")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for missing binary")
	}
}

func TestDoctorChecks_DataDirWritable(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "data dir writable")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_DataDirNotWritable(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.DataDir = filepath.Join(cfg.DataDir, "nested", "deep")
	// Make the parent read-only so MkdirAll on the nested dir fails.
	parent := filepath.Dir(filepath.Dir(cfg.DataDir))
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
	c := findCheck(t, doctorChecks(cfg), "data dir writable")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for unwritable data dir")
	}
}

func TestDoctorChecks_GitOnPath(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "git on PATH")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil (is git installed in this environment?)", err)
	}
}

func TestDoctorChecks_SecretKeyLength(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.SecretKey = "too-short"
	c := findCheck(t, doctorChecks(cfg), "secret key length")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for short secret key")
	}

	cfg.SecretKey = "this-is-a-32-plus-byte-secret-key!!"
	c = findCheck(t, doctorChecks(cfg), "secret key length")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_DatabaseOpens(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	c := findCheck(t, doctorChecks(cfg), "database opens")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_OIDCSkippedInDev(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.Env = "dev"
	c := findCheck(t, doctorChecks(cfg), "oidc discovery reachable")
	err := c.run()
	if err == nil {
		t.Fatal("run() = nil, want a skip sentinel")
	}
	if !errors.Is(err, errSkip) {
		t.Fatalf("run() = %v, want errSkip", err)
	}
}

func TestDoctorChecks_OIDCReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"issuer":"` + r.Host + `"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.Env = "prod"
	cfg.BaseURL = "https://styr.example.com"
	cfg.OIDC = []config.OIDCProvider{{Name: "Test", Issuer: ts.URL, ClientID: "abc"}}

	c := findCheck(t, doctorChecks(cfg), "oidc discovery reachable")
	if err := c.run(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
}

func TestDoctorChecks_OIDCUnreachable(t *testing.T) {
	cfg := baseTestConfig(t, writeFakeClaude(t))
	cfg.Env = "prod"
	cfg.BaseURL = "https://styr.example.com"
	cfg.OIDC = []config.OIDCProvider{{Name: "Test", Issuer: "http://127.0.0.1:1", ClientID: "abc"}}

	c := findCheck(t, doctorChecks(cfg), "oidc discovery reachable")
	if err := c.run(); err == nil {
		t.Fatal("run() = nil, want error for unreachable issuer")
	}
}

func TestRunDoctor_ExitsOneOnFailure(t *testing.T) {
	t.Setenv("STYR_CONFIG", "")
	t.Setenv("STYR_ENV", "dev")
	t.Setenv("STYR_DATA_DIR", t.TempDir())
	t.Setenv("STYR_SECRET_KEY", "too-short")
	t.Setenv("STYR_CLAUDE_BIN", writeFakeClaude(t))

	var out fakeWriter
	if code := runDoctor(&out); code != 1 {
		t.Fatalf("runDoctor() = %d, want 1; output:\n%s", code, out.String())
	}
	if !containsLine(out.String(), "FAIL secret key length") {
		t.Fatalf("output missing FAIL line:\n%s", out.String())
	}
}

func TestRunDoctor_ExitsZeroWhenHealthy(t *testing.T) {
	t.Setenv("STYR_CONFIG", "")
	t.Setenv("STYR_ENV", "dev")
	t.Setenv("STYR_DATA_DIR", t.TempDir())
	t.Setenv("STYR_SECRET_KEY", "this-is-a-32-plus-byte-secret-key!!")
	t.Setenv("STYR_CLAUDE_BIN", writeFakeClaude(t))

	var out fakeWriter
	if code := runDoctor(&out); code != 0 {
		t.Fatalf("runDoctor() = %d, want 0; output:\n%s", code, out.String())
	}
	if !containsLine(out.String(), "skip oidc discovery reachable") {
		t.Fatalf("output missing skip line:\n%s", out.String())
	}
}

// fakeWriter is a minimal io.Writer collecting output for assertions,
// avoiding a bytes.Buffer import just for this.
type fakeWriter struct{ buf []byte }

func (w *fakeWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}
func (w *fakeWriter) String() string { return string(w.buf) }

func containsLine(s, prefix string) bool {
	for _, line := range splitLines(s) {
		if len(line) >= len(prefix) && line[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
