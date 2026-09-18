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

- Claude token storage and handling (encryption at rest, never returned by the API, per-user
  isolation)
- Authentication and session management (OIDC/PKCE flow, session cookies, CSRF protection)
- Permission routing between the Claude Code CLI and the approvals inbox — in particular, any
  way a tool call could run without going through Claude Code's own permission system
- Session visibility rules (a user seeing or acting on a session they should not have access to)
- The install script and Docker image (supply-chain integrity, default configuration)

Out of scope (for now, given v0.1's status): denial-of-service reports against a
single-operator, homelab-scale deployment, and issues that require an attacker to already have
admin access or shell access on the host.

## A statement on permissions

Styr never bypasses Claude Code's own permission system. It never passes
`--dangerously-skip-permissions` or an equivalent bypass mode to the CLI, in any code path,
including unattended runs — every tool call a session makes still goes through the same
permission prompts you would see running Claude Code directly; Styr only routes those prompts to
its approvals inbox instead of a terminal. If you find a path where a tool call executes without
going through that permission system, please report it as above — it is treated as a security
issue, not a bug.
