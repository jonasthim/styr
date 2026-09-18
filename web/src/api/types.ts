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

/** The session row. Since T42 it carries the worktree fields (`worktree`,
 * `branch`, `base_ref`) and the diff counters `session.stats` patches
 * (`diff_add`, `diff_del`), all of them required by the schema. */
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

// --- review: diff, comments, checkpoints (T43/T44) -------------------------
// Aliases of the generated schema like everything above: docs/openapi.yaml
// describes the v0.3 review surface since T42, so `npm run gen:api` now has
// the real contract and T44 collapsed this block back into re-exports.

/** git's status letter for a changed file: added, modified, deleted, renamed. */
export type FileStatus = Schemas['ReviewFileChange']['status']

/** One file in GET /sessions/{id}/diff. `old_path` is only set for a rename. */
export type FileChange = Schemas['ReviewFileChange']

/** GET /sessions/{id}/diff: the whole worktree against the ref it started
 * from. Named DiffSummary here (and ReviewDiff in the spec) because that is
 * what internal/gitops calls it and what the UI reads it as. */
export type DiffSummary = Schemas['ReviewDiff']

export type DiffLineType = Schemas['DiffLine']['type']

/** `old_no`/`new_no` is 0 - not null - on the side the line does not exist
 * on: the handler serves plain Go ints (internal/api/review_handlers.go's
 * diffLineDTO), and git line numbers are 1-based, so 0 is unambiguous. */
export type DiffLine = Schemas['DiffLine']

export type DiffHunk = Schemas['DiffHunk']

/** GET /sessions/{id}/diff/file?path=. `truncated` says the diff was cut off
 * at gitops' 2 MB cap and the reader is looking at part of one. */
export type FileDiff = Schemas['FileDiff']

/** One inline review comment (table `review_comments`). `sent_at` is null
 * until POST review hands the comment to the CLI. */
export type ReviewComment = Schemas['ReviewComment']

export type Checkpoint = Schemas['Checkpoint']

/** POST /sessions/{id}/commit. */
export type CommitResult = JSONResponse<'commitSession', 200>

/** POST /sessions/{id}/pr. 409 carries code `gh_unavailable` or `no_remote`
 * instead. */
export type PullRequestResult = JSONResponse<'createSessionPR', 200>
