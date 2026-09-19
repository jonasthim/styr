# Styr API

Styr's REST and SSE API is documented as an OpenAPI 3.1 document at
[`docs/openapi.yaml`](openapi.yaml) — every operation there has a stable `operationId` and is
tagged by resource. This page is the human-readable overview: how to authenticate, the
conventions every endpoint follows, the event stream, the inbound webhook, a curl walkthrough,
the compatibility promise, and how to regenerate a client from the OpenAPI document.

## Base URL

Every API route is under `/api/v1`, relative to your deployment's `base_url` (see
[docs/CONFIGURATION.md](CONFIGURATION.md)). Two routes sit outside `/api/v1` entirely:
`GET /healthz` and `POST /hooks/{slug}` (the inbound webhook, see below). This walkthrough uses
`http://localhost:8080` throughout; substitute your own `base_url`.

## Authentication

Styr accepts two credentials, checked in this order on every `/api/v1` request:

1. **Session cookie** (`styr_session`) — set by the OIDC login flow
   (`GET /api/v1/auth/login/{slug}` → provider → `GET /api/v1/auth/callback`). This is what the
   web UI uses; a browser carries the cookie automatically. Cookie-authenticated requests that
   are not `GET`/`HEAD`/`OPTIONS` must also carry the header `X-Requested-With: styr` (a CSRF
   guard against cross-site form posts — a plain `<form>` cannot set custom headers). Requests
   authenticated by a personal API token are exempt from this header.
2. **Personal API token** — `Authorization: Bearer styr_pat_...`. Create one from the UI (Profile
   → API tokens) or via `POST /api/v1/me/api-tokens` while already signed in by cookie. The raw
   token is returned exactly once, in that creation response; Styr stores only its SHA-256 hash,
   so a lost token cannot be recovered, only revoked and replaced. This is what scripts, cron
   jobs and this walkthrough use.

A handful of routes need no credential at all: `GET /healthz`, `GET /api/v1/version`,
`GET /api/v1/auth/providers`, the login/callback routes, and `POST /hooks/{slug}` (which
authenticates with the trigger's own secret instead, see below).

Two authorization tiers sit on top of authentication: `member` (default; sees and manages their
own workspaces, sessions and tokens; sees shared and admin-registered resources) and `admin`
(sees and manages everything, plus user roles and server-wide settings). The first user to ever
log in becomes admin automatically.

## Error envelope

Every non-2xx JSON response (except a bare `204`/`202`/`302`) uses the same shape:

```json
{"error": {"code": "not_found", "message": "workspace not found"}}
```

`code` is a short, stable, machine-matchable string (`not_found`, `forbidden`, `invalid`,
`conflict`, `token_invalid`, `name_taken`, `invalid_path`, `invalid_pipeline`, `internal`, ...);
`message` is a human-readable sentence, safe to show in a UI, and never leaks internal detail on
a `500`. See `#/components/schemas/ErrorBody` in the OpenAPI document, and each operation's own
response descriptions for the codes it can return.

Common status codes: `401` not signed in, `403` signed in but not allowed to do this, `404` not
found (or not visible to you — Styr never distinguishes "doesn't exist" from "not yours" to an
unprivileged caller), `409` conflict with current state (e.g. a workspace still cloning, a
session already waiting on an approval), `422` the request body failed validation.

## Pagination

List endpoints that can grow without bound (`GET /api/v1/triggers/{id}/deliveries`,
`GET /api/v1/runs`, `GET /api/v1/pipeline-runs`, `GET /api/v1/schedules/{id}/firings`, ...) take
a single `?limit=N` query parameter, each with its own sensible default (see the operation in
`docs/openapi.yaml`) and no explicit maximum enforced beyond the handler's own cap. There is
deliberately no offset or cursor parameter: every one of these lists is already ordered
newest-first (or, for approvals, oldest-first) and read in full by the UI on each load, so
paging further back has not been needed. If your integration needs a stable window over a
large history, filter by an id you already have (`trigger`, `pipeline`, `loop_id`, ...) rather
than relying on offsets that would shift as new rows arrive.

## SSE event stream

`GET /api/v1/events` is a long-lived Server-Sent Events stream, filtered to events visible to
the caller (their own, owner-less, or — for an admin — everything). Each frame is:

```
event: <kind>
data: <json>

```

with a comment-only `: ping` frame sent every 25 seconds as a heartbeat (ignore lines starting
with `:`). The event kinds:

| kind | payload | when |
|---|---|---|
| `session.event` | a transcript `Event` (same shape as `GET /sessions/{id}/events`) | a new transcript event was appended to a session |
| `session.state` | `{state, ...}` | a session's state changed (running, waiting, idle, closed, ...) |
| `session.stats` | token/cost counters | a turn finished and usage stats updated |
| `approval.created` | an `Approval` | a session is waiting on a new permission decision |
| `approval.decided` | an `Approval` | a pending approval was allowed, denied or expired |
| `workspace.state` | `{workspace_id, state}` | a git-source workspace finished (or failed) cloning |

A client should treat an unrecognized `kind` as forward-compatible and ignore it rather than
erroring — new kinds may be added within the 1.x line (see Compatibility below).

## Inbound webhook

`POST /hooks/{slug}` is the one route a third party (Grafana, GitHub, a generic monitor) calls
directly; it is deliberately outside `/api/v1`; the `X-Requested-With` CSRF header and the
5-minute per-request timeout both do not apply to it. It is unauthenticated by session cookie —
the credential is the trigger's own secret (from `POST /api/v1/triggers` or
`POST /api/v1/triggers/{id}/rotate-secret`, which return the plaintext secret exactly once),
supplied one of three ways:

- header `X-Styr-Secret: <secret>`
- header `Authorization: Bearer <secret>`
- for a `github`-kind trigger, an `X-Hub-Signature-256` HMAC-SHA256 over the raw request body,
  keyed by the secret (GitHub's own webhook signing scheme — no header carries the secret
  itself)

The body is capped at 256 KiB and is not decoded as Styr's own JSON — its shape depends on the
trigger's `kind` (`generic`, `grafana`, `github`), and is handed to the template renderer as-is.
A successful call answers `202` with `{"delivery_id", "status", "run_id"?}`; see
`POST /api/v1/triggers/{id}/test` in the OpenAPI document, which runs the identical pipeline as
an authenticated dry run.

## Curl walkthrough

This creates a workspace, starts a session, sends it a message, watches its events, and
approves a pending tool call. `$TOKEN` is a personal API token (see Authentication above);
`$BASE` is your `base_url`.

```bash
BASE=http://localhost:8080
TOKEN=styr_pat_...

# 1. Create a workspace (an empty git repo Styr manages for you).
WORKSPACE_ID=$(curl -sf -X POST "$BASE/api/v1/workspaces" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "demo", "source": "empty"}' | jq -r .id)

# 2. Create a session on it. profile_id "interactive" is one of the built-in
#    profiles from GET /api/v1/profiles.
SESSION_ID=$(curl -sf -X POST "$BASE/api/v1/sessions" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "{\"workspace_id\": \"$WORKSPACE_ID\", \"profile_id\": \"interactive\", \"prompt\": \"List the files in this repo.\"}" \
  | jq -r .id)

# 3. Send it a follow-up message.
curl -sf -X POST "$BASE/api/v1/sessions/$SESSION_ID/messages" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"text": "Now create a README."}'

# 4. List its transcript events so far.
curl -sf "$BASE/api/v1/sessions/$SESSION_ID/events?limit=100" \
  -H "Authorization: Bearer $TOKEN" | jq .

# 5. If a tool call needs a permission decision, it shows up here; approve
#    the oldest pending one.
APPROVAL_ID=$(curl -sf "$BASE/api/v1/approvals?state=pending" \
  -H "Authorization: Bearer $TOKEN" | jq -r ".[] | select(.session_id == \"$SESSION_ID\") | .id" | head -1)
if [ -n "$APPROVAL_ID" ]; then
  curl -sf -X POST "$BASE/api/v1/approvals/$APPROVAL_ID" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{"decision": "allow"}'
fi
```

To watch events live instead of polling step 4, stream `GET /api/v1/events` with
`curl -N -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/events"` in a second terminal before
step 3.

## Compatibility promise

`docs/openapi.yaml`'s `info.version` is `1.0.0`. Within the `1.x` line:

- The API only changes **additively**: new operations, new optional request fields, new
  optional response fields, and new enum values may appear in any `1.x` release.
- Existing response fields are **never removed or retyped** (a `string` never becomes an
  `integer`, and so on).
- A new **required** request-body field is never added to an operation that already exists —
  a client written against `1.0.0` keeps working, unmodified, against every later `1.x` release.
- A **deprecation** — an operation or field Styr intends to remove — is announced in
  [`CHANGELOG.md`](../CHANGELOG.md) with at least one minor release's notice before it
  disappears, which (being additive-only) only actually happens at the next major version,
  `2.0.0`.

This is enforced, not just promised: `hack/openapi-compat` (see
[`hack/openapi-compat/main.go`](../hack/openapi-compat/main.go)) diffs the working tree's
`docs/openapi.yaml` against the previous release's copy and fails CI on any of the breaking
changes above. Run it yourself with `make api-compat`.

## Regenerating clients from the OpenAPI document

Styr's own web frontend generates its TypeScript types straight from `docs/openapi.yaml`:

```bash
cd web && npm run gen:api   # openapi-typescript ../docs/openapi.yaml -o src/api/schema.d.ts
```

Any OpenAPI 3.1-compatible generator works the same way against the same file — for example
[`openapi-generator-cli`](https://openapi-generator.tech/) for a typed client in another
language, or [Redocly](https://redocly.com/) (`npx @redocly/cli lint docs/openapi.yaml`) to lint
the document itself, which is what CI runs before every release.
