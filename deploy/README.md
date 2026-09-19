# Deploying Styr

Two ways to run Styr: the installer (bare Debian/Ubuntu host, systemd) or
Docker. Both need an OIDC provider reachable from wherever your browser
logs in, and a public `base_url` if you are not just testing on localhost.

## Option 1: installer + systemd

```bash
curl -fsSL https://raw.githubusercontent.com/jonasthim/styr/main/deploy/install.sh | sudo bash
```

This creates a system user `styr` (HOME `/var/lib/styr`), installs the
`styr` binary to `/usr/local/bin/styr`, writes `/etc/styr/config.yaml` from
`config.example.yaml`, generates `/etc/styr/env` with a random
`STYR_SECRET_KEY`, installs the Claude Code CLI for the `styr` user
(skip with `--no-claude`), and enables `styr.service`. It does **not**
install the Codex CLI: if you want Codex sessions too, install `codex`
yourself (for the `styr` user, so it is on that user's `PATH`), or point
`codex_bin` in `config.yaml` (or `STYR_CODEX_BIN`) at wherever you put it
— see [docs/CONFIGURATION.md](../docs/CONFIGURATION.md). Then:

1. Edit `/etc/styr/config.yaml`: set `base_url` and at least one `oidc`
   provider, and `systemctl restart styr`.
2. Run `sudo -u styr env STYR_CONFIG=/etc/styr/config.yaml
   /usr/local/bin/styr doctor` to check the install.
3. On any machine where you are logged into Claude, run `claude
   setup-token` and paste the token into the Styr UI under your profile
   (each user brings their own token; it bills their own Claude plan). For
   Codex sessions, paste an OpenAI API key into the same profile page's
   Codex key card instead — see [docs/HARNESSES.md](../docs/HARNESSES.md).

`install.sh --check` reports what would change, with no changes made;
`--uninstall` removes the service and binary but keeps `/var/lib/styr` and
`/etc/styr`. See `install.sh --help` for `--version`, `--binary` and
`--listen`, and [Upgrading](#upgrading) below to update an existing install.
`--version` refuses to install an older release than the one already
installed unless you also pass `--allow-downgrade` (see
[Upgrading](#upgrading)).

## Option 2: Docker

```bash
cp deploy/compose.yaml .
cp deploy/config.example.yaml config.yaml   # edit base_url and oidc
cat <<'EOF' > .env
STYR_CONFIG=/var/lib/styr/config.yaml
STYR_SECRET_KEY=$(openssl rand -base64 48)
STYR_OIDC_CLIENT_SECRET=
EOF
docker compose -f compose.yaml up -d
```

`deploy/compose.yaml` pulls `ghcr.io/jonasthim/styr:latest`; pass
`--build` to build `deploy/Dockerfile` from the repo root locally instead.
It bundles `git`, `openssh-client` and the Claude Code CLI (run as the
non-root `styr` user, uid 1000), mounts a named volume at `/var/lib/styr`
plus a bind mount at `/var/lib/styr/workspaces`, and publishes the app on
`127.0.0.1:8080` only -- put a TLS reverse proxy in front for remote access.
Like the installer, the image does not bundle the Codex CLI; add it to a
custom image build (or a bind mount on `PATH`) and set `codex_bin` if you
want Codex sessions from Docker too.

## Upgrading

**Take a backup first**: `styr backup /somewhere/styr-$(date +%F).tar.gz`
(as the `styr` user, or `docker compose exec styr styr backup ...` for
Docker) is one online, no-downtime command, and gives you a known-good
point to `styr restore` back to if the upgrade goes wrong -- see
[docs/OPERATIONS.md](../docs/OPERATIONS.md#backup).

There is no self-upgrade: re-run the installer (`curl ... | sudo bash`, same
as a first install) to fetch and install the latest `styr` binary, or pull a
new image tag and re-run `docker compose up -d` for Docker. Either way,
`styr` applies every pending database migration itself as part of opening
`styr.db` at startup (`goose` migrations embedded in the binary), so a
version bump never needs a separate migrate step -- just restart the
service (`systemctl restart styr`) or recreate the container. `styr
migrate` exists to apply migrations without starting the server (e.g. ahead
of a maintenance window); it is never required for an ordinary upgrade.
Config and data under `/etc/styr` and `/var/lib/styr` (or the Docker
volume) are untouched by either upgrade path.

`install.sh`'s very last line on success names the version transition it
just made -- `styr install: OK, upgraded 0.5.1 -> 1.0.0`, or `installed
1.0.0` on a first install, or `already current at 1.0.0` when there was
nothing to do -- so a script driving the installer can tell a real upgrade
from a no-op without parsing anything else (see
[docs/OPERATIONS.md](../docs/OPERATIONS.md#automating-upgrades)).

`install.sh --version vX.Y.Z` refuses to install a release older than the
one already installed (compared with `sort -V`, so `0.9.0` < `0.10.0`
correctly); pass `--allow-downgrade` to do it anyway. Downgrading the
binary does not downgrade the database schema, since migrations only ever
run forward -- restore a backup taken before the upgrade you want to undo
instead (`styr restore`, see `docs/OPERATIONS.md`).

## Both paths

`styr doctor` (installer) or `docker compose exec styr styr doctor` checks
the config, the database, the `claude` binary, OIDC discovery, free disk
space, orphan session worktrees and its own pid file. It does not yet
check the `codex` binary the same way -- an unreachable Codex CLI shows up
as a `codex` entry with `available: false` on `GET /api/v1/status` and in
the new-session dialog instead. Zero telemetry, no login wall beyond OIDC.
See [docs/OPERATIONS.md](../docs/OPERATIONS.md) for `styr backup`/`styr
restore`, upgrading/downgrading and orphan worktree cleanup in full, and
[docs/HARNESSES.md](../docs/HARNESSES.md) for the Codex key cards and
`codex_bin`.
