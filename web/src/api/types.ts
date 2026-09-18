// API types derived from the generated OpenAPI schema (docs/openapi.yaml ->
// `npm run gen:api` -> schema.d.ts), so these can't drift from the spec the
// backend handlers are checked against (internal/api/*_handlers.go). Only
// re-exports/aliases live here - never hand-written field lists - plus a
// handful of field-level overrides for schema properties OpenAPI can only
// describe as a bare "object": those come back from openapi-typescript as
// `Record<string, never>` (an empty object), which is unusable for the
// actual free-form JSON these fields carry (session/user preferences, a
// tool call's input, an event's decoded payload).
import type { components } from './schema'

type Schemas = components['schemas']

export type Role = Schemas['User']['role']

export type ClaudeTokenInfo = Schemas['ClaudeToken']

export type Me = Omit<Schemas['Me'], 'prefs'> & { prefs: Record<string, unknown> }

export type User = Omit<Schemas['User'], 'prefs'> & { prefs: Record<string, unknown> }

// Workspace overrides the generated schema outright rather than narrowing it:
// the backend card implementing the per-user workspaces contract (T29) hasn't
// regenerated schema.d.ts yet, so Schemas['Workspace'] still describes the
// old admin-registered-path shape. Field names match the contract exactly so
// this becomes a no-op once codegen catches up.
export type WorkspaceSource = 'git' | 'path' | 'empty'
export type WorkspaceState = 'cloning' | 'ready' | 'failed'

export type Workspace = Omit<Schemas['Workspace'], 'path' | 'error' | 'repo_url' | 'branch'> & {
  owner_id: string | null
  path: string
  source: WorkspaceSource
  repo_url: string | null
  branch: string | null
  managed: boolean
  state: WorkspaceState
  error: string | null
  worktrees: boolean
  created_at: string
  updated_at: string
}

export type ProfileMode = Schemas['Profile']['mode']

export type Profile = Schemas['Profile']

export type SessionState = Schemas['Session']['state']
export type Origin = Schemas['Session']['origin']

export type Session = Schemas['Session']

export type SessionEvent = Omit<Schemas['Event'], 'payload'> & { payload: unknown }

export type RiskTier = Schemas['Approval']['risk']
export type ApprovalState = Schemas['Approval']['state']

export type Approval = Omit<Schemas['Approval'], 'input' | 'updated_input'> & {
  input: unknown
  updated_input?: unknown
}

export type StatusInfo = Schemas['StatusInfo']

export type Provider = Schemas['Provider']

export type ApiErrorBody = Schemas['ErrorBody']

// ApiToken/ApiTokenCreated (T36, personal API tokens: GET/POST
// /me/api-tokens, DELETE /me/api-tokens/{id}) are hand-written rather than
// derived from Schemas, like Workspace above: schema.d.ts is regenerated
// from docs/openapi.yaml by `npm run gen:api`, a step this card does not
// run (schema.d.ts is a generated file owned by the codegen step, not this
// card's allowed files), so Schemas['APIToken'] does not exist yet. Field
// names match docs/openapi.yaml's APIToken/APITokenCreated schemas exactly,
// so this becomes redundant rather than wrong once codegen catches up.
export interface ApiToken {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at: string | null
  expires_at: string | null
}

/** POST /me/api-tokens's response: the same shape as ApiToken (destructure
 * to build one) plus `token`, the raw secret shown once and never returned
 * by any other response. */
export interface ApiTokenCreated {
  id: string
  name: string
  prefix: string
  token: string
  created_at: string
  last_used_at: string | null
  expires_at: string | null
}

// --- Triggers, templates, runs and notifications (T33) ---------------------
// The backend for this contract (T34/T35) hasn't landed yet, so these are
// hand-written rather than derived from schema.d.ts - a later card
// regenerates from openapi.yaml and these become no-ops, so field names
// match the plan's data model exactly (docs/superpowers/plans/2026-09-18-
// styr-v0.2-triggers.md, "Data model" and "API contract").

export interface Template {
  id: string
  owner_user_id: string | null
  name: string
  workspace_id: string
  profile_id: string
  title_template: string
  prompt_template: string
  system_prompt: string
  report_schema: string
  created_at: string
  updated_at: string
}

export interface TemplateRenderResult {
  title: string
  prompt: string
  errors: string[]
}

export type TriggerKind = 'generic' | 'grafana' | 'github'

export interface Trigger {
  id: string
  owner_user_id: string | null
  name: string
  slug: string
  kind: TriggerKind
  secret_hint: string
  template_id: string
  enabled: boolean
  dedupe_key_template: string
  cooldown_s: number
  storm_cap_per_hour: number
  run_on_resolved: boolean
  created_at: string
  updated_at: string
  last_delivery_at: string | null
}

/** Only the POST /triggers response carries the plaintext secret - it is
 * never shown again after this. */
export interface TriggerCreateResult {
  trigger: Trigger
  secret: string
}

export interface RotateSecretResult {
  secret: string
}

export type DeliveryStatus = 'accepted' | 'deduped' | 'cooldown' | 'storm' | 'rejected' | 'failed' | 'skipped'

export interface Delivery {
  id: string
  trigger_id: string
  received_at: string
  status: DeliveryStatus
  reason: string
  dedupe_key: string
  payload: unknown
  run_id: string | null
}

export interface ReplayResult {
  run_id?: string
}

/** Response shape for POST /triggers/{id}/test - a contract addition (not
 * spelled out in the plan beyond "runs the full pipeline as if delivered");
 * mirrors the inbound POST /hooks/{slug} response shape it stands in for. */
export interface TriggerTestResult {
  delivery_id: string
  status: DeliveryStatus
  run_id?: string
}

export type RunOutcome = 'running' | 'success' | 'failed' | 'timeout' | 'needs_human'
export type RunOrigin = 'webhook' | 'schedule' | 'replay' | 'test'

export interface Run {
  id: string
  session_id: string
  template_id: string | null
  trigger_id: string | null
  delivery_id: string | null
  origin: RunOrigin
  started_at: string
  finished_at: string | null
  outcome: RunOutcome
  report: string
  summary: string
  cost_usd: number
}

/** The report_schema seeded for the Grafana template (plan, "Template
 * rendering"). Other templates may use a different shape; report.tsx parses
 * defensively and only renders fields it can find. */
export interface StructuredReport {
  severity?: 'info' | 'warning' | 'critical'
  diagnosis?: string
  evidence?: string[]
  proposed_action?: string
  confidence?: number
  resolved_itself?: boolean
}

/** GET /runs/{id} augments the run row with the session it started and the
 * delivery that triggered it, per the plan ("includes session summary,
 * report, delivery"); GET /runs (list) returns bare Run rows. */
export interface RunDetail extends Run {
  session: { id: string; title: string; state: SessionState; workspace_id: string } | null
  delivery: Delivery | null
  trigger_name: string | null
  template_name: string | null
}

export type NotificationChannelKind = 'ntfy' | 'webhook'
export type NotificationEvent = 'run.finished' | 'run.needs_human' | 'run.failed'

export interface NotificationChannel {
  id: string
  kind: NotificationChannelKind
  name: string
  url: string
  events: NotificationEvent[]
  enabled: boolean
  /** Never the token itself - only whether one is set (ntfy channels only). */
  token_present: boolean
  created_at: string
}
