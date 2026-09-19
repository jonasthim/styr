package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
)

// testSecretKey is a fixed 36-byte key, long enough to satisfy
// config.Config.Validate, used across backup/restore tests.
const testSecretKey = "this-is-a-32-plus-byte-secret-key!!"

// setBackupTestEnv points STYR_DATA_DIR/STYR_SECRET_KEY/STYR_ENV at a fresh
// test config for the duration of t, with STYR_CONFIG unset (no config.yaml
// to back up) unless the caller sets it afterwards.
func setBackupTestEnv(t *testing.T, dataDir string) {
	t.Helper()
	t.Setenv("STYR_CONFIG", "")
	t.Setenv("STYR_ENV", "dev")
	t.Setenv("STYR_DATA_DIR", dataDir)
	t.Setenv("STYR_SECRET_KEY", testSecretKey)
}

// newMigratedDB creates and migrates a fresh database at
// <dataDir>/styr.db, closing it before returning so backup/restore's own
// db.Open calls do not contend with it.
func newMigratedDB(t *testing.T, dataDir string) {
	t.Helper()
	d, err := db.Open(filepath.Join(dataDir, "styr.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

// readTarGz reads every regular file entry of a gzip'd tar into a map keyed
// by entry name.
func readTarGz(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()

	out := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read entry %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = data
	}
	return out
}

func TestBackup_TarHasThreeEntriesAndNoSecrets(t *testing.T) {
	dataDir := t.TempDir()
	newMigratedDB(t, dataDir)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	const secretValue = "THIS-IS-A-SUPER-SECRET-VALUE-DO-NOT-SHIP"
	const oidcSecretValue = "OIDC-CLIENT-SECRET-DO-NOT-SHIP"
	const devUserValue = "someone@example.com"
	configYAML := "env: dev\n" +
		"listen: 127.0.0.1:8080\n" +
		"secret_key: " + secretValue + "\n" +
		"dev_user: " + devUserValue + "\n" +
		"oidc:\n" +
		"  - name: Test\n" +
		"    issuer: https://issuer.example.com\n" +
		"    client_id: abc123\n" +
		"    client_secret: " + oidcSecretValue + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	setBackupTestEnv(t, dataDir)
	t.Setenv("STYR_CONFIG", configPath)

	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	var stdout bytes.Buffer
	if code := runBackup(&stdout, []string{out}); code != 0 {
		t.Fatalf("runBackup() = %d, want 0; output:\n%s", code, stdout.String())
	}

	entries := readTarGz(t, out)
	for _, name := range []string{backupEntryDB, backupEntryConfig, backupEntryManifest} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("archive missing entry %q; got %v", name, keysOf(entries))
		}
	}
	if len(entries) != 3 {
		t.Fatalf("archive has %d entries, want 3: %v", len(entries), keysOf(entries))
	}

	configCopy := string(entries[backupEntryConfig])
	for _, secret := range []string{secretValue, oidcSecretValue, devUserValue, "secret_key:", "dev_user:", "client_secret:"} {
		if strings.Contains(configCopy, secret) {
			t.Fatalf("config.yaml copy leaked %q:\n%s", secret, configCopy)
		}
	}
	if !strings.Contains(configCopy, "listen: 127.0.0.1:8080") {
		t.Fatalf("config.yaml copy lost non-secret content:\n%s", configCopy)
	}
	if !strings.Contains(configCopy, "issuer: https://issuer.example.com") {
		t.Fatalf("config.yaml copy lost oidc provider fields:\n%s", configCopy)
	}

	var manifest backupManifest
	if err := json.Unmarshal(entries[backupEntryManifest], &manifest); err != nil {
		t.Fatalf("parse manifest.json: %v", err)
	}
	if manifest.SchemaVersion <= 0 {
		t.Fatalf("manifest.SchemaVersion = %d, want > 0", manifest.SchemaVersion)
	}
	if manifest.Hostname == "" {
		t.Fatal("manifest.Hostname is empty")
	}
	if manifest.CreatedAt == "" {
		t.Fatal("manifest.CreatedAt is empty")
	}

	if len(entries[backupEntryDB]) == 0 {
		t.Fatal("styr.db entry is empty")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBackup_MissingConfigEnvOmitsConfigEntry(t *testing.T) {
	dataDir := t.TempDir()
	newMigratedDB(t, dataDir)
	setBackupTestEnv(t, dataDir) // STYR_CONFIG left unset by the helper

	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	var stdout bytes.Buffer
	if code := runBackup(&stdout, []string{out}); code != 0 {
		t.Fatalf("runBackup() = %d, want 0; output:\n%s", code, stdout.String())
	}
	entries := readTarGz(t, out)
	if _, ok := entries[backupEntryConfig]; ok {
		t.Fatal("archive has a config.yaml entry with STYR_CONFIG unset")
	}
	if _, ok := entries[backupEntryDB]; !ok {
		t.Fatal("archive missing styr.db entry")
	}
	if _, ok := entries[backupEntryManifest]; !ok {
		t.Fatal("archive missing manifest.json entry")
	}
}

func TestBackup_MissingDatabaseFails(t *testing.T) {
	dataDir := t.TempDir() // no styr.db created
	setBackupTestEnv(t, dataDir)

	out := filepath.Join(t.TempDir(), "backup.tar.gz")
	var stdout bytes.Buffer
	if code := runBackup(&stdout, []string{out}); code == 0 {
		t.Fatalf("runBackup() = 0, want non-zero for a missing database; output:\n%s", stdout.String())
	}
}

func TestBackupRestore_RoundTripRowCounts(t *testing.T) {
	srcDir := t.TempDir()
	d, err := db.Open(filepath.Join(srcDir, "styr.db"))
	if err != nil {
		t.Fatalf("open source database: %v", err)
	}
	users := db.NewUsers(d)
	ctx := context.Background()
	const wantUsers = 3
	for i := 0; i < wantUsers; i++ {
		u := domain.User{
			ID:      uuid.NewString(),
			Issuer:  "https://issuer.example.com",
			Subject: uuid.NewString(),
			Email:   uuid.NewString() + "@example.com",
			Role:    domain.RoleMember,
		}
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	wantCount, err := users.Count(ctx)
	if err != nil {
		t.Fatalf("count source users: %v", err)
	}
	if wantCount != wantUsers {
		t.Fatalf("count = %d, want %d", wantCount, wantUsers)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close source database: %v", err)
	}

	setBackupTestEnv(t, srcDir)
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	var backupOut bytes.Buffer
	if code := runBackup(&backupOut, []string{archive}); code != 0 {
		t.Fatalf("runBackup() = %d, want 0; output:\n%s", code, backupOut.String())
	}

	dstDir := t.TempDir()
	setBackupTestEnv(t, dstDir)
	var restoreOut bytes.Buffer
	if code := runRestore(&restoreOut, []string{archive}); code != 0 {
		t.Fatalf("runRestore() = %d, want 0; output:\n%s", code, restoreOut.String())
	}

	restored, err := db.Open(filepath.Join(dstDir, "styr.db"))
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer restored.Close()
	gotCount, err := db.NewUsers(restored).Count(ctx)
	if err != nil {
		t.Fatalf("count restored users: %v", err)
	}
	if gotCount != wantCount {
		t.Fatalf("restored user count = %d, want %d", gotCount, wantCount)
	}
}

func TestRestore_RefusesWithLivePIDFile(t *testing.T) {
	// A backup archive to attempt to restore; its content is never read
	// because the pid-file check runs before the archive is opened, so a
	// bogus path is enough here.
	archive := filepath.Join(t.TempDir(), "does-not-matter.tar.gz")

	dataDir := t.TempDir()
	setBackupTestEnv(t, dataDir)
	if err := writePIDFile(pidFilePath(dataDir)); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	var stdout bytes.Buffer
	if code := runRestore(&stdout, []string{archive}); code == 0 {
		t.Fatalf("runRestore() = 0, want non-zero while the pid file is live; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "running") {
		t.Fatalf("output does not mention the running server:\n%s", stdout.String())
	}
}

func TestRestore_ForceOverridesLivePIDFile(t *testing.T) {
	srcDir := t.TempDir()
	newMigratedDB(t, srcDir)
	setBackupTestEnv(t, srcDir)
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	var backupOut bytes.Buffer
	if code := runBackup(&backupOut, []string{archive}); code != 0 {
		t.Fatalf("runBackup() = %d, want 0; output:\n%s", code, backupOut.String())
	}

	dstDir := t.TempDir()
	setBackupTestEnv(t, dstDir)
	if err := writePIDFile(pidFilePath(dstDir)); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	var stdout bytes.Buffer
	if code := runRestore(&stdout, []string{"--force", archive}); code != 0 {
		t.Fatalf("runRestore(--force) = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestRestore_RefusesNewerSchemaVersion(t *testing.T) {
	srcDir := t.TempDir()
	newMigratedDB(t, srcDir)
	setBackupTestEnv(t, srcDir)
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	var backupOut bytes.Buffer
	if code := runBackup(&backupOut, []string{archive}); code != 0 {
		t.Fatalf("runBackup() = %d, want 0; output:\n%s", code, backupOut.String())
	}

	// Rewrite the archive's manifest.json with an impossibly high
	// schema_version, simulating a backup made by a newer styr binary.
	tampered := filepath.Join(t.TempDir(), "tampered.tar.gz")
	tamperManifestSchemaVersion(t, archive, tampered, 999999)

	dstDir := t.TempDir()
	setBackupTestEnv(t, dstDir)
	var stdout bytes.Buffer
	if code := runRestore(&stdout, []string{tampered}); code == 0 {
		t.Fatalf("runRestore() = 0, want non-zero for a too-new schema_version; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "newer than this binary supports") {
		t.Fatalf("output does not explain the refusal:\n%s", stdout.String())
	}
}

// tamperManifestSchemaVersion rewrites srcArchive's manifest.json entry
// with schemaVersion and writes the result to dstArchive, copying every
// other entry unchanged.
func tamperManifestSchemaVersion(t *testing.T, srcArchive, dstArchive string, schemaVersion int64) {
	t.Helper()
	entries := readTarGz(t, srcArchive)
	var manifest backupManifest
	if err := json.Unmarshal(entries[backupEntryManifest], &manifest); err != nil {
		t.Fatalf("parse manifest.json: %v", err)
	}
	manifest.SchemaVersion = schemaVersion
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	entries[backupEntryManifest] = manifestJSON

	out, err := os.Create(dstArchive)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for name, data := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}
