#!/usr/bin/env bash
# Styr installer for Debian 12+ / Ubuntu 22.04+ on x86_64 or arm64.
#
# One-command install (nothing else to download by hand):
#
#   curl -fsSL https://raw.githubusercontent.com/jonasthim/styr/main/deploy/install.sh | sudo bash
#
# Or download this file first and run it locally:
#
#   sudo ./install.sh [--version vX.Y.Z | --binary PATH] [--listen ADDR] [--no-claude]
#   sudo ./install.sh --check        # report installed vs latest version and planned changes; makes no changes
#   sudo ./install.sh --uninstall    # remove the service and binary; keeps /var/lib/styr and /etc/styr
#
# This script is fully self-contained: deploy/styr.service and
# deploy/config.example.yaml are embedded below as heredocs, so the
# one-liner above needs nothing else.
#
# GENERATED FILE. This exact file (deploy/install.sh) is produced from
# deploy/install.sh.in by deploy/build-installer.sh (`make deploy-sync`). Edit
# deploy/install.sh.in and the deploy/* source files it includes, not this
# file directly -- your edits would be overwritten on the next
# `make deploy-sync`.
#
# Idempotent: re-running upgrades the binary and unit file; data in
# /var/lib/styr, /etc/styr/config.yaml and /etc/styr/env are never
# overwritten. There is no self-upgrade and no sudoers drop-in in v0.1 --
# re-run this script (as root) to upgrade.
set -euo pipefail

REPO="jonasthim/styr"
DATA_DIR="/var/lib/styr"
CONF_DIR="/etc/styr"
CONFIG_FILE="$CONF_DIR/config.yaml"
ENV_FILE="$CONF_DIR/env"
REAL_BIN="/usr/local/bin/styr"
SERVICE_FILE="/etc/systemd/system/styr.service"
CLAUDE_SYMLINK="/usr/local/bin/claude"

BINARY=""
VERSION="latest"
LISTEN="127.0.0.1:8080"
UNINSTALL=0
NO_CLAUDE=0
CHECK=0

log() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
Styr installer

Usage:
  sudo ./install.sh [options]
  curl -fsSL https://raw.githubusercontent.com/jonasthim/styr/main/deploy/install.sh | sudo bash

Options:
  --version vX.Y.Z   Install this release tag instead of the latest one.
  --binary PATH      Install a locally built binary instead of downloading a
                      release (skips download and checksum verification).
  --listen ADDR      Address the server listens on (default 127.0.0.1:8080).
                      Only applied the first time config.yaml is written.
  --no-claude        Skip installing the Claude Code CLI for the styr user.
  --uninstall        Remove the systemd unit and the binary. Keeps
                      /var/lib/styr and /etc/styr.
  --check            Print installed vs latest/target version and what would
                      change. Makes no changes.
  -h, --help          Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --listen) LISTEN="$2"; shift 2 ;;
    --no-claude) NO_CLAUDE=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    --check) CHECK=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown flag $1 (see --help)" ;;
  esac
done

[[ $EUID -eq 0 ]] || die "run as root, e.g.: curl -fsSL .../install.sh | sudo bash"
command -v systemctl >/dev/null 2>&1 || die "systemd is required (systemctl not found)"

# Runtime prerequisites: git (workspaces are git checkouts), curl and CA certs
# (the Claude Code installer and the release download). Debian LXC images from
# community scripts ship without git.
ensure_pkgs() {
  local missing=()
  command -v git >/dev/null 2>&1 || missing+=(git)
  command -v curl >/dev/null 2>&1 || missing+=(curl)
  [[ -f /etc/ssl/certs/ca-certificates.crt ]] || missing+=(ca-certificates)
  [[ ${#missing[@]} -eq 0 ]] && return 0
  if command -v apt-get >/dev/null 2>&1; then
    log "Installing prerequisites: ${missing[*]}"
    DEBIAN_FRONTEND=noninteractive apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${missing[@]}"
  else
    die "missing prerequisites: ${missing[*]} (install them and re-run)"
  fi
}

# claude_path prints the styr user's Claude Code binary, or nothing. It must not
# use a login shell: on Debian/Ubuntu a login shell prints the MOTD, which then
# pollutes the captured path.
claude_path() {
  local p
  for p in /var/lib/styr/.local/bin/claude /var/lib/styr/.claude/local/claude /usr/local/bin/claude; do
    if [[ -x "$p" && ! -L "$p" ]]; then printf '%s\n' "$p"; return 0; fi
  done
  p="$(run_as_styr env PATH="/var/lib/styr/.local/bin:/usr/local/bin:/usr/bin:/bin" bash -c 'command -v claude' </dev/null 2>/dev/null | tail -n 1 || true)"
  [[ -n "$p" && -x "$p" ]] && printf '%s\n' "$p"
}

arch() {
  case "$(uname -m)" in
    x86_64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "unsupported architecture $(uname -m) (only linux amd64 and arm64 are supported)" ;;
  esac
}

# ---- embedded deploy/ file contents -----------------------------------------
# The heredoc bodies below are generated verbatim from the standalone files in
# deploy/ by deploy/build-installer.sh. Do not hand-edit them; edit the source
# file and run `make deploy-sync`, which also fails in CI on drift.

service_unit_content() {
  cat <<'SERVICE_EOF'
# Installed by deploy/install.sh to /etc/systemd/system/styr.service
[Unit]
Description=Styr
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=styr
Environment=STYR_CONFIG=/etc/styr/config.yaml
EnvironmentFile=/etc/styr/env
ExecStart=/usr/local/bin/styr serve
Restart=always
RestartSec=3

# Hardening. ProtectHome is intentionally NOT set: the Claude Code CLI and
# styr both write under the styr user's HOME, which lives inside
# /var/lib/styr (ReadWritePaths below), so ProtectHome would not need to
# deny anything extra here -- it is left off to match the deploy card
# exactly and because outbound network access (to the OIDC issuer, GitHub,
# and wherever the user's workspaces point) must not be restricted.
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/styr
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes

[Install]
WantedBy=multi-user.target
SERVICE_EOF
}

config_example_content() {
  cat <<'CONFIG_EOF'
# Example styr configuration. Copy to /etc/styr/config.yaml (or point
# STYR_CONFIG at a copy) and edit. Every field may also be set with a
# STYR_* environment variable; env values win over this file.

# env: deployment mode, "prod" or "dev". Defaults to "prod". Only "dev"
# honours dev_user and skips the base_url/oidc requirements below.
env: prod

# listen: address the HTTP server binds to.
listen: 127.0.0.1:8080

# base_url: the public URL users reach styr at. Required in prod; used to
# build the OIDC redirect URI.
base_url: https://styr.example.com

# data_dir: directory for the sqlite database, per-user data and workspace
# checkouts. Defaults to /var/lib/styr in prod, ./data in dev.
data_dir: /var/lib/styr

# secret_key: 32+ bytes (base64 or raw), used to encrypt stored Claude
# tokens at rest. Required. Generate with: openssl rand -base64 32
secret_key: change-me-to-a-random-32-byte-value

# claude_bin: path to the Claude Code CLI binary.
claude_bin: claude

# max_open_sessions: maximum number of concurrently open Claude sessions.
max_open_sessions: 4

# idle_timeout: how long an open session may sit idle before it is closed.
idle_timeout: 15m

# approval_timeout: how long an unattended profile waits for approval
# before the request expires.
approval_timeout: 30m

# oidc: one or more OpenID Connect providers shown on the login page.
# Required in prod (at least one provider).
oidc:
  - name: Authentik
    issuer: https://auth.example.com/application/o/styr/
    client_id: styr
    # client_secret can instead be supplied via STYR_OIDC_CLIENT_SECRET,
    # which always fills provider 0's secret.
    client_secret: change-me
    scopes: [openid, profile, email]

# dev_user: email of the synthetic logged-in user used when env is "dev".
# Ignored in prod.
dev_user: dev@example.com
CONFIG_EOF
}
# ---- end embedded content ---------------------------------------------------

resolve_latest_tag() {
  local loc tag
  loc="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null)" || loc=""
  if [[ "$loc" =~ /releases/tag/(.+)$ ]]; then
    printf '%s\n' "${BASH_REMATCH[1]}"
    return 0
  fi
  tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/')" || tag=""
  if [[ -n "$tag" ]]; then
    printf '%s\n' "$tag"
    return 0
  fi
  return 1
}

# Runs the installed binary to print its version. Never as root, matching
# the styr process's own privilege level.
installed_version() {
  if [[ -x "$REAL_BIN" ]] && id styr >/dev/null 2>&1; then
    run_as_styr "$REAL_BIN" version 2>/dev/null || echo "unknown"
  elif [[ -x "$REAL_BIN" ]]; then
    echo "unknown"
  else
    echo "(not installed)"
  fi
}

# run_as_styr CMD... executes CMD as the service user with a clean
# environment (runuser where available, sudo otherwise).
run_as_styr() {
  if command -v runuser >/dev/null 2>&1; then
    runuser -u styr -- "$@"
  else
    sudo -u styr -H -- "$@"
  fi
}

download_and_install_binary() {
  local tag="$1" ver a tarball tmp
  a="$(arch)"
  ver="${tag#v}"
  tarball="styr_${ver}_linux_${a}.tar.gz"
  tmp="$(mktemp -d)"
  log "Downloading $tarball ($tag)"
  curl -fsSL "https://github.com/$REPO/releases/download/$tag/$tarball" -o "$tmp/$tarball" \
    || { rm -rf "$tmp"; die "download failed: $tag/$tarball (use --binary PATH for a local build)"; }
  curl -fsSL "https://github.com/$REPO/releases/download/$tag/SHA256SUMS" -o "$tmp/SHA256SUMS" \
    || { rm -rf "$tmp"; die "download failed: $tag/SHA256SUMS (use --binary PATH for a local build)"; }
  log "Verifying checksum"
  ( cd "$tmp" && grep -E " \*?${tarball//./\\.}\$" SHA256SUMS | sha256sum -c - ) >/dev/null \
    || { rm -rf "$tmp"; die "checksum verification failed for $tarball"; }
  tar -xzf "$tmp/$tarball" -C "$tmp"
  [[ -f "$tmp/styr" ]] || { rm -rf "$tmp"; die "$tarball did not contain a styr binary"; }
  install -o root -g root -m 0755 "$tmp/styr" "$REAL_BIN.new"
  mv -f "$REAL_BIN.new" "$REAL_BIN"
  rm -rf "$tmp"
}

# ---- uninstall ---------------------------------------------------------------
do_uninstall() {
  if [[ $CHECK -eq 1 ]]; then
    echo "Would stop and disable styr.service"
    echo "Would remove: $SERVICE_FILE $REAL_BIN"
    echo "Would keep:   $DATA_DIR $CONF_DIR"
    return 0
  fi
  log "Stopping styr"
  systemctl disable --now styr.service 2>/dev/null || true
  rm -f "$SERVICE_FILE" "$REAL_BIN"
  systemctl daemon-reload
  log "Removed the service and binary. Data kept in $DATA_DIR and $CONF_DIR."
}

if [[ $UNINSTALL -eq 1 ]]; then
  do_uninstall
  exit 0
fi

# ---- check mode ----------------------------------------------------------------
if [[ $CHECK -eq 1 ]]; then
  echo "styr installer -- check mode, no changes will be made"
  echo
  echo "Installed version: $(installed_version)"
  if [[ -n "$BINARY" ]]; then
    echo "Target: local binary $BINARY"
  else
    tag="$VERSION"
    if [[ "$VERSION" == "latest" ]]; then
      tag="$(resolve_latest_tag)" || die "could not resolve the latest release (network?); pass --version vX.Y.Z or --binary PATH"
    fi
    echo "Target version: $tag ($(arch))"
  fi
  echo
  echo "Planned changes:"
  if id styr >/dev/null 2>&1; then
    echo "  - system user 'styr': present"
  else
    echo "  - system user 'styr': would be created (HOME=$DATA_DIR)"
  fi
  if [[ -d "$DATA_DIR" ]]; then
    echo "  - $DATA_DIR: present"
  else
    echo "  - $DATA_DIR: would be created (0750)"
  fi
  if diff -q <(service_unit_content) "$SERVICE_FILE" >/dev/null 2>&1; then
    echo "  - styr.service: up to date"
  else
    echo "  - styr.service: would install/update"
  fi
  if [[ -f "$CONFIG_FILE" ]]; then
    echo "  - $CONFIG_FILE: present, left untouched"
  else
    echo "  - $CONFIG_FILE: would be created (listen=$LISTEN)"
  fi
  if [[ -f "$ENV_FILE" ]]; then
    echo "  - $ENV_FILE: present, left untouched"
  else
    echo "  - $ENV_FILE: would be created (0600, new STYR_SECRET_KEY)"
  fi
  if [[ $NO_CLAUDE -eq 1 ]]; then
    echo "  - Claude Code CLI: skipped (--no-claude)"
  elif id styr >/dev/null 2>&1 && claude_path >/dev/null 2>&1; then
    echo "  - Claude Code CLI: already installed for styr"
  else
    echo "  - Claude Code CLI: would be installed for styr"
  fi
  if systemctl is-active --quiet styr.service 2>/dev/null; then
    echo "  - styr.service: active; would be restarted onto the installed binary"
  else
    echo "  - styr.service: would be enabled and started"
  fi
  exit 0
fi

# ---- install / upgrade ----------------------------------------------------------

SERVICE_WAS_ACTIVE=0
if systemctl is-active --quiet styr.service 2>/dev/null; then
  SERVICE_WAS_ACTIVE=1
fi

ensure_pkgs

log "Creating user and directories"
if ! id styr >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin --user-group styr
fi
install -d -o styr -g styr -m 0750 "$DATA_DIR"
install -d -o root -g styr -m 0750 "$CONF_DIR"

log "Installing binary"
if [[ -n "$BINARY" ]]; then
  [[ -f "$BINARY" ]] || die "--binary $BINARY not found"
  log "Installing local binary $BINARY"
  install -o root -g root -m 0755 "$BINARY" "$REAL_BIN"
else
  tag="$VERSION"
  if [[ "$VERSION" == "latest" ]]; then
    tag="$(resolve_latest_tag)" || die "could not resolve the latest release (network?); pass --version vX.Y.Z or --binary PATH for a local build"
  fi
  download_and_install_binary "$tag"
fi

log "Installing the systemd unit"
service_unit_content > "$SERVICE_FILE"
chmod 0644 "$SERVICE_FILE"

if [[ ! -f "$CONFIG_FILE" ]]; then
  log "Writing $CONFIG_FILE"
  config_example_content | sed "s|^listen: .*|listen: $LISTEN|" > "$CONFIG_FILE"
  chown root:styr "$CONFIG_FILE"
  chmod 0640 "$CONFIG_FILE"
fi

if [[ ! -f "$ENV_FILE" ]]; then
  log "Generating $ENV_FILE with a new STYR_SECRET_KEY"
  secret="$(openssl rand -base64 48)"
  {
    printf 'STYR_SECRET_KEY=%s\n' "$secret"
    printf 'STYR_OIDC_CLIENT_SECRET=\n'
  } > "$ENV_FILE.new"
  chown root:root "$ENV_FILE.new"
  chmod 0600 "$ENV_FILE.new"
  mv -f "$ENV_FILE.new" "$ENV_FILE"
fi

if [[ $NO_CLAUDE -eq 0 ]]; then
  if claude_path >/dev/null 2>&1; then
    log "Claude Code CLI already installed for styr"
  else
    log "Installing the Claude Code CLI for styr"
    curl -fsSL https://claude.ai/install.sh | sudo -u styr bash
  fi
  # systemd's own default PATH does not include the styr user's HOME, so
  # symlink the installed binary into /usr/local/bin (already on that
  # default PATH) so `claude_bin: claude` resolves for the service too.
  claude_real="$(claude_path 2>/dev/null || true)"
  if [[ -n "$claude_real" ]]; then
    ln -sfn "$claude_real" "$CLAUDE_SYMLINK"
  else
    echo "warning: could not locate the installed claude binary for styr; set claude_bin in $CONFIG_FILE to its full path" >&2
  fi
else
  log "Skipping Claude Code CLI install (--no-claude)"
fi

log "Starting styr"
systemctl daemon-reload
systemctl enable styr.service >/dev/null 2>&1 || systemctl enable styr.service
if [[ $SERVICE_WAS_ACTIVE -eq 1 ]]; then
  systemctl restart styr.service
else
  systemctl start styr.service
fi
sleep 1
systemctl --no-pager --lines=5 status styr.service || true

cat <<MSG

Installed. Next steps:

  1. Edit $CONFIG_FILE: set base_url to the public URL you will reach styr at,
     and fill in at least one oidc provider (issuer, client_id; client_secret
     can also go in $ENV_FILE as STYR_OIDC_CLIENT_SECRET). Then:
       systemctl restart styr

  2. Check the install:
       sudo -u styr env STYR_CONFIG=$CONFIG_FILE $REAL_BIN doctor

  3. On a machine where you are logged into Claude, run:
       claude setup-token
     and paste the resulting token into the styr UI under your profile once
     you have logged in (each user brings their own token; it bills their
     Claude plan).

Logs: journalctl -u styr -f
Data: $DATA_DIR   Config: $CONF_DIR
Upgrade later by re-running this installer.
MSG
