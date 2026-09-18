package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
)

// checkTimeout bounds every doctor check that talks to a subprocess or the
// network, so a hung claude binary or an unreachable OIDC issuer cannot
// leave doctor running forever.
const checkTimeout = 10 * time.Second

// errSkip marks a check as intentionally skipped rather than failed (for
// example, OIDC discovery in dev mode). Wrap it with fmt.Errorf("%w: ...")
// to attach a reason.
var errSkip = errors.New("skip")

// check is one doctor diagnostic: a human-readable name and a function that
// returns nil (ok), an error wrapping errSkip (skip), or any other error
// (FAIL).
type check struct {
	name string
	run  func() error
}

// doctorChecks builds every doctor check against cfg. Each check is
// self-contained so doctor_test.go can run one at a time against a fake
// claude binary or a missing one.
func doctorChecks(cfg config.Config) []check {
	return []check{
		{"config loads", func() error {
			_, err := config.Load(os.Getenv("STYR_CONFIG"))
			return err
		}},
		{"data dir writable", func() error { return checkDataDirWritable(cfg.DataDir) }},
		{"claude binary found and --version runs", func() error { return checkClaudeBinary(cfg.ClaudeBin) }},
		{"git on PATH", func() error {
			if _, err := exec.LookPath("git"); err != nil {
				return fmt.Errorf("git not found on PATH: %w", err)
			}
			return nil
		}},
		{"secret key length", func() error {
			if n := len(cfg.SecretKey); n < 32 {
				return fmt.Errorf("secret_key is %d bytes, want at least 32", n)
			}
			return nil
		}},
		{"database opens", func() error { return checkDatabaseOpens(cfg.DBPath()) }},
		{"oidc discovery reachable", func() error { return checkOIDCDiscovery(cfg) }},
	}
}

// checkDataDirWritable creates cfg.DataDir if needed and confirms Styr can
// write a file into it.
func checkDataDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return fmt.Errorf("write to %s: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("clean up %s: %w", name, err)
	}
	return nil
}

// checkClaudeBinary runs `<bin> --version` and reports whether it starts and
// exits cleanly.
func checkClaudeBinary(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, bin, "--version").Run(); err != nil {
		return fmt.Errorf("%s --version: %w", bin, err)
	}
	return nil
}

// checkDatabaseOpens opens (creating and migrating if needed) the database
// at path and closes it again.
func checkDatabaseOpens(path string) error {
	d, err := db.Open(path)
	if err != nil {
		return err
	}
	return d.Close()
}

// checkOIDCDiscovery GETs every configured provider's
// /.well-known/openid-configuration and expects 200 OK. It is skipped in dev
// (where OIDC is optional) and when no provider is configured.
func checkOIDCDiscovery(cfg config.Config) error {
	if cfg.Env == "dev" {
		return fmt.Errorf("%w: dev mode", errSkip)
	}
	if len(cfg.OIDC) == 0 {
		return fmt.Errorf("%w: no oidc providers configured", errSkip)
	}
	client := &http.Client{Timeout: checkTimeout}
	for _, p := range cfg.OIDC {
		url := strings.TrimRight(p.Issuer, "/") + "/.well-known/openid-configuration"
		resp, err := client.Get(url)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Name, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: unexpected status %s", p.Name, resp.Status)
		}
	}
	return nil
}

// runDoctor loads the configuration (best effort: a load error is reported
// as its own FAIL line rather than aborting) and prints one line per check:
// "ok", "FAIL" or "skip". It returns 1 when any check fails, 0 otherwise.
func runDoctor(stdout io.Writer) int {
	cfg, _ := config.Load(os.Getenv("STYR_CONFIG"))

	failed := false
	for _, c := range doctorChecks(cfg) {
		switch err := c.run(); {
		case err == nil:
			fmt.Fprintf(stdout, "ok  %s\n", c.name)
		case errors.Is(err, errSkip):
			fmt.Fprintf(stdout, "skip %s: %v\n", c.name, err)
		default:
			fmt.Fprintf(stdout, "FAIL %s: %v\n", c.name, err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}
