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
export type WorkspaceAccessMode = Schemas['Workspace']['access']

export type Workspace = Schemas['Workspace']

/** GET /workspaces/{id}/access (admin-only, T64). */
export type WorkspaceAccess = Schemas['WorkspaceAccess']

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

/** Since v0.5 a trigger runs either a template or a pipeline: `pipeline_id`
 * is the alternative to `template_id`. Exactly one is set, and the unused
 * one is '' (template_id, a plain Go string) or null (pipeline_id). */
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

/** A run row. `loop_id` is the loop this run is an iteration of, '' (not
 * null - the handler serves a plain Go string) for a one-shot run, and
 * `iteration` is its 1-based position in that loop, 0 outside one. */
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
 * row plus the session it started, the delivery and template it came from,
 * the loop it is an iteration of, and the pipeline step it is one attempt
 * of together with that step's pipeline - each null when that record is
 * unavailable (the template was since deleted, the run belongs to no loop
 * or to no pipeline). */
export type RunView = Omit<Schemas['RunView'], 'run' | 'delivery' | 'step'> & {
  run: Run
  delivery: Delivery | null
  /** The pipeline step this run is one attempt of; null for a run outside a
   * pipeline, and paired with `pipeline` (also null then). */
  step: StepRun | null
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
export type Schedule = Omit<Schemas['Schedule'], 'vars'> & { vars: Record<string, unknown> }

/** POST/PATCH /schedules(/{id}). PATCH replaces the mutable fields rather
 * than merging, so the UI always sends the whole row back. */
export type ScheduleInput = Omit<Schemas['ScheduleInput'], 'vars'> & { vars?: Record<string, unknown> }

/** `skipped_overlap` is the scheduler declining to start a second run while
 * the previous one is still going; `failed` carries the reason in `reason`. */
export type ScheduleFiringStatus = Schemas['ScheduleFiring']['status']

export type ScheduleFiring = Schemas['ScheduleFiring']

/** POST /schedules/preview: the next five times a cron would fire, plus a
 * human sentence for the expression. An expression that does not parse is a
 * 422 with code `invalid_cron`, not a field on this body. */
export type CronPreview = Schemas['SchedulePreview']

/** POST /templates/{id}/run answers 202 with the run it started. */
export type RunStartedResult = Schemas['RunID']

/** POST /schedules/{id}/run answers 202 with the same `run_id` plus, for a
 * schedule that starts a pipeline, the bare pipeline run id behind the
 * "pr:" reference `run_id` carries - so the UI can open the pipeline run
 * page instead of a run page that has nothing to show. */
export type ScheduleRunResult = Schemas['ScheduleRunStarted']

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

// --- pipelines (v0.5) ------------------------------------------------------
// Aliases of the generated schema like everything above: T57's handlers
// landed with docs/openapi.yaml describing them, so T58's `npm run gen:api`
// replaced T54's hand-written block with re-exports. Three shapes moved
// while doing it, because the handler - not the plan's prose - is the
// contract: a graph node's `foreach` is a boolean ("this step fans out"),
// not the expression; `PipelineRunView.pipeline` is always present (the
// handler fails the whole request when it cannot read it); and each entry
// of its `steps` is a step run *plus* the run summary of its current
// attempt. The same `Record<string, never>` overrides as everywhere above
// apply to the two free-form JSON fields (a run's input, a step's report).

/** A YAML DAG of steps, each of which runs a template. `yaml` is the whole
 * definition; the parsed shape only ever reaches the UI as a `PipelineGraph`
 * from POST /pipelines/validate. */
export type Pipeline = Schemas['Pipeline']

/** POST/PATCH /pipelines(/{id}). PATCH replaces the mutable fields rather
 * than merging, so the UI always sends the whole row back. */
export type PipelineInput = Schemas['PipelineInput']

/** One node of the validated DAG: the step's id, the template it runs, its
 * worktree mode ('', 'own' or 'shared' - the handler serves the raw yaml
 * string, so it is not narrowed to a union) and whether it fans out. */
export type PipelineNode = Schemas['GraphNode']

export type PipelineEdge = Schemas['GraphEdge']

export type PipelineGraph = Schemas['Graph']

/** One validation failure, pointing at the line of the definition it came
 * from (1-based, so it can be shown in the editor's gutter; 0 when the
 * validator could not place it). */
export type PipelineError = Schemas['Problem']

/** POST /pipelines/validate. `graph` is whatever could be parsed even when
 * `ok` is false, so the preview keeps showing the shape while it is edited. */
export type PipelineValidation = Schemas['ValidationResult']

/** POST /pipelines/{id}/start. */
export type PipelineStartResult = Schemas['PipelineRunID']

export type PipelineRunState = Schemas['PipelineRun']['state']

export type PipelineRun = Omit<Schemas['PipelineRun'], 'input'> & { input: Record<string, unknown> }

export type StepRunState = Schemas['StepRun']['state']

/** One execution of one node. A fan-out node has one step run per item
 * (`index_in_fanout`, `item`); a retried node has one per attempt.
 * `report` is the run's structured report once the step succeeds, null
 * before that. */
export type StepRun = Omit<Schemas['StepRun'], 'report'> & { report: unknown }

/** One entry of GET /pipeline-runs/{id}'s `steps`: the step run plus the
 * Run summary of its current attempt (null until the executor started
 * one). Assignable to StepRun everywhere the graph and the log take one. */
export type PipelineRunStep = StepRun & { run: Run | null }

/** GET /pipeline-runs/{id}. `pipeline` is never null: the handler answers
 * 404 rather than a view without it. */
export type PipelineRunView = Omit<Schemas['PipelineRunView'], 'run' | 'steps'> & {
  run: PipelineRun
  steps: PipelineRunStep[]
}

/** The SSE `pipeline.state` frame's payload: a pipeline run's own state, or
 * one step run's when `step_run_id` is set. Hand-written because
 * docs/openapi.yaml describes REST bodies, not the bus messages
 * GET /events streams (the same reason SessionEvent's payload is `unknown`). */
export interface PipelineStatePayload {
  pipeline_run_id: string
  step_run_id?: string
  state: PipelineRunState | StepRunState
}
