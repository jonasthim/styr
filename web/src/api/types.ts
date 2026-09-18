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

/** Since v0.4 a template can carry a loop: `loop_until` names the report
 * field whose truthiness ends the loop ('' means no loop) and `loop_max`
 * caps the iterations. */
export type Template = Schemas['Template']

export type TemplateRenderResult = JSONResponse<'renderTemplate', 200>

export type TriggerKind = Schemas['Trigger']['kind']

/** Since T54 a trigger runs either a template or a pipeline: `pipeline_id`
 * is the alternative to `template_id` (exactly one is set; the unused one is
 * '' / null). Written as an intersection because docs/openapi.yaml only
 * learns about the column in T57. */
export type Trigger = Schemas['Trigger'] & {
  pipeline_id: string | null
}

/** Only the POST /triggers response carries the plaintext secret - it is
 * never shown again after this. `trigger` is re-stated so it carries T54's
 * `pipeline_id`, which docs/openapi.yaml only learns about in T57. */
export type TriggerCreateResult = Omit<JSONResponse<'createTrigger', 201>, 'trigger'> & { trigger: Trigger }

export type RotateSecretResult = JSONResponse<'rotateTriggerSecret', 200>

export type DeliveryStatus = Schemas['Delivery']['status']

export type Delivery = Omit<Schemas['Delivery'], 'payload'> & { payload: unknown }

export type ReplayResult = JSONResponse<'replayDelivery', 202>

/** POST /triggers/{id}/test answers with the same {delivery_id, status,
 * run_id?} shape as the inbound POST /hooks/{slug} it stands in for. */
export type TriggerTestResult = Schemas['DeliveryResult']

export type RunOutcome = Schemas['Run']['outcome']

/** A run row. `loop_id` is the loop this run is an iteration of, '' (not
 * null - the handler serves a plain Go string) for a one-shot run, and
 * `iteration` is its 1-based position in that loop, 0 outside one. */
export type Run = Omit<Schemas['Run'], 'report'> & {
  /** The structured report the CLI produced, parsed out of the session's
   * result; null until the run finishes with one. Its shape is whatever the
   * template's report_schema asked for - see StructuredReport for the one
   * seeded for Grafana. */
  report: unknown
  /** Set on every run a pipeline step started (T54); null otherwise. The
   * schema learns about it in T57. */
  step_run_id: string | null
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
 * row plus the session it started, the delivery and template it came from,
 * and the loop it is an iteration of - each null when that record is
 * unavailable (the template was since deleted, the run belongs to no loop). */
export type RunView = Omit<Schemas['RunView'], 'run' | 'delivery'> & {
  run: Run
  delivery: Delivery | null
  /** The pipeline step this run is (T54) and the pipeline it belongs to;
   * both null for a run outside a pipeline. */
  step: StepRun | null
  pipeline: Pipeline | null
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

// --- schedules, loops and stats (v0.4) -------------------------------------
// Aliases of the generated schema like everything above: T50's handlers
// landed with docs/openapi.yaml describing them, so T51's `npm run gen:api`
// replaced T49's hand-written block with re-exports. Two field-level
// overrides survive, both for the same reason as `prefs` and a tool call's
// `input` at the top of this file: a schema property OpenAPI can only
// describe as a bare "object" generates as `Record<string, never>`.

/** A cron entry that starts a template on a cadence. `cron` is a standard
 * 5-field expression (or an `@hourly`/`@daily` descriptor) in the server's
 * timezone; `vars` is merged into the template's render context.
 *
 * `last_outcome` is the scheduler's own word for how the last firing went
 * ('' before the first one) - see components/schedules/lastOutcome.ts. */
export type Schedule = Omit<Schemas['Schedule'], 'vars'> & {
  vars: Record<string, unknown>
  /** The pipeline this schedule starts, or null when it runs a template (T54). */
  pipeline_id: string | null
}

/** POST/PATCH /schedules(/{id}). PATCH replaces the mutable fields rather
 * than merging, so the UI always sends the whole row back. */
export type ScheduleInput = Omit<Schemas['ScheduleInput'], 'vars'> & {
  vars?: Record<string, unknown>
  /** Exactly one of template_id / pipeline_id is set (T54). */
  pipeline_id?: string | null
}

/** `skipped_overlap` is the scheduler declining to start a second run while
 * the previous one is still going; `failed` carries the reason in `reason`. */
export type ScheduleFiringStatus = Schemas['ScheduleFiring']['status']

export type ScheduleFiring = Schemas['ScheduleFiring']

/** POST /schedules/preview: the next five times a cron would fire, plus a
 * human sentence for the expression. An expression that does not parse is a
 * 422 with code `invalid_cron`, not a field on this body. */
export type CronPreview = Schemas['SchedulePreview']

/** POST /schedules/{id}/run and POST /templates/{id}/run both answer 202
 * with the run they started. */
export type RunStartedResult = Schemas['RunID']

export type LoopState = Schemas['Loop']['state']

/** A template with `loop_until` set repeats on one session until the
 * report's field is truthy (`done`), the cap is reached (`exhausted`), a run
 * fails (`failed`) or someone stops it (`stopped`). */
export type Loop = Schemas['Loop']

/** GET /loops/{id}: the loop plus its runs in iteration order. */
export type LoopView = Omit<Schemas['LoopView'], 'runs'> & { runs: Run[] }

/** What a session was doing across one slice of the Gantt window. */
export type GanttKind = Schemas['Segment']['kind']

export type GanttSegment = Schemas['Segment']

export type GanttLane = Schemas['GanttLane']

/** GET /stats/gantt?from=&to=. Lanes are the sessions active in the window,
 * capped server-side at 200. */
export type GanttStats = JSONResponse<'statsGantt', 200>

export type CostDay = Schemas['DayCost']

/** One row of a cost breakdown: a user, an origin or a template. */
export type CostBreakdown = Schemas['NamedCost']

/** GET /stats/costs?days=. `total_usd` is the window's total; `window` is
 * that window's size in days - not a pair of timestamps - so the page
 * derives the dates it shows from `days` itself. */
export type CostStats = Schemas['Costs']

// --- pipelines (v0.5, T54) -------------------------------------------------
// Hand-written like the T49 block above and for the same reason: the handlers
// these describe land in T57 and docs/openapi.yaml only gains them then. They
// are exactly the contract in
// docs/superpowers/plans/2026-09-19-styr-v0.5-pipelines.md ("API"), which the
// msw mock implements verbatim, so T58's `npm run gen:api` can replace this
// block with re-exports without moving a field.

/** A YAML DAG of steps, each of which runs a template. `yaml` is the whole
 * definition; the parsed shape only ever reaches the UI as a `PipelineGraph`
 * from POST /pipelines/validate. */
export interface Pipeline {
  id: string
  owner_id: string | null
  name: string
  workspace_id: string
  yaml: string
  created_at: string
  updated_at: string
}

/** POST/PATCH /pipelines(/{id}). PATCH replaces the mutable fields rather
 * than merging, so the UI always sends the whole row back. */
export interface PipelineInput {
  name: string
  workspace_id: string
  yaml: string
}

/** One node of the validated DAG: the step's id, the template it runs, its
 * worktree mode, and the `foreach` expression when it fans out ('' when it
 * does not). */
export interface PipelineNode {
  id: string
  template: string
  worktree: 'own' | 'shared'
  foreach: string
}

export interface PipelineEdge {
  from: string
  to: string
}

export interface PipelineGraph {
  nodes: PipelineNode[]
  edges: PipelineEdge[]
}

/** One validation failure, pointing at the line of the definition it came
 * from (1-based, so it can be shown in the editor's gutter). */
export interface PipelineError {
  line: number
  message: string
}

/** POST /pipelines/validate. `graph` is whatever could be parsed even when
 * `ok` is false, so the preview keeps showing the shape while it is edited. */
export interface PipelineValidation {
  ok: boolean
  errors: PipelineError[]
  graph: PipelineGraph
}

/** POST /pipelines/{id}/start. */
export interface PipelineStartResult {
  pipeline_run_id: string
}

export type PipelineRunState = 'running' | 'success' | 'failed' | 'cancelled' | 'timeout'

export interface PipelineRun {
  id: string
  pipeline_id: string
  origin: string
  origin_ref: string
  input: Record<string, unknown>
  state: PipelineRunState
  started_at: string
  finished_at: string | null
  cost_usd: number
}

export type StepRunState = 'pending' | 'running' | 'success' | 'failed' | 'skipped' | 'cancelled'

/** One execution of one node. A fan-out node has one step run per item
 * (`index_in_fanout`, `item`); a retried node has one per attempt. */
export interface StepRun {
  id: string
  pipeline_run_id: string
  step_id: string
  index_in_fanout: number
  item: string
  run_id: string | null
  attempt: number
  state: StepRunState
  /** The run's structured report once the step succeeds; null before that. */
  report: unknown
  started_at: string | null
  finished_at: string | null
  worktree: string
}

/** GET /pipeline-runs/{id}. */
export interface PipelineRunView {
  run: PipelineRun
  pipeline: Pipeline | null
  steps: StepRun[]
  graph: PipelineGraph
}

/** The SSE `pipeline.state` frame's payload: a pipeline run's own state, or
 * one step run's when `step_run_id` is set. */
export interface PipelineStatePayload {
  pipeline_run_id: string
  step_run_id?: string
  state: PipelineRunState | StepRunState
}
