// API types derived from the generated OpenAPI schema (docs/openapi.yaml ->
// `npm run gen:api` -> schema.d.ts), so these can't drift from the spec the
// backend handlers are checked against (internal/api/*_handlers.go). Only
// re-exports/aliases live here - never hand-written field lists - plus a
// handful of field-level overrides for schema properties OpenAPI can only
// describe as a bare "object": those come back from openapi-typescript as
// `Record<string, never>` (an empty object), which is unusable for the
// actual free-form JSON these fields carry (session/user preferences, a
// tool call's input, an event's decoded payload, a delivery's webhook body,
// a run's structured report).
import type { components, operations } from './schema'

type Schemas = components['schemas']

/** The JSON body of one operation's response, for the handful of endpoints
 * whose response shape docs/openapi.yaml declares inline rather than as a
 * named component (a create envelope, a dry-run result). */
type JSONResponse<O extends keyof operations, S extends keyof operations[O]['responses']> =
  operations[O]['responses'][S] extends { content: { 'application/json': infer T } } ? T : never

export type Role = Schemas['User']['role']

export type ClaudeTokenInfo = Schemas['ClaudeToken']

export type Me = Omit<Schemas['Me'], 'prefs'> & { prefs: Record<string, unknown> }

export type User = Omit<Schemas['User'], 'prefs'> & { prefs: Record<string, unknown> }

export type WorkspaceSource = Schemas['Workspace']['source']
export type WorkspaceState = Schemas['Workspace']['state']

export type Workspace = Schemas['Workspace']

export type ProfileMode = Schemas['Profile']['mode']

/** Reasoning effort levels; '' means the CLI's own default. */
export type Effort = Schemas['Profile']['effort']

/** One entry of GET /status's model list: the alias the CLI takes, and its label. */
export type ModelOption = Schemas['StatusInfo']['models'][number]

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

// --- personal API tokens (T36) ---------------------------------------------

export type ApiToken = Schemas['APIToken']

/** POST /me/api-tokens's response: id/name/prefix plus `token`, the raw
 * secret shown once and never returned by any other response. */
export type ApiTokenCreated = Schemas['APITokenCreated']

// --- triggers, templates, runs and notifications (T33/T34/T35) -------------

export type Template = Schemas['Template']

export type TemplateRenderResult = JSONResponse<'renderTemplate', 200>

export type TriggerKind = Schemas['Trigger']['kind']

export type Trigger = Schemas['Trigger']

/** Only the POST /triggers response carries the plaintext secret - it is
 * never shown again after this. */
export type TriggerCreateResult = JSONResponse<'createTrigger', 201>

export type RotateSecretResult = JSONResponse<'rotateTriggerSecret', 200>

export type DeliveryStatus = Schemas['Delivery']['status']

export type Delivery = Omit<Schemas['Delivery'], 'payload'> & { payload: unknown }

export type ReplayResult = JSONResponse<'replayDelivery', 202>

/** POST /triggers/{id}/test answers with the same {delivery_id, status,
 * run_id?} shape as the inbound POST /hooks/{slug} it stands in for. */
export type TriggerTestResult = Schemas['DeliveryResult']

export type RunOutcome = Schemas['Run']['outcome']

export type Run = Omit<Schemas['Run'], 'report'> & {
  /** The structured report the CLI produced, parsed out of the session's
   * result; null until the run finishes with one. Its shape is whatever the
   * template's report_schema asked for - see StructuredReport for the one
   * seeded for Grafana. */
  report: unknown
}

/** The report_schema seeded for the Grafana template (plan, "Template
 * rendering"). Other templates may define a different shape, so
 * ReportView.tsx reads defensively and renders only the fields it finds. */
export interface StructuredReport {
  severity?: 'info' | 'warning' | 'critical'
  diagnosis?: string
  evidence?: string[]
  proposed_action?: string
  confidence?: number
  resolved_itself?: boolean
}

/** GET /runs and GET /runs/{id} both answer with runs in this shape: the run
 * row plus the session it started and the delivery and template it came
 * from, each null when that record is unavailable (e.g. the template was
 * since deleted). */
export type RunView = Omit<Schemas['RunView'], 'run' | 'delivery'> & {
  run: Run
  delivery: Delivery | null
}

export type NotificationChannelKind = Schemas['NotificationChannel']['kind']

/** The events the UI offers a channel. Narrower than the schema on purpose:
 * the backend stores (and a hand-written config could add) any string, but
 * these three are the ones notify.Service publishes and the add-channel
 * dialog lists. */
export type NotificationEvent = 'run.finished' | 'run.needs_human' | 'run.failed'

export type NotificationChannel = Schemas['NotificationChannel']
