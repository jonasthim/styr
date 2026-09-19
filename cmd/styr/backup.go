package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"

	"github.com/jonasthim/styr/internal/config"
)

// Backup archive layout: an online-consistent copy of the database (made
// with SQLite's VACUUM INTO, so it is safe to run while `styr serve` keeps
// writing to the live file), the resolved config file with its secrets
// stripped when one is readable, and a manifest describing both. Never the
// env file, tokens, or a user's Claude HOME: those are either secrets that
// must not leave the host in a plain tar.gz, or state each user recreates
// by logging in again.
const (
	backupEntryDB       = "styr.db"
	backupEntryConfig   = "config.yaml"
	backupEntryManifest = "manifest.json"
)

// secretConfigKeys are top-level config.yaml keys stripped from the copy a
// backup carries.
var secretConfigKeys = []string{"secret_key", "dev_user"}

// backupManifest describes one styr backup archive, written as
// manifest.json inside it.
type backupManifest struct {
	StyrVersion   string `json:"styr_version"`
	SchemaVersion int64  `json:"schema_version"`
	CreatedAt     string `json:"created_at"`
	Hostname      string `json:"hostname"`
}

// runBackup writes an online-consistent backup of the configured database,
// plus a redacted copy of the resolved config file when one is readable, to
// out as a gzip'd tar.
func runBackup(stdout io.Writer, args []string) int {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stdout, "usage: styr backup <file.tar.gz>")
		return 2
	}
	out := args[0]

	cfg, err := config.Load(os.Getenv("STYR_CONFIG"))
	if err != nil {
		fmt.Fprintf(stdout, "backup: %v\n", err)
		return 1
	}

	tmpDir, err := os.MkdirTemp("", "styr-backup-*")
	if err != nil {
		fmt.Fprintf(stdout, "backup: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbCopy := filepath.Join(tmpDir, "styr.db")
	schemaVersion, err := vacuumBackup(cfg.DBPath(), dbCopy)
	if err != nil {
		fmt.Fprintf(stdout, "backup: %v\n", err)
		return 1
	}

	manifest := backupManifest{
		StyrVersion:   version,
		SchemaVersion: schemaVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Hostname:      hostnameOrUnknown(),
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(stdout, "backup: %v\n", err)
		return 1
	}

	configYAML, haveConfig := redactedConfigYAML(os.Getenv("STYR_CONFIG"))

	if err := writeBackupArchive(out, dbCopy, manifestJSON, configYAML, haveConfig); err != nil {
		fmt.Fprintf(stdout, "backup: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "backup written to %s\n", out)
	fmt.Fprintf(stdout, "  styr_version:   %s\n", manifest.StyrVersion)
	fmt.Fprintf(stdout, "  schema_version: %d\n", manifest.SchemaVersion)
	fmt.Fprintf(stdout, "  created_at:     %s\n", manifest.CreatedAt)
	fmt.Fprintf(stdout, "  hostname:       %s\n", manifest.Hostname)
	if haveConfig {
		fmt.Fprintln(stdout, "  config.yaml:    included (secret_key, dev_user, oidc client_secret stripped)")
	} else {
		fmt.Fprintln(stdout, "  config.yaml:    not included (STYR_CONFIG unset or unreadable)")
	}
	return 0
}

// vacuumBackup copies the database at srcPath into a fresh file at dstPath
// with `VACUUM INTO`, and returns the copy's goose schema version. dstPath
// must not already exist.
//
// The modernc.org/sqlite driver has no OS-level read-only open (its DSN
// silently ignores the standard "mode=ro" query parameter), so "read-only"
// here means the connection only ever runs VACUUM INTO -- never a write
// statement against srcPath itself. VACUUM INTO takes its own read
// transaction against the live database and writes the compacted copy to a
// separate file, so it is safe to run while `styr serve` is concurrently
// writing to srcPath: SQLite's WAL mode lets the one writer and any number
// of readers proceed without blocking each other.
func vacuumBackup(srcPath, dstPath string) (int64, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return 0, fmt.Errorf("open database %s: %w", srcPath, err)
	}

	src, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", srcPath))
	if err != nil {
		return 0, fmt.Errorf("open database %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()
	src.SetMaxOpenConns(1)

	quoted := strings.ReplaceAll(dstPath, "'", "''")
	if _, err := src.ExecContext(context.Background(), "VACUUM INTO '"+quoted+"'"); err != nil {
		return 0, fmt.Errorf("vacuum %s into %s: %w", srcPath, dstPath, err)
	}

	dst, err := sql.Open("sqlite", "file:"+dstPath)
	if err != nil {
		return 0, fmt.Errorf("open backup copy: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if err := goose.SetDialect("sqlite3"); err != nil {
		return 0, fmt.Errorf("goose dialect: %w", err)
	}
	v, err := goose.GetDBVersion(dst)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}

// redactedConfigYAML reads the YAML file at path and returns it with every
// key in secretConfigKeys, and client_secret in every oidc provider,
// removed. ok is false when path is empty or the file cannot be read or
// parsed as a YAML mapping -- a backup omits config.yaml entirely rather
// than risk shipping an unredacted copy.
func redactedConfigYAML(path string) (data []byte, ok bool) {
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, false
	}
	for _, key := range secretConfigKeys {
		delete(doc, key)
	}
	if providers, ok := doc["oidc"].([]any); ok {
		for _, item := range providers {
			if provider, ok := item.(map[string]any); ok {
				delete(provider, "client_secret")
			}
		}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, false
	}
	return out, true
}

// hostnameOrUnknown returns os.Hostname(), or "unknown" when it fails
// (a backup manifest field must never abort the backup by itself).
func hostnameOrUnknown() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// writeBackupArchive writes a gzip'd tar to outPath containing the database
// copy at dbCopyPath as backupEntryDB, manifestJSON as backupEntryManifest,
// and, when haveConfig, configYAML as backupEntryConfig.
func writeBackupArchive(outPath, dbCopyPath string, manifestJSON, configYAML []byte, haveConfig bool) (err error) {
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	if err = addTarFile(tw, dbCopyPath, backupEntryDB); err != nil {
		return err
	}
	if haveConfig {
		if err = addTarBytes(tw, backupEntryConfig, configYAML); err != nil {
			return err
		}
	}
	if err = addTarBytes(tw, backupEntryManifest, manifestJSON); err != nil {
		return err
	}
	if err = tw.Close(); err != nil {
		return fmt.Errorf("finalize archive: %w", err)
	}
	if err = gz.Close(); err != nil {
		return fmt.Errorf("finalize archive: %w", err)
	}
	return nil
}

// addTarFile copies the file at path into tw as an entry named name.
func addTarFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("build %s header: %w", name, err)
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write %s header: %w", name, err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// addTarBytes writes data into tw as an entry named name.
func addTarBytes(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write %s header: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
