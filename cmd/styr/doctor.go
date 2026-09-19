package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/gitops"
)

// checkTimeout bounds every doctor check that talks to a subprocess or the
// network, so a hung claude binary or an unreachable OIDC issuer cannot
// leave doctor running forever.
const checkTimeout = 10 * time.Second

// errSkip marks a check as intentionally skipped rather than failed (for
// example, OIDC discovery in dev mode). Wrap it with fmt.Errorf("%w: ...")
// to attach a reason.
var errSkip = errors.New("skip")

// errWarn marks a check as a warning rather than a failure: something worth
// a human's attention (low disk space, orphan worktrees, a stale pid file)
// that does not make doctor exit non-zero the way a FAIL does. Wrap it with
// fmt.Errorf("%w: ...") to attach a reason.
var errWarn = errors.New("warn")

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
		{"codex binary found (optional)", func() error { return checkCodexBinary(cfg.CodexBin) }},
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
		{"disk space free on data dir", func() error { return checkDiskSpace(cfg.DataDir) }},
		{"orphan session worktrees", func() error { return checkOrphanWorktrees(cfg) }},
		{"pid file liveness", func() error { return checkPIDFileLiveness(cfg) }},
	}
}

// diskWarnBytes and diskFailBytes are the free-space thresholds for the
// "disk space free on data dir" check.
const (
	diskWarnBytes int64 = 1 << 30   // 1 GiB
	diskFailBytes int64 = 200 << 20 // 200 MiB
)

// checkDiskSpace reports the free space available on the filesystem
// holding dir (creating dir if needed): FAIL below diskFailBytes, warn
// below diskWarnBytes, ok otherwise.
func checkDiskSpace(dir string) error {
	if dir == "" {
		return errors.New("data_dir not configured")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return fmt.Errorf("statfs %s: %w", dir, err)
	}
	free := int64(stat.Bavail) * int64(stat.Bsize) //nolint:unconvert // Bavail/Bsize widths vary by arch
	switch {
	case free < diskFailBytes:
		return fmt.Errorf("only %s free on %s, want at least %s", formatBytes(free), dir, formatBytes(diskFailBytes))
	case free < diskWarnBytes:
		return fmt.Errorf("%w: only %s free on %s, want at least %s", errWarn, formatBytes(free), dir, formatBytes(diskWarnBytes))
	default:
		return nil
	}
}

// formatBytes renders n as a human-readable size using binary (1024-based)
// units, e.g. "1.3 GiB".
func formatBytes(n int64) string {
	const (
		kib = 1 << 10
		mib = 1 << 20
		gib = 1 << 30
	)
	switch {
	case n >= gib:
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/mib)
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/kib)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// worktreesSubdirName is the workspace-relative directory session worktrees
// live under (internal/sessions.worktreesSubdir, duplicated here since that
// constant is unexported and doctor.go may only touch cmd/styr files).
const worktreesSubdirName = ".styr/worktrees"

// orphanWorktree is a directory under a workspace's worktrees root that no
// session row references.
type orphanWorktree struct {
	wsPath string // the owning workspace's checkout path, for RemoveWorktree
	path   string
	size   int64
}

// findOrphanWorktrees opens cfg's database and, for every registered
// workspace, lists the directories under "<workspace>/.styr/worktrees/"
// that no session's worktree column names (see internal/db.Sessions.
// GetByWorktree). A workspace with no worktrees directory yet is not an
// error.
func findOrphanWorktrees(ctx context.Context, cfg config.Config) ([]orphanWorktree, error) {
	if cfg.DataDir == "" {
		return nil, nil
	}
	d, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = d.Close() }()

	workspaces, err := db.NewWorkspaces(d).ListVisible(ctx, "", true)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	sessions := db.NewSessions(d)

	var out []orphanWorktree
	for _, ws := range workspaces {
		root := filepath.Join(ws.Path, filepath.FromSlash(worktreesSubdirName))
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			_, err := sessions.GetByWorktree(ctx, path)
			switch {
			case err == nil:
				continue
			case errors.Is(err, domain.ErrNotFound):
				out = append(out, orphanWorktree{wsPath: ws.Path, path: path, size: dirSize(path)})
			default:
				return nil, fmt.Errorf("look up worktree %s: %w", path, err)
			}
		}
	}
	return out, nil
}

// dirSize sums the size of every regular file under root. Errors walking
// individual entries are ignored: a best-effort size is enough for a
// doctor report.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// checkOrphanWorktrees warns when findOrphanWorktrees finds any orphan,
// reporting their count and total size.
func checkOrphanWorktrees(cfg config.Config) error {
	orphans, err := findOrphanWorktrees(context.Background(), cfg)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	var total int64
	for _, o := range orphans {
		total += o.size
	}
	return fmt.Errorf("%w: %d orphan worktree(s) using %s; run `styr doctor --prune-worktrees` to remove them",
		errWarn, len(orphans), formatBytes(total))
}

// pruneOrphanWorktrees removes every orphan findOrphanWorktrees finds with
// `git worktree remove --force` plus its branch (internal/gitops.Repo.
// RemoveWorktree), stopping at the first error so a bad workspace path
// cannot silently eat the rest of the list.
func pruneOrphanWorktrees(ctx context.Context, cfg config.Config) (removed int, freed int64, err error) {
	orphans, err := findOrphanWorktrees(ctx, cfg)
	if err != nil {
		return 0, 0, err
	}
	for _, o := range orphans {
		if err := (gitops.Repo{Path: o.wsPath}).RemoveWorktree(ctx, o.path, true); err != nil {
			return removed, freed, fmt.Errorf("remove worktree %s: %w", o.path, err)
		}
		removed++
		freed += o.size
	}
	return removed, freed, nil
}

// checkPIDFileLiveness reports the state of <data_dir>/styr.pid: ok when
// absent (nothing running) or present and live, warn when present but
// stale (the process it names is gone).
func checkPIDFileLiveness(cfg config.Config) error {
	if cfg.DataDir == "" {
		return errors.New("data_dir not configured")
	}
	path := pidFilePath(cfg.DataDir)
	pid, ok, err := readPIDFile(path)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if pidLive(pid) {
		return nil
	}
	return fmt.Errorf("%w: stale pid file %s names pid %d, which is not running; `styr serve` overwrites it on its next start",
		errWarn, path, pid)
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

// checkCodexBinary is checkClaudeBinary for the optional Codex CLI: a
// binary that is not on PATH is a skip (Codex sessions are simply
// unavailable, which /api/v1/status also reports), while one that is
// present but cannot answer --version is a FAIL.
func checkCodexBinary(bin string) error {
	if bin == "" {
		return fmt.Errorf("%w: codex_bin not configured; Codex sessions unavailable", errSkip)
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%w: %s not found; Codex sessions unavailable until it is installed", errSkip, bin)
	}
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
// checkReason is err's message without the leading "<sentinel>: " that
// wrapping errSkip/errWarn with %w adds, so a line reads
// "skip codex binary found (optional): codex not found" rather than
// "skip ...: skip: codex not found".
func checkReason(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

func runDoctor(stdout io.Writer, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stdout)
	prune := fs.Bool("prune-worktrees", false, "remove orphan session worktrees (no session row references them) before running checks")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	loadEnv(stdout)
	cfg, _ := config.Load(os.Getenv("STYR_CONFIG"))

	if *prune {
		removed, freed, err := pruneOrphanWorktrees(context.Background(), cfg)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "warn prune worktrees: %v\n", err)
		case removed == 0:
			fmt.Fprintln(stdout, "note no orphan worktrees to prune")
		default:
			fmt.Fprintf(stdout, "note pruned %d orphan worktree(s), freed %s\n", removed, formatBytes(freed))
		}
	}

	failed := false
	for _, c := range doctorChecks(cfg) {
		switch err := c.run(); {
		case err == nil:
			fmt.Fprintf(stdout, "ok  %s\n", c.name)
		case errors.Is(err, errSkip):
			fmt.Fprintf(stdout, "skip %s: %s\n", c.name, checkReason(err, errSkip))
		case errors.Is(err, errWarn):
			fmt.Fprintf(stdout, "warn %s: %s\n", c.name, checkReason(err, errWarn))
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
