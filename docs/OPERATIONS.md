# Operations

Backup, restore, upgrade, downgrade, orphan worktrees and the pid file `styr serve` writes while
it runs. `cmd/styr/{backup,restore,pidfile}.go` and `cmd/styr/doctor.go` are the source of truth
if anything here and the code disagree.

## Running a subcommand by hand

Every subcommand that loads the configuration (`serve`, `migrate`, `doctor`, `backup`,
`restore`) first loads `/etc/styr/env` (or the file named by `STYR_ENV_FILE`) into its
environment, without overriding variables that are already set. That is where the installer
puts `STYR_SECRET_KEY` and the OIDC client secret; the systemd unit passes the same file as
`EnvironmentFile=`, so under systemd the load is a no-op. Run by hand as the `styr` user
(`sudo -u styr env STYR_CONFIG=/etc/styr/config.yaml styr backup ...`) it is what makes the
command find the secret key without a `set -a; . /etc/styr/env` first. The command prints
`note loaded N variable(s) from /etc/styr/env` when it did, and a `warn ... exists but is not
readable by this user` line when the file is there but the calling user cannot read it (it is
`root:styr 0640`) — the usual cause of a puzzling `secret_key must be at least 32 bytes` from a
correctly installed host.

## Backup

```
styr backup <file.tar.gz>
```

Writes an online-consistent snapshot of the running deployment to `<file.tar.gz>`: no need to
stop `styr serve` first, and no server-side lock to check or wait on. It works because SQLite
has none of a client/server database's locking to negotiate — `styr backup` opens the database
itself and runs `VACUUM INTO '<tmp file>'`, which takes its own read transaction against the
live file and writes a fresh, compacted copy to a new one. In WAL mode (how `styr serve` always
opens the database, see `internal/db/db.go`) a reader like this never blocks, and is never
blocked by, the one writer, so the copy is safe to take while the server keeps handling
requests. The `styr backup` process itself never issues a write statement against the live
database file — "read-only" in that sense, since the driver Styr uses
(`modernc.org/sqlite`) has no OS-level read-only open mode to request instead.

The archive is a gzip'd tar with up to three entries:

| Entry | Contents |
|---|---|
| `styr.db` | The `VACUUM INTO` copy of the database. |
| `config.yaml` | The file at `STYR_CONFIG`, when one is set and readable, with `secret_key`, `dev_user` and every `oidc[].client_secret` stripped. Omitted entirely when `STYR_CONFIG` is unset or the file cannot be read or parsed — a backup never ships an unredacted config rather than guess. |
| `manifest.json` | `{styr_version, schema_version, created_at, hostname}` — see below. |

`styr backup` prints the manifest summary and whether `config.yaml` was included:

```
backup written to /root/styr-2026-09-19.tar.gz
  styr_version:   0.5.0
  schema_version: 10
  created_at:     2026-09-19T14:03:11Z
  hostname:       styr-prod
  config.yaml:    included (secret_key, dev_user, oidc client_secret stripped)
```

**Never included:** `/etc/styr/env` (holds `STYR_SECRET_KEY` and the OIDC client secret in the
clear), any Claude/API token, or any user's `HOME` under `data_dir/users/<id>` — those are either
secrets that must not leave the host in a plain `tar.gz`, or state each user recreates for free
by logging in (`claude setup-token`) again. A worktree checkout under a workspace's
`.styr/worktrees/` is likewise not backed up: it is disposable, recreated from the branch it was
built on (see [Orphan worktrees](#orphan-worktrees) below for what happens when the branch
itself is gone too).

## Restore

```
styr restore <file.tar.gz> [--force]
```

Replaces the configured database (`data_dir/styr.db`) with the one inside `<file.tar.gz>`, then
runs every pending migration on the restored copy — the same `goose` migrations
`styr serve`/`styr migrate` apply on an ordinary upgrade (see [Upgrading](#upgrading)). It never
touches `config.yaml`, even when the archive carries one: config is deploy-time state, restore
only ever replaces data.

Refuses to run in two cases:

- **A `styr serve` process looks live.** `styr serve` writes its PID to `data_dir/styr.pid` at
  startup and removes it again on a clean shutdown (see [The pid file](#the-pid-file)). If that
  file exists and names a live process (`kill -0` semantics), restore refuses — swapping the
  database file out from under a running server corrupts its view of the world. Stop the service
  first (`systemctl stop styr`), or pass `--force` to override the check (only ever appropriate
  when you know the pid file is stale but doctor or `kill -0` disagrees, e.g. a container
  restart that left the file behind under a different pid namespace).
- **The backup's `schema_version` is newer than this binary supports.** Restore determines "how
  far this binary can migrate" by migrating a throwaway scratch database to head and reading its
  resulting schema version (`cmd/styr/restore.go`'s `latestSchemaVersion`) — cmd/styr has no
  direct access to `internal/db`'s embedded migration files, so this is the only way to ask.
  When the backup's `schema_version` is higher, the binary is older than the one that made the
  backup and restoring would leave rows and columns it does not understand; install a newer
  `styr` first (see [Upgrading](#upgrading)).

On success, the previous `data_dir/styr.db` (and any `-wal`/`-shm` siblings) is renamed aside to
`styr.db.bak-<UTC timestamp>` rather than deleted, so a bad restore is always recoverable by
hand. A fresh `data_dir` with no existing database restores straight in, with nothing to keep.
Restore prints what it did:

```
restored /root/styr-2026-09-19.tar.gz into /var/lib/styr/styr.db
  backup schema_version: 10 (styr 0.5.0, created 2026-09-19T14:03:11Z)
  migrated to:           10
  previous database kept at /var/lib/styr/styr.db.bak-20260919T140512Z
  config.yaml untouched
```

## Upgrading

Take a backup first: `styr backup <file>` (see [Backup](#backup) above) is one online,
no-downtime command that costs nothing while the old version is still running, and it is the
way back if the new version turns out to be a mistake (`styr restore`, see
[Restore](#restore)).

There is no self-upgrade: re-run the installer (`curl ... | sudo bash`, or `--version vX.Y.Z`
for a specific release) to fetch and install a newer `styr` binary, or pull a new image tag for
Docker. `styr` applies every pending database migration itself as part of opening `styr.db` at
startup, so a version bump never needs a separate `styr migrate` step — restarting the service is
enough. See `deploy/README.md`'s own Upgrading section for the full walkthrough.

### Automating upgrades

Keep the installer's output when you script it. It exits non-zero and prints `error: …` on
any refusal (for example the downgrade guard), and its very last line on success names the
version transition it just made: `styr install: OK, upgraded 0.5.1 -> 1.0.0` (an upgrade),
`styr install: OK, installed 1.0.0` (a first install), or `styr install: OK, already current at
1.0.0` (nothing to do). Discarding output and checking `styr version` afterwards hides a refused
upgrade behind an unchanged version; matching against the exact line above instead tells a real
upgrade, a first install and a no-op apart without parsing anything else.

## Downgrading

`install.sh --version vX.Y.Z` refuses to install a release older than the one already installed,
comparing versions with `sort -V` (natural/dotted order, so `0.9.0` correctly sorts before
`0.10.0`) rather than a plain string compare:

```
$ sudo ./install.sh --version v0.4.0
error: refusing to downgrade styr from 0.5.0 to 0.4.0; pass --allow-downgrade to override
```

Pass `--allow-downgrade` to install the older release anyway. Downgrading the binary does **not**
downgrade the database schema — `goose` migrations only ever run forward — so an older binary
started against a database a newer one already migrated may refuse to start or behave
unexpectedly. `styr restore` is the supported way back to an older schema: restore a backup taken
before the upgrade you want to undo (see [Restore](#restore) above), which also requires a
compatible-or-newer binary already installed.

## Orphan worktrees

Every session on a worktree-enabled workspace gets its own git worktree under
`<workspace path>/.styr/worktrees/<session id>` (see `docs/REVIEW.md`). Normal session cleanup
(Discard, or closing out a session already committed or turned into a PR) removes its worktree
immediately, but a directory can still be left behind — a process killed mid-cleanup, a
hand-edited database row, a worktree left over from before a downgrade/rollback. `styr doctor`
reports these as **orphan worktrees**: a directory under some registered workspace's
`.styr/worktrees/` that no session row's `worktree` column names
(`internal/db.Sessions.GetByWorktree`).

```
$ styr doctor
...
warn orphan session worktrees: 2 orphan worktree(s) using 340.0 MiB; run `styr doctor --prune-worktrees` to remove them
...
```

`styr doctor --prune-worktrees` removes every orphan it finds before running its checks, via the
same `git worktree remove --force` plus branch delete that Discard uses
(`internal/gitops.Repo.RemoveWorktree`), and prints how many it removed and how much space that
freed:

```
$ styr doctor --prune-worktrees
note pruned 2 orphan worktree(s), freed 340.0 MiB
ok  config loads
...
```

An orphan worktree is a warning, not a failure — it never makes `styr doctor`'s exit code
non-zero on its own — since disk space aside, a stray worktree is harmless: nothing else on the
system references it.

## The pid file

`styr serve` writes its process id to `data_dir/styr.pid` right after creating `data_dir`, and
removes the file again on every shutdown path, including a graceful SIGINT/SIGTERM (see
`cmd/styr/serve.go`, `cmd/styr/pidfile.go`). It exists purely so another `styr` invocation —
`restore`'s running-server check, or `doctor`'s liveness check — can tell whether a server is up
against a given `data_dir` without a network call or a database-level lock: `kill -0 <pid>`
(sending signal 0, which the kernel validates without delivering anything) tells the difference
between "still running" and "process gone, file just never got cleaned up" (a crash, `kill -9`,
or a host power loss).

`styr doctor` reports the file's state as its own check: `ok` when there is no pid file (nothing
running) or the pid it names is live, `warn` when the file exists but names a process that is no
longer running — a stale file, safe to ignore, since the next `styr serve` start overwrites it
unconditionally.

## Both paths

`styr doctor` (installer) or `docker compose exec styr styr doctor` checks the config, the
database, the `claude` binary, OIDC discovery, free disk space on `data_dir` (`warn` below 1 GiB
free, `FAIL` below 200 MiB), orphan worktrees, and the pid file's liveness. It does not yet probe
the `codex` binary the same way `claude`'s is checked — neither the installer nor the Docker
image installs the Codex CLI, so if you want Codex sessions, install `codex` yourself and point
`codex_bin` (`STYR_CODEX_BIN`) at it; an unreachable one shows up as `available: false` on
`GET /api/v1/status` rather than as a `doctor` warning. Each user then pastes a Codex key
(Profile → Codex key card) or an admin sets the service-wide one under Settings, the same way a
Claude token or the service token works — see `docs/HARNESSES.md`. See `docs/CONFIGURATION.md`
for every `config.yaml` key and the data directory layout, and `deploy/README.md` for
install/upgrade/uninstall.
