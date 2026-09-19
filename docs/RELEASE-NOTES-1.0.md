# Styr v1.0.0 — "Boring and durable"

Styr runs your Claude Code and Codex agents: start them from a browser, watch what they do,
approve what matters from your phone, and keep every decision on your own box.

## What Styr is

Styr is a small, self-hosted control plane that sits in front of one or more agentic coding
CLIs. It drives the Claude Code CLI with your own `claude setup-token`, so sessions bill your
existing plan instead of metered API usage; as of v1.0 it also drives the OpenAI Codex CLI, on
a pasted API key. Every tool call a session makes still goes through that CLI's own permission
system — Styr never bypasses it — Styr just moves the prompt from a terminal you have to be
sitting at into an inbox you can act on from your phone. Sessions, triggers, runs, schedules and
pipelines are all reachable over a documented, versioned REST and SSE API, and the whole thing
ships as one Go binary with an embedded React frontend and a SQLite database: `styr serve`, or
the Docker image, and you're running.

## Why it exists

Coding agents are useful unattended, not just while you're staring at a terminal: a Grafana
alert can start an investigation, a cron schedule can run a template every morning, a webhook
can kick off a whole pipeline of chained agents. But someone still has to review a diff, decide
a permission prompt, or read a report — and that someone is rarely sitting at the machine that
started the run. Styr exists to be the thing between "an agent wants to do something" and "a
human is asleep, at work, or just not at their desk": a private, self-hosted surface — sessions,
approvals, review, cost — reachable from a browser or a phone, with nothing about what you're
working on or what you asked an agent to do ever leaving your own box.

## What's proven

- The real backend — the full stack, driving the fake Claude Code and Codex CLIs rather than a
  mocked frontend — is exercised end to end by the `real-desktop`/`real-phone` Playwright suite
  before every release: login, sessions, approvals, triggers, runs, review, schedules, loops,
  pipelines, and now a harness switch and the viewer role.
- Styr runs as the author's own homelab deployment — the audience it was built for — driving
  real Claude Code sessions against real repositories, with alerts landing as unattended
  investigations and diffs reviewed and merged from a phone.
- The API is frozen at `1.0.0` and, within the `1.x` line, changes only additively; a CI guard
  (`hack/openapi-compat`) diffs every change against the previous release and fails the build on
  a removed field, a retyped field, or a newly required one. See
  [docs/API.md](API.md#compatibility-promise).
- Backup and restore are both real, online operations: `styr backup` takes a no-downtime SQLite
  snapshot while the server keeps serving requests, and `styr restore` refuses to run against a
  live server or a schema newer than the binary doing the restoring supports.

## What's not

- There are no external users yet. Styr has exactly one deployment's worth of real-world
  mileage behind it, and the compatibility promise above is a promise going forward, not a
  track record yet.
- Codex sessions have no per-command approvals — the CLI itself has no host-side approval
  channel, only a sandbox policy chosen when the session starts — so a Codex session is a
  coarser trust decision than a Claude Code one. See
  [docs/HARNESSES.md](HARNESSES.md#approvals).
  A `workspace-write` Codex turn can also *read* anything the host OS user Styr runs as can
  read, not just the session's own workspace, which is why each user's harness state lives
  under its own isolated `HOME`.
  See [SECURITY.md](../SECURITY.md).
- Codex sign-in is a pasted OpenAI API key, not a `codex login` under your own Styr account —
  headless device auth was deliberately not attempted for this release.
- `styr doctor` and the installer check the `claude` binary but not yet `codex`; an unreachable
  Codex CLI shows up as unavailable in the harness picker instead of a `doctor` warning.
- No load testing beyond a single operator's homelab scale, and no multi-node story: one `styr`
  process, one SQLite database, one host.

## How to install

Bare host, systemd:

```bash
curl -fsSL https://github.com/jonasthim/styr/releases/latest/download/install.sh | sudo bash -s -- --listen 0.0.0.0:8080
```

Or Docker:

```bash
cp deploy/compose.yaml .
cp deploy/config.example.yaml config.yaml   # edit base_url and oidc
docker compose -f compose.yaml up -d
```

Either way: edit `/etc/styr/config.yaml` (or `config.yaml`) to set `base_url` and an OIDC
provider, restart the service, and open `base_url` in a browser — the first user to log in
becomes admin. See [deploy/README.md](../deploy/README.md) for the full walkthrough, including
upgrading an existing install (take a `styr backup` first — one command, no downtime).

## Four things you can do in five minutes

1. **Start a session.** Pick a workspace, a profile, and a harness (Claude Code or Codex), type
   a prompt, and watch it stream in real time.
2. **Approve a permission prompt from your phone.** Open the inbox on any device signed in with
   your account — no terminal, no SSH session, just a page.
3. **Review a diff and open a PR.** On a worktree-enabled workspace, open the session's Review
   tab, read the diff inline, and hit Commit or Open PR straight from the browser.
4. **Point a webhook at a trigger.** Wire a Grafana alert, a GitHub event, or a generic POST at
   `/hooks/{slug}`, and watch an unattended investigation land on the Runs page with its
   structured report and its cost.

See [README.md](../README.md) for the full feature list release by release, and
[docs/HARNESSES.md](HARNESSES.md), [docs/API.md](API.md) and
[docs/OPERATIONS.md](OPERATIONS.md) for the detail behind this release specifically.
