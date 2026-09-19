package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useEnvFileForSecret moves STYR_SECRET_KEY out of the process environment
// and into an env file pointed at by STYR_ENV_FILE, the way a hand-run
// subcommand on an installed host finds its secrets (systemd passes
// /etc/styr/env to serve; nothing passes it to a shell). It returns the
// env file's path.
func useEnvFileForSecret(t *testing.T) string {
	t.Helper()
	envFile := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(envFile, []byte("STYR_SECRET_KEY="+testSecretKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STYR_ENV_FILE", envFile)
	t.Setenv("STYR_SECRET_KEY", testSecretKey) // registers the restore
	os.Unsetenv("STYR_SECRET_KEY")
	return envFile
}

func TestBackup_LoadsEnvFile(t *testing.T) {
	dataDir := t.TempDir()
	setBackupTestEnv(t, dataDir)
	newMigratedDB(t, dataDir)
	envFile := useEnvFileForSecret(t)

	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	var stdout bytes.Buffer
	if code := runBackup(&stdout, []string{out}); code != 0 {
		t.Fatalf("runBackup() = %d, want 0; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "note loaded 1 variable(s) from "+envFile) {
		t.Fatalf("output lacks the env-file note:\n%s", stdout.String())
	}
}

func TestRestore_LoadsEnvFile(t *testing.T) {
	dataDir := t.TempDir()
	setBackupTestEnv(t, dataDir)
	newMigratedDB(t, dataDir)

	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	var out bytes.Buffer
	if code := runBackup(&out, []string{archive}); code != 0 {
		t.Fatalf("runBackup() = %d; output:\n%s", code, out.String())
	}

	useEnvFileForSecret(t)
	out.Reset()
	if code := runRestore(&out, []string{archive}); code != 0 {
		t.Fatalf("runRestore() = %d, want 0; output:\n%s", code, out.String())
	}
}

func TestMigrate_LoadsEnvFile(t *testing.T) {
	dataDir := t.TempDir()
	setBackupTestEnv(t, dataDir)
	useEnvFileForSecret(t)

	var out bytes.Buffer
	if code := runMigrate(&out); code != 0 {
		t.Fatalf("runMigrate() = %d, want 0; output:\n%s", code, out.String())
	}
}

func TestLoadEnv_WarnsWhenUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	envFile := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(envFile, []byte("STYR_SECRET_KEY=x\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STYR_ENV_FILE", envFile)
	var out bytes.Buffer
	loadEnv(&out)
	if !strings.Contains(out.String(), "warn "+envFile+" exists but is not readable") {
		t.Fatalf("output = %q, want unreadable warning", out.String())
	}
}
