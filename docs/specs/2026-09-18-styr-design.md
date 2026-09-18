# Styr: open-source mission control for Claude Code agents

Part 1 is the product spec. Part 2 is the v0.1 implementation plan written as self-contained
task cards for delegation to cheaper models. When the repo exists, copy Part 1 to
`docs/specs/2026-09-18-styr-design.md` and Part 2 to
`docs/superpowers/plans/2026-09-18-styr-v0.1.md` (Task 1 does this).

---

# Part 1: Spec

## Context

Jonas wants a self-hosted web UI that spawns Claude Code sessions, orchestrates work between them
(pipelines, loops, schedules) and starts sessions from webhooks such as Grafana alerts. This is
an **open-source product with a best-in-class UI**, aimed at self-hosters and homelab people,
MIT licensed, "mission control" design direction, Claude Code first behind a harness adapter.

Name: **Styr** (Swedish: steer, control; as in *styrman*, helmsman). Binary `styr`, module
`github.com/jonasthim/styr`.

### Why this can win (landscape survey, 2026-09-18)

- The OSS field is dead or dying: Vibe Kanban's company shut down (Apr 2026), Crystal deprecated,
  opcode unmaintained since Oct 2025, claude-code-webui archived. claudecodeui and Happy Coder
  remain; neither has triggers, schedules, pipelines or loops.
- **Nobody open source ships a trigger layer (webhooks, cron, DAGs, until-green loops) together
  with Claude Code's real permission prompts.** OSS drivers hard-code skip-permissions; vendor
  clouds have triggers but run unattended, hourly-capped, per-user, and cannot reach a homelab.
- Complaints to design against: binary permissions, quota and cost opacity, the review
  bottleneck, fragility and abandonment, Electron bloat, login walls, default telemetry.

### Hard constraints

- **No API credits.** The Agent SDK is out (its quickstart forbids claude.ai login for SDK-built
  agents). The headless CLI with a `claude setup-token` OAuth token bills the Max plan.
- Drive the CLI only through documented flags (verified on CLI 2.1.276): `-p --input-format
  stream-json --output-format stream-json --include-partial-messages --replay-user-messages
  --session-id --resume --fork-session --name --permission-mode --permission-prompts host|none
  --permission-prompt-tool --allowedTools --disallowedTools --max-turns --max-budget-usd
  --json-schema --add-dir --mcp-config --strict-mcp-config --append-system-prompt --worktree`.
- Never pass `--dangerously-skip-permissions` or `bypassPermissions`. Ever.
- Zero telemetry, no login wall, opt-in update check. Single binary plus a Docker image.

### Decisions log

| Topic | Decision |
|---|---|
| Audience | Self-hosters and homelab people; a handful of users |
| License | MIT |
| Stack | Go backend, single binary, embedded React SPA, SQLite, SSE |
| Design direction | Mission control: dark by default, dense, monospace accents, live activity, glanceable |
| Signature experiences | Approvals inbox on the phone, live agent activity view, diff review before merge, pipeline and loop builder |
| Harnesses | `Harness` adapter interface, Claude Code implementation first |
| Unattended autonomy | Read-only investigate profile plus allowlisted remediation; anything else waits in the inbox with a timeout that defaults to deny |
| Users | OIDC login (PKCE) with user profiles in v0.1; roles admin and member; first user becomes admin |
| Claude token per user | Each user stores their own `claude setup-token` in their profile (encrypted at rest); their sessions bill their plan. Admin sets a separate service token for unattended runs |
| Visibility | Sessions are private to their owner; admins see everything; unattended runs are visible to all members |
| Notifications | In-app inbox first; ntfy and outbound webhook in v0.3 |
| Demo | Screenshots and a video; zero telemetry |

## Product

### Positioning

"Styr runs your Claude Code agents: start them from a browser or a webhook, watch what they do,
approve what matters from your phone, chain them into pipelines, and keep every decision on your
own box." Free, self-hosted, one binary.

### Information architecture

Left rail (icons-only collapsed; bottom tab bar on phone): **Inbox** (badge), **Sessions**,
**Runs**, **Pipelines**, **Schedules**, **Triggers**, **Workspaces**, **Settings**. `Cmd+K`
palette reaches every object and action. `?` shows shortcuts.

1. **Inbox**. Two groups: *Needs you* (permission prompt, agent question, failed run, plan
   awaiting approval) and *FYI* (finished, resolved, scheduled ran). Each item carries the
   session's "now" line, the tool and input, a **risk tier** (read / write / exec /
   destructive), and enough transcript context to decide. Keys: `J/K` move, `A` allow, `D`
   deny, `E` edit input then allow, `O` open session, `S` snooze. Pending count in the tab
   title. This is the phone screen; renders at 390 px first.
2. **Sessions**. Attention-ordered list: blocked on you, running, idle, closed. Each row: title,
   workspace, state glyph, **now line** ("Editing src/auth.ts · 12s"), activity sparkline, diff
   badge, turns and cost, origin.
3. **Session view**. *Transcript* as blocks (each tool call collapses use and result into one
   block with header row: tool, target, duration, outcome; expandable, deep-linkable; text
   streams without layout jump). *Right panel*: Activity (span timeline), Changes (files
   touched), Info (model, profile, context meter, cost, session id). *Composer*: input, `/`
   commands, inline permission card when a prompt is pending.
4. **Diff review** (v0.2). Worktree per session, diff with inline comments that become prompts,
   commit or PR via `gh`.
5. **Runs** (v0.3). Every unattended execution with trigger, duration, outcome, cost, report.
6. **Pipelines** (v0.5). YAML DAG with live graph; loops are repeat-until-done steps.
7. **Schedules** (v0.4). Cron table.
8. **Triggers** (v0.3). Inbound generic JSON, Grafana alerting, GitHub; outbound ntfy, webhook.
9. **Workspaces**. Registered checkouts, default profile, worktree settings.
10. **Settings** (admin). Profiles, harness status, service token, limits, OIDC, users, about.
11. **Profile**. Name and avatar from OIDC, own Claude token (paste, verify, revoke), theme,
    shortcuts, notification preferences.
12. **Login**. One button per provider, dark canvas, the Styr mark. First user becomes admin.

### Design system "mission control"

- Canvas near-black (`#0b0d10`), surfaces as three luminance steps, hairline borders, saturated
  colour only for state: running green, needs-you amber, failed red, scheduled blue. Light theme
  provided; dark default.
- Type: Geist Sans 13–14 px body, weight 500 labels, tight tracking on headings; Geist Mono for
  paths, commands, ids; tabular numerals wherever a number changes.
- Density: 32 px rows, 4/8 px grid. Motion: transform and opacity only, 120–180 ms, reduced
  motion honoured; streaming text never shifts layout.
- Table stakes: `Cmd+K`, full keyboard nav with visible focus, optimistic UI with undo toasts,
  content-shaped skeletons, empty states with one action, WCAG AA, ARIA live regions, deep links,
  responsive to 390 px, three-screen onboarding.

### Release roadmap

| Release | Contents |
|---|---|
| v0.1 Cockpit | OIDC login and profiles, sessions, session view with blocks and activity timeline, inbox with approvals, workspaces, profiles, palette and shortcuts, install script, Docker image, docs |
| v0.2 Triggers (shipped) | Templates, inbound webhooks (generic, Grafana, GitHub), runs and reports, dedupe, cooldown, outbound ntfy and webhook, personal API tokens, per-session model and effort, slash-command menu |
| v0.3 Review | Worktree per session, diff view, inline comments as prompts, commit and PR, plan approval checklist |
| v0.4 Schedules and loops | Cron, until-done loops, fleet Gantt, cost dashboard |
| v0.5 Pipelines | YAML DAG, live graph, fan-out, retries |
| v1.0 | Second harness (Codex CLI), stable API |

## Architecture

```
browser (React SPA) ── HTTPS ── styr (Go) ── stdio JSONL ── claude -p (one process per open session)
                                  ├── SQLite (own event log)    └── <data>/users/<id>/.claude transcripts
                                  ├── in-process bus → SSE
                                  └── harness adapter, sessions service, auth
```

Auth model: OIDC with PKCE only. First user becomes `admin`; others `member`. Sessions belong to
a user; members see only their own; admins see all; unattended sessions (`owner_user_id` null)
are visible to all. A session cannot start unless its owner has a verified Claude token.
Each user gets their own `HOME` under the data dir so CLI state is separate. Every mutation is
recorded in `audit`.

Permission prompts: the CLI is started with `--permission-prompt-tool stdio` (the flag the Agent
SDK itself passes when a `canUseTool` callback is set; Vibe Kanban used the same) so prompts
arrive on stdout as `control_request` messages and answers go back on stdin as
`control_response`. Task 2 records the real shapes into fixtures before the codec is written.

Shared interfaces fixed for later releases: session template {workspace, profile, title
template, prompt template, report JSON schema, appended system prompt}; trigger {name, secret,
template, dedupe key, cooldown, storm cap}; run record {session, origin, template, started,
finished, outcome, report}; loop {template, until field, max iterations}; pipeline {YAML DAG of
templated steps, reports feed later steps, worktree per step}.

---
