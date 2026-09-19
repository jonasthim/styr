# Security policy

## Reporting a vulnerability

Please report security issues privately by email to **jonas.thim@gmail.com** rather than opening
a public issue. Include:

- A description of the issue and its impact
- Steps to reproduce, or a proof of concept
- The affected version or commit

You should get an acknowledgement within a few days. We aim to keep you updated as the issue is
investigated and fixed.

## Disclosure policy

We follow coordinated disclosure with a **90-day** window from the initial report: we'll work
with you to understand and fix the issue, and ask that details stay private until a fix is
released or 90 days have passed, whichever comes first. We're happy to credit reporters in the
release notes unless you'd prefer to stay anonymous.

## Scope

In scope:

- Claude token and Codex key storage and handling (encryption at rest, never returned in full
  by the API, per-user isolation)
- Authentication and session management (OIDC/PKCE flow, session cookies, CSRF protection)
- Personal API tokens (`styr_pat_...`): generation, hashing, scope and revocation
- Permission routing between the Claude Code CLI and the approvals inbox — in particular, any
  way a tool call could run without going through Claude Code's own permission system
- The Codex harness's sandbox policy — any way a Codex session could obtain
  `danger-full-access`, write outside its assigned sandbox, or otherwise act as though a bypass
  flag had been passed (see [A statement on permissions](#a-statement-on-permissions) below)
- Per-user `HOME` isolation for both harnesses (`data_dir/users/<id>`) — any way one user's
  Claude or Codex CLI state, credentials or transcripts could leak into another user's session
- Session visibility rules (a user seeing or acting on a session they should not have access
  to), including the `viewer` role and a shared workspace's `listed` access allowlist
- Backup files (`styr backup`): confirming a `.tar.gz` archive never carries a secret —
  `secret_key`, `dev_user`, an OIDC `client_secret`, `/etc/styr/env`, any Claude token or Codex
  key, and any user's `HOME` are all excluded on purpose (see
  [docs/OPERATIONS.md](docs/OPERATIONS.md#backup))
- The install script and Docker image (supply-chain integrity, default configuration)

Out of scope (for now, given Styr's homelab-scale status): denial-of-service reports against a
single-operator deployment, and issues that require an attacker to already have admin access or
shell access on the host.

## A statement on permissions

Styr never bypasses either harness's own permission system, and never passes a bypass flag to
either CLI, in any code path, including unattended runs:

- **Claude Code.** Styr never passes Claude Code's own bypass flag or an equivalent bypass
  mode — every tool call a session makes still goes through the same permission prompts you
  would see running Claude Code directly; Styr only routes those prompts to its approvals inbox
  instead of a terminal.
- **Codex.** The Codex CLI has no per-command approval channel at all: a session's permissions
  are the `--sandbox` policy (`read-only` or `workspace-write`) Styr chooses from the session's
  profile when the process starts. Styr never passes Codex's own bypass flags, and never
  requests `danger-full-access`. Because a `workspace-write` turn runs as the same OS user as
  the rest of Styr, it can *read* anything that user can read on the host, not just the
  session's own workspace — the sandbox constrains writes, not reads — which is why each user's
  Claude and Codex state lives under its own `HOME` (`data_dir/users/<id>`) rather than a
  shared one. See [docs/HARNESSES.md](docs/HARNESSES.md) for the full approval-model difference
  between the two harnesses, and `internal/harness/codex/testdata/PROTOCOL.md` for how this was
  verified against the real CLI.

If you find a path where a tool call executes without going through the applicable permission
system above — including a Codex turn reading or writing outside what its sandbox policy should
allow — please report it as above; it is treated as a security issue, not a bug.
