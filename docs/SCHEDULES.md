# Schedules and loops

v0.4 keeps things going without a human in the loop (so to speak) two ways: a **schedule** fires
a template run on a cron cadence, and a **loop** repeats a template's run on the same session
until its structured report says it's done. Both reuse v0.2's run engine (`internal/runs`) and
templates (`internal/templates`) — a schedule or a loop is just another way to start a run, not a
separate execution path. This doc covers setup and the exact semantics; `internal/schedules`,
`internal/runs/loops.go` and `internal/stats` are the source of truth if anything here and the
code disagree.

## Schedules

A schedule is a cron expression bound to a template (`/schedules`, rail item with a clock icon).
`internal/schedules` owns the table, computes each schedule's next run, and ticks every 30
seconds to fire due ones through the same `runs.Engine.Start` a webhook trigger uses — a
schedule's runs show up on the Runs page exactly like a trigger's, with origin `schedule`.

### Cron syntax

Schedules accept the standard 5-field cron expression — `minute hour day-of-month month
day-of-week` — with **no seconds field** (`github.com/robfig/cron/v3`, parsed with
`cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor`), plus the descriptor forms:
`@hourly`, `@daily`/`@midnight`, `@weekly`, `@monthly`, `@yearly`/`@annually`, and `@every
<duration>` (e.g. `@every 1h30m`). `POST /schedules/preview {cron}` validates an expression and
returns its next 5 fire times (from the server's clock) plus a **human-readable description**,
hand-written for the common shapes the create/edit dialog shows live as you type:

- `@hourly` → "every hour"; `@daily`/`@midnight` → "every day at 00:00"; `@weekly` → "every
  Sunday at 00:00"; `@monthly` → "on the 1st of the month at 00:00"; `@yearly`/`@annually` →
  "once a year on Jan 1 at 00:00"; `@every 1h30m` → "every 1h30m".
- `* * * * *` → "every minute"; `*/N * * * *` → "every N minutes"; `M * * * *` → "every hour at
  :MM"; `M H * * *` → "every day at HH:MM"; `M H * * D` → "every &lt;weekday&gt; at HH:MM" (day
  0 and 7 both mean Sunday).
- Anything else (ranges, lists, steps outside the shapes above) falls back to showing the raw
  cron expression — the description is a convenience, not a full cron-English translator.

There is no per-schedule timezone: every schedule's cron is evaluated against **the server's own
local time** (whatever `TZ` `styr serve` runs under), and the UI shows next/last run times in
that same timezone with no per-user override. Run a UTC server if you want predictable times
across operators in different timezones.

### The 30 s tick and overlap skipping

A single goroutine (`Schedules.Run`, started alongside the run engine's own loop in
`cmd/styr/serve.go`) ticks every 30 seconds (`tickInterval`) and loads every enabled schedule
whose `next_run_at` is at or before now. Only one scheduler runs, in the same process that
happens to hold `styr serve` — there is no leader election or distributed lock, matching the
single-binary, single-process design (ADR-003).

For each due schedule, one tick does exactly one of three things:

- **Started** — the schedule's previous firing did not leave a run still `running` (or has no
  prior run at all): `runs.Engine.Start` is called with the schedule's template and vars, and a
  `schedule_firings` row is written with status `started` and the new run's id.
- **Skipped (overlap)** — the run this schedule last started is still `running`: nothing is
  started this tick, and a firing row is written with status `skipped_overlap`. **A schedule
  never runs two of its own instances concurrently** — this is the whole point of the overlap
  check, since a slow run (a large investigation, a stuck approval) must not pile up a queue of
  duplicate sessions.
- **Failed** — starting the run itself errored (e.g. the template's workspace or profile is now
  invalid): a firing row is written with status `failed` and a reason.

Whatever happened, the schedule's `next_run_at` is recomputed from its cron and `last_run_at`/
`last_outcome` are stamped. The firings log (`GET /schedules/{id}/firings`, shown in a drawer per
schedule) is therefore a complete history of every tick's decision, not just the runs that
actually started — it's how you tell "this schedule hasn't fired in 3 days" apart from "this
schedule has been skipping every tick because the previous run never finishes."

**Run now** (`POST /schedules/{id}/run`, a button in the table) fires immediately regardless of
cron or `next_run_at`, and — unlike a tick — does **not** check for an overlapping run; it always
starts one.

### Vars and the injected `schedule` variable

A schedule stores its own `vars` (a JSON object, edited as JSON in the create/edit dialog) that
are merged into the template's render context on every firing, exactly like a webhook trigger's
payload-derived vars. On top of whatever the schedule's own vars contain, every firing injects a
`schedule` key:

```json
{ "schedule": { "name": "<schedule name>", "fired_at": "<RFC3339 UTC timestamp>" } }
```

so a template can reference `{{.schedule.name}}` or `{{.schedule.fired_at}}` in its prompt or
title without the operator having to pass them by hand. `vars` must decode to a JSON object (an
empty value defaults to `{}`); anything else is rejected at create/update time as a 422, since the
tick always needs an object to merge `schedule` into.

### Visibility

A schedule is owned by its creator by default; an admin may create (or edit) a schedule as
**shared** (owner `null`), visible and runnable by everyone — the same ownership rule templates
and triggers already use (`visibleOwner`/`mutableBy` in `internal/schedules/schedules.go`):
admins see and can change every schedule, a member sees and can change their own plus every
shared one, and cannot touch another member's personal schedule at all.

## Loops

A loop repeats a template's run **on the same Claude Code session** until its structured report
says the work is done, or its iteration budget runs out. Unlike a schedule (which starts a brand
new session every firing), a loop's iterations all share one conversation, so each iteration sees
every previous one in the model's own context.

### Enabling a loop on a template

The template editor's **Loop** section sets two fields, stored on the template itself:

- `loop_until` — the name of the report field a loop watches. Empty (the default) means the
  template does not loop at all; every run of it is a plain one-shot run. The field is picked
  from the template's report JSON schema's boolean fields in the UI, or typed free-hand; if a
  loop's report ever omits the field entirely, that reads as "not done yet" (see below), it does
  not error.
- `loop_max` — the most iterations a loop may run in total. `0` (unset) falls back to a default
  of **5 iterations**.

Any run of a looping template — from a webhook, a schedule, "Run now" on the template, or the
loop's own next-iteration send — becomes iteration 1 of a new `loops` row (`runs.Engine.startLoop`
in `internal/runs/loops.go`). A one-shot template is completely unaffected; loops only exist for
templates that opt in.

### The truthiness rule

When a run's report arrives, the loop advances by checking `report[loop_until]`
(`internal/runs/loops.go`'s `loopDone`/`truthy`):

- **Missing entirely** → not done, keep looping.
- `false`, `0` (number or the string `"0"`), `""`, or the case-insensitive strings `"no"`, `"n"`,
  `"off"` → not done, keep looping.
- Any other value — `true`, a non-zero number, a non-empty string that isn't one of the negative
  words above, a non-empty array or object — counts as **done**.

So a report of `{"done": true}`, `{"done": "yes"}` or `{"done": 1}` all end the loop; `{"done":
false}`, `{"done": ""}`, `{"done": "no"}` and a report with no `done` key at all all continue it.

### The iteration prompt

When the report says "not done" and the loop still has budget left, the engine re-renders the
template's prompt against the loop's original variables plus a `loop` variable:

```json
{ "loop": { "iteration": <next>, "max": <loop_max>, "previous_report": <decoded previous report> } }
```

and sends it as the session's next turn with this **exact prefix**, prepended to the rendered
prompt:

```
Iteration N of M. Previous report: <the previous report, compact JSON, truncated to 4000 characters>

<the template's rendered prompt>
```

`N`/`M` are the new iteration number and the loop's `loop_max`. The previous report is JSON-
compacted (whitespace stripped) and capped at 4000 characters (`reportPreviewLimit`) so one
oversized report can't fill the context on its own — the full, untruncated report still lives on
the previous iteration's own run row, viewable from the loop's page.

### Same session, one cost line

Every iteration is sent with `sessions.Send` on the **same session id** the loop's first run
created — there is no `--resume` under a new session, no fresh process, no new conversation. This
is deliberate: it's what lets the model see its own previous attempt's tool calls and report
inline rather than being told about them second-hand, which matters for a repeat-until-correct
loop (fix the tests, keep going until they're green). The cost implication follows directly:
**a loop's cost accrues on one session**, the same way a long interactive session's cost does —
there is no per-iteration cost line, only the session's running total. The cost dashboard and the
loop page both show that one session total, not N separate charges.

### States

A loop is always in exactly one of:

| State | Meaning |
|---|---|
| `running` | An iteration is in flight, or between two iterations. |
| `done` | The watched report field turned truthy. Terminal. |
| `exhausted` | `loop_max` iterations ran and none reported done. Terminal. |
| `failed` | An iteration's run did not finish `success` (it failed, timed out, or the engine could not send the next iteration's prompt at all). Terminal. |
| `stopped` | An operator stopped the loop by hand. Terminal. |

Every state but `running` is terminal (`domain.LoopState.Terminal()`): once a loop leaves
`running` nothing advances it further, even if you could somehow coerce a later report to look
truthy.

### Stop

**Stop** (`POST /loops/{id}/stop`, on the Loops page — a tab next to Runs) ends a running loop
immediately: it's recorded `stopped` and its underlying session is **closed**, so whatever
iteration was in flight (or about to start) stops costing anything right away rather than running
to completion first. Stopping a loop that is not `running` (already terminal) is a 409 conflict —
there's nothing left to stop.

### Loop variables are held in memory only

The variables a loop's first iteration was started with (a schedule's `vars`, a webhook's
rendered payload, whatever was passed to "Run now") are **not persisted anywhere** — they live
only in an in-process cache (`internal/runs/loops.go`'s `varsCache`) keyed by loop id, for as
long as the `styr serve` process that started the loop keeps running. The `loops` table itself
holds no payload column at all; a loop only outlives the process that started it as history (its
row, its runs, its reports).

**This means a restart mid-loop loses those variables.** If `styr` restarts while a loop is
`running` (a deploy, a crash, a reboot), the loop's row survives in SQLite, but the next time an
iteration is rendered — which won't happen automatically, since nothing resumes a `running` loop
across a restart on its own — the cached vars are gone, so the prompt template renders with those
variables **empty** rather than erroring. In practice a loop stuck `running` across a restart is
effectively abandoned; check the Loops page after any restart for loops still showing `running`
and stop them by hand if they aren't going to be driven further.

## The fleet Gantt

The Sessions page's **Gantt** view (`/sessions?view=gantt`, a segmented control next to the list
view) shows one horizontal lane per session, active over a chosen window (`1h`, `6h` or `24h`),
with each lane's time broken into coloured segments — this is the "what is the whole fleet doing
right now" view a bunch of schedules and loops running unattended makes useful. It's computed
fresh on every request (`GET /stats/gantt?from=&to=`, `internal/stats.Service.Gantt`) — there is
no background aggregation or stored rollup, so the numbers are always exactly as current as the
underlying events table.

### How segments are derived

A lane's segments come from replaying that session's own event log
(`internal/stats/gantt.go`'s `busySegments`) as a small state machine, one open segment at a
time:

- a `user` event closes whatever segment is open and opens a new **running** segment — a
  user/operator turn always starts running time;
- a `permission_request` event closes the open segment (typically running) and opens a
  **waiting** segment;
- a `tool_use` or `text` event closes an open **waiting** segment and reopens **running** — this
  is how the approval being answered shows up, since approvals themselves aren't events;
- a `result` or `exit` event closes whatever segment is open, and the session goes quiet until
  its next `user` event.

Any segment still open when the state machine runs out of events is closed at the window's end,
so a turn still in progress (or a session still waiting on an approval right now) shows as
running/waiting all the way to the live edge instead of silently disappearing.

### The waiting approximation

**Waiting is approximated, not measured directly.** Styr's event log has no "approval answered"
event of its own — an approval is a derived row, not a bus event — so the Gantt infers that
waiting ended at whatever the *next* `tool_use`/`text` event's timestamp is, which in practice is
very close to (but not exactly) the moment a human tapped Allow. A session that sits waiting and
is never answered (timed out, denied) has its waiting segment simply closed at the window edge
like any other still-open segment.

### Idle gaps and the events lookback

Between busy segments — and between a window's edges and the nearest segment — the Gantt fills in
**idle** time, but only for gaps of **5 seconds or longer** (`minIdleSegment`); anything shorter
is folded into whatever's adjacent rather than rendered as its own sliver. To reconstruct a
segment that was already open before the requested window starts (a turn that began an hour ago
and is still running), Gantt actually reads events from **one hour before** `from` (`ganttLookback`)
even though only the `[from, to]` portion is ever rendered — a segment that's genuinely still
open shows as running/waiting from the window's start, not as a gap followed by a sudden jump
into a busy segment.

### The 200-session cap and windows

A Gantt request selects every session visible to the caller (the same visibility rule as the
Sessions list: your own plus every unattended session; admins see all) whose `last_active_at`
falls on or after `from` and `created_at` falls on or before `to`, sorted by `last_active_at`
descending, and **caps the result at 200 lanes** — the busiest 200 sessions in the window, not an
arbitrary 200. `from`/`to` default to the last 6 hours when omitted, and the endpoint refuses a
window wider than **7 days** (422) — a fleet Gantt over events is proportional to how many events
that window covers, so an unbounded window would let one request read the whole events table. The
UI's own window control only ever asks for 1h/6h/24h, well inside that ceiling.

## The cost page

`/runs/costs` (a tab under Runs, next to the Loops tab) is the whole-fleet spend view: stat tiles
for today/7d/30d, a per-day bar chart, and breakdown tables by owning user, by origin (`ui`,
`webhook`, `schedule`, `loop`, `pipeline`), and the top templates by spend.

### What the numbers mean

The dollar figures are **not a real charge** — Styr drives the Claude Code CLI on your own Max
plan (ADR-001), so nothing here is metered API billing. Instead, every number is the CLI's own
`total_cost_usd` figure from the `result` envelope of each turn (see ADR-002), summed per session
and per run — i.e. **what the same work would have cost at API list pricing, reported by the CLI
itself while actually running on your subscription plan.** It's a useful proxy for "how much
compute did this cost", not an invoice; the UI's own copy calls this out as "API-equivalent cost"
so it's never mistaken for a real bill.

### Windows

`GET /stats/costs?days=` aggregates over a trailing window of days, defaulting to **30** and
capped at **365**; the per-day series, the day tiles (today/7d/30d) and the top-5-templates
breakdown are all slices of that same window rather than separate queries.

### Visibility for members vs admins

Costs reuse the same visibility rule as the sessions list rather than a separate one: a **member**
sees the cost of their own sessions plus every unattended session (schedules, loops, webhook
triggers — nobody's personal session, so visible to all, same as the Runs page); an **admin** sees
every session's cost, including every other member's personal sessions. There is no per-user
opt-out — anyone who can see a session's existence can see what it cost.
