package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
)

// runRestore replaces the configured database with the one inside a backup
// archive (see backup.go), keeping the previous file as
// "styr.db.bak-<timestamp>", then runs every pending migration on the
// restored copy. It never touches config.yaml -- config is deploy-time
// state a backup only carries a redacted reference copy of, not something
// restore re-applies.
func runRestore(stdout io.Writer, args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(stdout)
	force := fs.Bool("force", false, "restore even if a styr serve process appears to be running")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdout, "usage: styr restore <file.tar.gz> [--force]")
		return 2
	}
	archivePath := fs.Arg(0)

	cfg, err := config.Load(os.Getenv("STYR_CONFIG"))
	if err != nil {
		fmt.Fprintf(stdout, "restore: %v\n", err)
		return 1
	}

	if !*force {
		pid, running, err := serverRunning(cfg.DataDir)
		if err != nil {
			fmt.Fprintf(stdout, "restore: %v\n", err)
			return 1
		}
		if running {
			fmt.Fprintf(stdout, "restore: styr serve appears to be running (pid %d, from %s); stop it first or pass --force\n", pid, pidFilePath(cfg.DataDir))
			return 1
		}
	}

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		fmt.Fprintf(stdout, "restore: %v\n", err)
		return 1
	}

	manifest, extractedDB, err := extractBackupArchive(archivePath, cfg.DataDir)
	if err != nil {
		fmt.Fprintf(stdout, "restore: %v\n", err)
		return 1
	}

	latest, err := latestSchemaVersion()
	if err != nil {
		_ = os.Remove(extractedDB)
		fmt.Fprintf(stdout, "restore: %v\n", err)
		return 1
	}
	if manifest.SchemaVersion > latest {
		_ = os.Remove(extractedDB)
		fmt.Fprintf(stdout, "restore: backup schema_version %d is newer than this binary supports (latest %d); install a newer styr and retry\n", manifest.SchemaVersion, latest)
		return 1
	}

	backedUpPath, err := replaceDatabase(cfg.DBPath(), extractedDB)
	if err != nil {
		fmt.Fprintf(stdout, "restore: %v\n", err)
		return 1
	}

	migratedDB, err := db.Open(cfg.DBPath())
	if err != nil {
		fmt.Fprintf(stdout, "restore: %v\n", err)
		return 1
	}
	newVersion, verErr := goose.GetDBVersion(migratedDB.DB)
	_ = migratedDB.Close()
	if verErr != nil {
		fmt.Fprintf(stdout, "restore: read migrated schema version: %v\n", verErr)
		return 1
	}

	fmt.Fprintf(stdout, "restored %s into %s\n", archivePath, cfg.DBPath())
	fmt.Fprintf(stdout, "  backup schema_version: %d (styr %s, created %s)\n", manifest.SchemaVersion, manifest.StyrVersion, manifest.CreatedAt)
	fmt.Fprintf(stdout, "  migrated to:           %d\n", newVersion)
	if backedUpPath != "" {
		fmt.Fprintf(stdout, "  previous database kept at %s\n", backedUpPath)
	} else {
		fmt.Fprintln(stdout, "  no previous database to keep")
	}
	fmt.Fprintln(stdout, "  config.yaml untouched")
	return 0
}

// extractBackupArchive reads archivePath (a backup.go-produced gzip'd tar)
// and returns its parsed manifest plus the path of a temporary file, created
// inside dbDestDir so it can later be renamed atomically into place, holding
// the extracted styr.db entry. The caller owns cleaning up that temp file on
// every path (success moves it away with os.Rename; failure must remove it
// itself). config.yaml, if present, is ignored -- restore never touches
// config.
func extractBackupArchive(archivePath, dbDestDir string) (manifest *backupManifest, dbTmpPath string, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, "", fmt.Errorf("%s is not a gzip archive: %w", archivePath, err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var manifestBytes []byte
	var dbTmp *os.File
	cleanup := func() {
		if dbTmp != nil {
			_ = dbTmp.Close()
			_ = os.Remove(dbTmp.Name())
		}
	}

	for {
		hdr, terr := tr.Next()
		if errors.Is(terr, io.EOF) {
			break
		}
		if terr != nil {
			cleanup()
			return nil, "", fmt.Errorf("read %s: %w", archivePath, terr)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		switch hdr.Name {
		case backupEntryManifest:
			manifestBytes, terr = io.ReadAll(tr)
			if terr != nil {
				cleanup()
				return nil, "", fmt.Errorf("read manifest.json: %w", terr)
			}
		case backupEntryDB:
			if dbTmp == nil {
				dbTmp, terr = os.CreateTemp(dbDestDir, ".styr-restore-*")
				if terr != nil {
					return nil, "", fmt.Errorf("create temp file: %w", terr)
				}
			}
			if _, terr = io.Copy(dbTmp, tr); terr != nil {
				cleanup()
				return nil, "", fmt.Errorf("extract styr.db: %w", terr)
			}
		}
	}

	if manifestBytes == nil {
		cleanup()
		return nil, "", fmt.Errorf("%s is not a styr backup: missing manifest.json", archivePath)
	}
	if dbTmp == nil {
		return nil, "", fmt.Errorf("%s is not a styr backup: missing styr.db", archivePath)
	}
	if err := dbTmp.Close(); err != nil {
		_ = os.Remove(dbTmp.Name())
		return nil, "", fmt.Errorf("write temp file: %w", err)
	}

	var m backupManifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		_ = os.Remove(dbTmp.Name())
		return nil, "", fmt.Errorf("parse manifest.json: %w", err)
	}
	return &m, dbTmp.Name(), nil
}

// latestSchemaVersion migrates a throwaway database to head with the
// binary's own embedded migrations (via db.Open) and returns the resulting
// goose schema version -- the highest schema version this binary knows how
// to run against. cmd/styr has no access to internal/db's embedded
// migration files itself, so a scratch database is the only way to ask "how
// far can this binary migrate".
func latestSchemaVersion() (int64, error) {
	dir, err := os.MkdirTemp("", "styr-schema-check-*")
	if err != nil {
		return 0, fmt.Errorf("determine latest schema version: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	scratch, err := db.Open(filepath.Join(dir, "styr.db"))
	if err != nil {
		return 0, fmt.Errorf("determine latest schema version: %w", err)
	}
	defer func() { _ = scratch.Close() }()

	v, err := goose.GetDBVersion(scratch.DB)
	if err != nil {
		return 0, fmt.Errorf("determine latest schema version: %w", err)
	}
	return v, nil
}

// replaceDatabase installs newDBPath as dbPath. Any existing file at dbPath
// (and its -wal/-shm siblings) is renamed aside to "<dbPath>.bak-<UTC
// timestamp>" first, rather than deleted, so a bad restore is recoverable.
// backedUpPath is "" when there was nothing to back up.
func replaceDatabase(dbPath, newDBPath string) (backedUpPath string, err error) {
	if _, statErr := os.Stat(dbPath); statErr == nil {
		backedUpPath = dbPath + ".bak-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.Rename(dbPath, backedUpPath); err != nil {
			return "", fmt.Errorf("back up existing database: %w", err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			old := dbPath + suffix
			if _, err := os.Stat(old); err == nil {
				_ = os.Rename(old, backedUpPath+suffix)
			}
		}
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("stat %s: %w", dbPath, statErr)
	}

	if err := os.Rename(newDBPath, dbPath); err != nil {
		return backedUpPath, fmt.Errorf("install restored database: %w", err)
	}
	return backedUpPath, nil
}
