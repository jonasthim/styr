# Triggers

v0.2 turns an inbound webhook — a Grafana alert, a generic JSON payload, a GitHub event — into
an unattended Styr session: render a prompt from the payload, run it under a profile with no
human watching, and land the result on the Runs page (and, optionally, in ntfy or an outbound
webhook). This doc covers setup, the rendering pipeline, and the operational and security
semantics; `internal/templates`, `internal/triggers` and `internal/runs` are the source of truth
if anything here and the code disagree.

## Concepts

- **Template.** What a run does: a workspace, a profile, a prompt (Go `text/template`, rendered
  against the payload), an optional title template, an optional appended system prompt, and an
  optional JSON report schema. Templates can be personal or shared (owner `null`, admin-managed);
  a fresh install seeds one shared template, "Grafana alert investigation" (see below). Templates
  live under Triggers → Templates in the UI (`/triggers?tab=templates`).
- **Trigger.** An inbound endpoint, `POST /hooks/{slug}`, bound to one template. A trigger has a
  kind (`generic`, `grafana` or `github`), a bearer secret, and the dedupe/cooldown/storm-cap
  settings below. Creating a trigger picks (or generates) its slug and mints its secret.
- **Delivery.** One logged call to a trigger's webhook URL — accepted, deduped, in cooldown,
  storm-capped, rejected (bad payload), failed (an error starting the run) or skipped (a resolved
  alert the trigger doesn't want). Every inbound call produces exactly one delivery row, whatever
  the outcome, so you can always see why an alert did or didn't start a session. `GET
  /triggers/{id}/deliveries` lists them; the Triggers page shows them in a drawer per trigger.
- **Run.** The unattended session a delivery started (or a replay/test triggered): which
  template, trigger and delivery caused it, when it started and finished, its outcome, its
  structured report, a one-line summary and its cost. Runs are visible to every signed-in user
  regardless of who owns the underlying session — they're shared infrastructure, like other
  unattended sessions (see `docs/ARCHITECTURE.md`'s visibility rules). The Runs page is `/runs`,
  each run's detail page is `/runs/$id`.

## Setting up a Grafana contact point

1. In Styr, go to **Triggers → New trigger**, pick kind **Grafana**, name it, and point it at a
   template (the seeded "Grafana alert investigation" template works out of the box, or pick your
   own). Save.
2. Styr shows the webhook URL and the secret **once** — copy both now; only the secret's hash is
   kept from here on. For a Grafana trigger the create dialog also shows a ready-to-paste
   contact-point snippet (URL plus the `Authorization` header).
3. In Grafana: **Alerting → Contact points → + Add contact point**. Choose integration
   **Webhook**.
4. Set:
   - **URL**: `https://<your-styr-base-url>/hooks/<slug>` (the slug Styr generated from the
     trigger's name, e.g. `/hooks/grafana-alerts`).
   - **HTTP Method**: `POST`.
   - **Authorization header** → **Scheme**: `Bearer`, **Credentials**: the secret Styr showed
     you. (Grafana webhook contact points support a custom header instead; either works, since
     Styr accepts the secret as `X-Styr-Secret`, `Authorization: Bearer <secret>`, or a
     `?secret=` query parameter — see Security below.)
5. Save, then use Grafana's "Test" button on the contact point — Styr logs the test call as a
   delivery even if no route sends real alerts to it yet, so you can confirm the secret works
   before wiring up a notification policy.
6. Point an alert rule's notification policy at the new contact point. Grafana's webhook
   integration posts the Alertmanager-compatible payload documented under **Template
   variables** below; Styr's `grafana` normalisation expects exactly that shape.

Grafana re-sends the same alert on every evaluation while it's firing and again once when it
resolves; dedupe, cooldown and the storm cap (below) are what keep that from starting a session
per evaluation.

## Generic webhook and GitHub setup

**Generic** (kind `generic`): create a trigger, point anything that can POST JSON at
`https://<base>/hooks/<slug>` with either `X-Styr-Secret: <secret>` or
`Authorization: Bearer <secret>`. The whole decoded body is available to the template as
`.payload`.

**GitHub** (kind `github`): GitHub webhooks cannot send a custom `Authorization` header value you
control, so the documented setup uses the query-string secret instead:

1. Create a trigger with kind **GitHub**, pointed at a template.
2. In the GitHub repo (or org) **Settings → Webhooks → Add webhook**, set the **Payload URL** to
   `https://<base>/hooks/<slug>?secret=<secret>` and leave GitHub's own **Secret** field empty —
   Styr's secret carried in the URL is the only authentication (see Security below, and ADR-010
   in `docs/DECISIONS.md`). **Content type**: `application/json`. Pick the events you want
   (`Pull requests`, `Issues`, etc.).
3. GitHub's normalisation lifts `action`, `repo` (`repository.full_name`), `sender`
   (`sender.login`) and `number` (`pull_request.number` or `issue.number`) to the top level of
   the template variables, alongside the full payload as `.payload`.

## Template variables and functions

Templates are Go `text/template` (`missingkey=zero`: a missing map key renders as empty rather
than `<no value>`). Every kind exposes `.payload` (the fully decoded JSON body); `grafana` and
`github` additionally lift these fields to the top level:

| Kind | Extra variables |
|---|---|
| `grafana` | `.status` (`firing`\|`resolved`), `.alerts` (list of `{status, labels, annotations, startsAt, endsAt, fingerprint, generatorURL, silenceURL, dashboardURL, panelURL, values}`), `.commonLabels`, `.commonAnnotations`, `.groupLabels`, `.title`, `.message`, `.externalURL` |
| `github` | `.action`, `.repo` (`repository.full_name`), `.sender` (`sender.login`), `.number` (`pull_request.number` or `issue.number`) — only when present in the payload |
| `generic` | `.payload` only |

Functions available in every rendered template (prompt, title and dedupe key alike):

| Function | Signature | Behaviour |
|---|---|---|
| `lower` | `lower s` | lowercase |
| `upper` | `upper s` | uppercase |
| `join` | `join sep list` | joins a `[]string` or `[]any` with `sep` |
| `default` | `default fallback value` | `fallback` when `value` is nil/empty, else `value` |
| `truncate` | `truncate n s` | first `n` runes of `s` |
| `json` | `json v` | `v` marshalled to a JSON string |
| `now` | `now` | current time, RFC3339 UTC |
| `sha256` | `sha256 s` | hex SHA-256 digest of `s` |
| `alertnames` | `alertnames .alerts` | `[]string` of `.labels.alertname` from a Grafana alert list |

The seeded **"Grafana alert investigation"** template (workspace-bound on first install, profile
`investigate`) renders:

```
A Grafana alert is {{ .status }}. Investigate read-only and report.
{{ range .alerts }}- {{ .labels.alertname }} on {{ default "unknown" .labels.instance }}: {{ .annotations.summary }}
  {{ .annotations.description }} (since {{ .startsAt }})
{{ end }}
Use the workspace's runbooks and only read-only commands. Do not change anything.
```

with title `{{ .status }}: {{ join ", " (alertnames .alerts) }}`, an appended system prompt
("You are investigating a production alert read-only...") and the report schema described under
**Report schema** below. Use **Templates → Render with sample** (or `POST
/templates/{id}/render`) to dry-run a template against a real or sample payload before wiring up
a trigger.

## Dedupe, cooldown and the storm cap

Every trigger has a **dedupe key template**, rendered per delivery; an empty one falls back to
the kind's default:

- `grafana`: `{{ .status }}:{{ range .alerts }}{{ .fingerprint }},{{ end }}` — the same alert in
  the same state (firing or resolved) always renders the same key, so repeated evaluations of a
  still-firing alert keep deduping indefinitely.
- every other kind: `{{ json .payload | sha256 }}` — a hash of the whole normalised payload, so
  an identical body dedupes but a changed one doesn't.

A delivery is gated, in order:

1. **Resolved skip.** A `grafana` trigger with `run_on_resolved` off (the default) skips a
   `resolved` alert outright — logged with status `skipped`, reason `resolved`. See below.
2. **Cooldown.** If the same dedupe key had an accepted delivery within `cooldown_s` (default
   **600s / 10 minutes**), the new one is logged as `cooldown` and no run starts.
3. **Dedupe.** Past the cooldown, an identical key is still deduped: for `grafana`, always (the
   key already encodes status and fingerprint); for other kinds, within a rolling **24 hour**
   window of the last accepted delivery with that key. Logged as `deduped`.
4. **Storm cap.** If neither of the above applies, the trigger's accepted-delivery count in the
   last hour is checked against `storm_cap_per_hour` (default **10**); at or past it, the
   delivery is logged as `storm` and no run starts.

Only past this gate does a delivery start a run (`accepted`). A malformed payload for the
trigger's kind is logged as `rejected` before dedupe is even computed; a run that fails to start
(e.g. the template's workspace or profile was deleted) is logged as `failed` after the delivery
row already exists.

`POST /triggers/{id}/test` (and a delivery's `POST /deliveries/{id}/replay`) can pass `force:
true` to skip cooldown, dedupe and the storm cap — but never the resolved-alert skip, so a
trigger that opted out of resolved alerts still means it even under a forced test.

## Resolved alerts and `run_on_resolved`

Grafana sends a `resolved` payload for an alert that stops firing, with the same fingerprint(s)
it fired with. By default (`run_on_resolved: false`) Styr does not start an investigation for
that — there's nothing left to look at, and it would just burn a run. Turn `run_on_resolved` on
for a trigger if you want a session to note that an alert self-resolved (the seeded report
schema's `resolved_itself` field exists for exactly this). Non-Grafana triggers ignore
`run_on_resolved`; the resolved-skip rule only looks at `.status` on a `grafana` payload.

## Run outcomes and the timeout

A run's `outcome` starts `running` and ends one of:

- **`success`** — the session's `result` event carried no error; the report is the CLI's
  structured output (see below) or the model's final text.
- **`failed`** — the session's process died, or its `result` event was flagged an error.
- **`timeout`** — the run was still `running` past the timeout; the engine interrupts and closes
  the session and records this outcome itself.

The run **timeout is a fixed 30 minutes today** (`runTimeout` in `cmd/styr/wire.go`, matching
`runs.DefaultTimeout`) — not yet configurable via `config.yaml`. A sweep checks every minute for
runs older than the timeout.

`needs_human` exists as a run-outcome value in the API and the Runs page's filter chips, but the
run engine doesn't currently set it on a run row: when an unattended session raises an approval,
the run stays `running` — the approval can still be answered, or expire and deny, without ending
it — and the engine instead fires a one-time `run.needs_human` **notification** (see below) so an
operator knows to look. A run only ever settles into `success`, `failed` or `timeout`.

## Report schema and rendering

A template's `report_schema` (JSON Schema) is passed to the CLI as `--json-schema`, which
constrains the model's final turn to a synthetic structured-output tool call (ADR-009 in
`docs/DECISIONS.md`); the run's `report` is that structured output verbatim, or, when the schema
is unset or the model didn't produce one, `{"result": "<the model's final text>"}`. The run's
one-line `summary` is the report's `diagnosis` field when present, else the model's text,
truncated to 200 characters.

The seeded Grafana template's schema requires `severity` (`info`\|`warning`\|`critical`),
`diagnosis`, `proposed_action` and `confidence` (0–1), and allows `evidence` (a string list) and
`resolved_itself` (bool). The Run detail page's Report card renders whichever of these fields are
present — nothing is required beyond that a template's schema author put it there:

- a severity badge (info/warning/critical, coloured neutral/attention/failed)
- a "Resolved itself" badge when `resolved_itself` is true
- a confidence meter (a filled 0–100% bar) when `confidence` is a number
- the `diagnosis` text
- an `evidence` bullet list
- the `proposed_action` text

A report that doesn't parse as JSON, or is empty, renders as "No report yet." A template with its
own `report_schema` gets its own fields rendered the same way; fields the schema doesn't define
simply don't appear.

## Notifications

`internal/notify` delivers three run events to admin-configured channels: `run.finished`,
`run.needs_human`, `run.failed`. A channel is `ntfy` or `webhook`, each subscribed to whichever
event kinds it wants (`events`, default `["run.finished", "run.needs_human", "run.failed"]` —
every channel gets everything unless narrowed). An optional bearer token is sealed at rest with
the same `crypto.Box` that encrypts Claude tokens, and never returned by the API once set.

- **ntfy**: `POST <url>` with the event body as the plain-text request body and headers `Title`,
  `Priority` (1–5; `run.needs_human`/`run.failed` send 4, others 3), `Tags` (comma-separated ntfy
  emoji tags: `raised_hand,styr` for needs-human, `rotating_light,styr` for failed,
  `white_check_mark,styr` otherwise), `Click` (a deep link to `<base_url>/runs/<id>`), and
  `Authorization: Bearer <token>` when a token is set.
- **webhook**: `POST <url>` with a JSON body `{"kind", "title", "body", "url", "priority",
  "tags", "sent_at"}` and header `X-Styr-Event: <kind>`; `Authorization: Bearer <token>` when a
  token is set.

Every attempt is bounded by a 10s timeout with one retry on a network error or 5xx response; a
notification failure is logged and never affects the run itself. Settings → Notifications lets an
admin add a channel and send a test event (`POST /notifications/{id}/test`) without waiting for a
real run.

## Replay and test payloads

- **Send test payload** (`POST /triggers/{id}/test`, or the Triggers page's per-trigger action):
  runs the full pipeline — auth is implicit (you're already an authenticated operator), but
  dedupe/cooldown/storm apply unless `force: true` — against a payload you supply, or the kind's
  realistic sample when you don't (`GET /triggers/samples/{kind}`, also what "Render with sample"
  on the Templates page uses).
- **Replay** (`POST /deliveries/{id}/replay`, the Runs and Triggers pages' "Re-run"/replay
  action): re-renders a past delivery's exact stored payload and starts a fresh run, always
  bypassing dedupe, cooldown and the storm cap (but not the resolved-alert skip). The replay is
  logged as its own new delivery row pointing back at the one it replays — the original delivery
  is never modified.

## Security notes

- **Secrets are shown once.** A trigger's webhook secret (`styr_whs_` + 32 random bytes,
  base64url) is returned only in the create response and in `POST
  /triggers/{id}/rotate-secret`'s response. From then on only a `secret_hint` (its last six
  characters) is ever exposed.
- **sha256 at rest.** Styr stores only the secret's sha256 hash, never the secret itself (ADR-010
  in `docs/DECISIONS.md`) — the same scheme as personal API tokens. An inbound secret is compared
  against the hash in constant time. It may arrive as the `X-Styr-Secret` header, an
  `Authorization: Bearer <secret>` header, or a `secret` query parameter (needed for senders,
  GitHub included, that can't set custom headers). Because storing only a hash rules out
  verifying GitHub's own `X-Hub-Signature-256` HMAC (which needs the raw secret as the MAC key),
  a `github` trigger authenticates with the same bearer-secret check as every other kind — its
  body is not otherwise integrity-checked, so its URL (secret and all) is as sensitive as the
  secret itself. Rotate a trigger's secret if its URL may have leaked.
- **Unattended sessions run under the service token.** A run's session always has `owner_user_id
  = null` and uses the admin-configured service token (`docs/CONFIGURATION.md` → Tokens), never
  a triggering user's own credential — there isn't one; the caller is a webhook. It runs under
  the profile the template names (its permission mode, tool allow/deny lists, and — from v0.2 —
  its default model and effort).
- **Approvals page every member's inbox.** If an unattended session's profile still needs a
  permission decision the profile itself doesn't cover, the request behaves exactly like any
  other approval: it shows up in the inbox of every signed-in member (unattended sessions are
  visible to everyone, not just admins — see `docs/ARCHITECTURE.md`'s visibility rules), and, if
  left unanswered past `approval_timeout` (`config.yaml`, default 30 minutes), it **expires and
  defaults to deny** rather than blocking the run forever.
