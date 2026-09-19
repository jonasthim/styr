// In-memory mock of the v0.5 "Pipelines" API contract
// (docs/superpowers/plans/2026-09-19-styr-v0.5-pipelines.md, "API"):
// pipelines and their validation, starting one, and pipeline runs with their
// step runs. Spread into handlers.ts's exported `handlers` array.
//
// The seed data lives in ./pipelinesState (a leaf module, so triggersHandlers
// can read a run's step back without a cycle); this module owns the routes,
// the Run rows and sessions the seeded step runs point at, and the
// `pipeline.state` frames the live layer patches caches from.
import { http, HttpResponse } from 'msw'
import type {
  ApiErrorBody,
  Pipeline,
  PipelineGraph,
  PipelineRun,
  PipelineRunView,
  PipelineStartResult,
  PipelineStatePayload,
  Run,
  Session,
  StepRun,
} from '../api/types'
import { DEV_USER_ID, sessions } from './sessionsState'
import { runs } from './triggersHandlers'
import { emitFakeEvent } from './fakeEventSource'
import { validatePipeline } from './pipelineYaml'
import {
  PIPELINE_RUN_DONE_ID,
  PIPELINE_RUN_RUNNING_ID,
  STARTER_YAML,
  STEP_RUN_IDS,
  pipelineRuns,
  pipelines,
  stepRuns,
} from './pipelinesState'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

function errorBody(code: string, message: string): ApiErrorBody {
  return { error: { code, message } }
}

let idSeq = 1
function nextId(prefix: string): string {
  idSeq += 1
  return `${prefix}-${idSeq}`
}

// --- the sessions and runs behind the seeded step runs ---------------------

function seedStepSession(id: string, title: string, running: boolean, startedAt: string): Session {
  const session: Session = {
    id: `sess-${id}`,
    owner_id: null,
    title,
    workspace_id: 'w1',
    profile_id: 'interactive',
    harness: 'claude',
    state: running ? 'running' : 'closed',
    origin: 'pipeline',
    origin_ref: id,
    worktree: '',
    branch: '',
    base_ref: '',
    worktree_shared: false,
    diff_add: 0,
    diff_del: 0,
    created_at: startedAt,
    last_active_at: iso(0),
    num_turns: running ? 2 : 4,
    cost_usd: running ? 0.04 : 0.09,
    tokens_in: 0,
    tokens_out: 0,
    now_line: running ? 'Working…' : '',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: [],
  }
  sessions.push(session)
  return session
}

function seedStepRun(stepRun: StepRun, title: string): void {
  if (!stepRun.run_id) return
  const running = stepRun.state === 'running'
  const startedAt = stepRun.started_at ?? iso(0)
  const session = seedStepSession(stepRun.run_id, title, running, startedAt)
  const run: Run = {
    id: stepRun.run_id,
    session_id: session.id,
    template_id: null,
    trigger_id: null,
    delivery_id: null,
    origin: 'pipeline',
    started_at: startedAt,
    finished_at: stepRun.finished_at,
    outcome: stepRun.state === 'failed' ? 'failed' : stepRun.state === 'running' ? 'running' : 'success',
    report: stepRun.report,
    summary: title,
    cost_usd: session.cost_usd,
    loop_id: '',
    iteration: 0,
    step_run_id: stepRun.id,
  }
  runs.push(run)
}

const STEP_RUN_TITLES: Record<string, string> = {
  [STEP_RUN_IDS.triage]: 'Triage the CI failure',
  [STEP_RUN_IDS.fixAttempt1]: 'Apply the fix (attempt 1)',
  [STEP_RUN_IDS.fixAttempt2]: 'Apply the fix (attempt 2)',
  [STEP_RUN_IDS.reviewA]: 'Review internal/api/runs_handlers.go',
  [STEP_RUN_IDS.reviewB]: 'Review internal/runs/engine.go',
  [STEP_RUN_IDS.draft]: 'Draft the release notes',
  [STEP_RUN_IDS.publish]: 'Open the release PR',
}

for (const step of stepRuns) {
  if (step.run_id) seedStepRun(step, STEP_RUN_TITLES[step.run_id] ?? step.step_id)
}

// --- helpers ---------------------------------------------------------------

function graphFor(pipelineId: string): PipelineGraph {
  const pipeline = pipelines.find((p) => p.id === pipelineId)
  if (!pipeline) return { nodes: [], edges: [] }
  return validatePipeline(pipeline.yaml).graph
}

/** GET /pipeline-runs/{id}'s body. `pipeline` is always present (the real
 * handler answers 404 rather than a view without it) and every step carries
 * the Run summary of its attempt, null until one has started - the shape of
 * internal/api's pipelineRunViewDTO. */
function viewOf(run: PipelineRun, pipeline: Pipeline): PipelineRunView {
  return {
    run,
    pipeline,
    steps: stepRuns
      .filter((s) => s.pipeline_run_id === run.id)
      .map((step) => ({ ...step, run: runs.find((r) => r.id === step.run_id) ?? null })),
    graph: graphFor(run.pipeline_id),
  }
}

/** The SSE frame the executor publishes as a pipeline run or one of its steps
 * moves; useLiveEvents.ts patches the ['pipeline-run', id] cache from it. */
function emitPipelineState(payload: PipelineStatePayload): void {
  emitFakeEvent('pipeline.state', { kind: 'pipeline.state', owner_id: null, payload })
}

function startStep(pipelineRunId: string, stepId: string, attempt: number, item = '', index = 0): StepRun {
  const id = nextId('step')
  const runId = nextId('run-step')
  const step: StepRun = {
    id,
    pipeline_run_id: pipelineRunId,
    step_id: stepId,
    index_in_fanout: index,
    item,
    run_id: runId,
    attempt,
    state: 'running',
    report: null,
    started_at: iso(0),
    finished_at: null,
    worktree: `/srv/styr/worktrees/${stepId}-${attempt}`,
  }
  stepRuns.push(step)
  seedStepRun(step, item ? `${stepId}: ${item}` : stepId)
  return step
}

/** Creates a pipeline run and its first level of step runs - what POST
 * /pipelines/{id}/start does, and what a trigger delivery or a schedule
 * firing does on the real backend (internal/pipelines' Executor.Start).
 * Exported so ./schedulesHandlers can fire a pipeline schedule without
 * duplicating the fan-out rules. */
export function startPipelineRun(
  pipeline: Pipeline,
  origin: string,
  originRef: string,
  input: Record<string, unknown>,
): PipelineRun {
  const graph = validatePipeline(pipeline.yaml).graph
  const run: PipelineRun = {
    id: nextId('prun'),
    pipeline_id: pipeline.id,
    origin,
    origin_ref: originRef,
    input,
    state: 'running',
    started_at: iso(0),
    finished_at: null,
    cost_usd: 0,
  }
  pipelineRuns.unshift(run)

  // Every node starts pending; the ones with no dependency start running
  // straight away, which is what `advance` does on the real executor's
  // first pass.
  const hasDependency = new Set(graph.edges.map((e) => e.to))
  for (const node of graph.nodes) {
    if (hasDependency.has(node.id)) {
      stepRuns.push({
        id: nextId('step'),
        pipeline_run_id: run.id,
        step_id: node.id,
        index_in_fanout: 0,
        item: '',
        run_id: null,
        attempt: 1,
        state: 'pending',
        report: null,
        started_at: null,
        finished_at: null,
        worktree: '',
      })
    } else {
      startStep(run.id, node.id, 1)
    }
  }
  return run
}

// --- routes ----------------------------------------------------------------

export const pipelinesHandlers = [
  http.get('/api/v1/pipelines', () => HttpResponse.json(pipelines)),

  http.post('/api/v1/pipelines', async ({ request }) => {
    const body = (await request.json()) as Partial<Pipeline>
    const yaml = body.yaml ?? STARTER_YAML
    const result = validatePipeline(yaml)
    if (!result.ok) {
      return HttpResponse.json(
        { ...errorBody('invalid_pipeline', 'This definition has errors.'), errors: result.errors },
        { status: 422 },
      )
    }
    const now = iso(0)
    const pipeline: Pipeline = {
      id: nextId('pipe'),
      owner_id: DEV_USER_ID,
      name: body.name ?? 'new-pipeline',
      workspace_id: body.workspace_id ?? 'w1',
      yaml,
      created_at: now,
      updated_at: now,
    }
    pipelines.unshift(pipeline)
    return HttpResponse.json(pipeline, { status: 201 })
  }),

  // Ahead of /pipelines/:id only for readability - msw ranks a static
  // segment above a param one whatever the order, and nothing POSTs to
  // /pipelines/{id} anyway.
  http.post('/api/v1/pipelines/validate', async ({ request }) => {
    const body = (await request.json()) as { yaml?: string; workspace_id?: string }
    const yaml = body.yaml ?? ''
    const result = validatePipeline(yaml)
    // Mock-only escape hatch: a definition carrying the literal
    // "forward-need" marker always comes back invalid, so a test can ask for
    // the error path without depending on the parser's own rules.
    if (yaml.includes('forward-need')) {
      const line = yaml.split('\n').findIndex((l) => l.includes('forward-need')) + 1
      return HttpResponse.json({
        ok: false,
        errors: [{ line, message: 'This step needs one that is not defined above it.' }, ...result.errors],
        graph: result.graph,
      })
    }
    return HttpResponse.json(result)
  }),

  http.get('/api/v1/pipelines/:id', ({ params }) => {
    const pipeline = pipelines.find((p) => p.id === params.id)
    if (!pipeline) return HttpResponse.json(errorBody('not_found', 'No such pipeline.'), { status: 404 })
    return HttpResponse.json(pipeline)
  }),

  http.patch('/api/v1/pipelines/:id', async ({ request, params }) => {
    const pipeline = pipelines.find((p) => p.id === params.id)
    if (!pipeline) return HttpResponse.json(errorBody('not_found', 'No such pipeline.'), { status: 404 })
    const body = (await request.json()) as Partial<Pipeline>
    if (body.yaml !== undefined) {
      const result = validatePipeline(body.yaml)
      if (!result.ok) {
        return HttpResponse.json(
          { ...errorBody('invalid_pipeline', 'This definition has errors.'), errors: result.errors },
          { status: 422 },
        )
      }
      pipeline.yaml = body.yaml
    }
    if (body.name !== undefined) pipeline.name = body.name
    if (body.workspace_id !== undefined) pipeline.workspace_id = body.workspace_id
    pipeline.updated_at = iso(0)
    return HttpResponse.json(pipeline)
  }),

  http.delete('/api/v1/pipelines/:id', ({ params }) => {
    const index = pipelines.findIndex((p) => p.id === params.id)
    if (index === -1) return HttpResponse.json(errorBody('not_found', 'No such pipeline.'), { status: 404 })
    pipelines.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/v1/pipelines/:id/start', async ({ request, params }) => {
    const pipeline = pipelines.find((p) => p.id === params.id)
    if (!pipeline) return HttpResponse.json(errorBody('not_found', 'No such pipeline.'), { status: 404 })
    const body = (await request.json().catch(() => ({}))) as { input?: Record<string, unknown> }
    const run = startPipelineRun(pipeline, 'ui', '', body.input ?? {})
    const result: PipelineStartResult = { pipeline_run_id: run.id }
    return HttpResponse.json(result, { status: 202 })
  }),

  http.get('/api/v1/pipeline-runs', ({ request }) => {
    const url = new URL(request.url)
    const pipeline = url.searchParams.get('pipeline')
    const state = url.searchParams.get('state')
    const limit = Number(url.searchParams.get('limit') ?? '100')
    const filtered = pipelineRuns.filter(
      (r) => (!pipeline || r.pipeline_id === pipeline) && (!state || r.state === state),
    )
    return HttpResponse.json(filtered.slice(0, limit))
  }),

  http.get('/api/v1/pipeline-runs/:id', ({ params }) => {
    const run = pipelineRuns.find((r) => r.id === params.id)
    const pipeline = run ? pipelines.find((p) => p.id === run.pipeline_id) : undefined
    if (!run || !pipeline) return HttpResponse.json(errorBody('not_found', 'No such pipeline run.'), { status: 404 })
    return HttpResponse.json(viewOf(run, pipeline))
  }),

  http.post('/api/v1/pipeline-runs/:id/cancel', ({ params }) => {
    const run = pipelineRuns.find((r) => r.id === params.id)
    if (!run) return HttpResponse.json(errorBody('not_found', 'No such pipeline run.'), { status: 404 })
    for (const step of stepRuns.filter((s) => s.pipeline_run_id === run.id)) {
      if (step.state === 'running') {
        step.state = 'cancelled'
        step.finished_at = iso(0)
        emitPipelineState({ pipeline_run_id: run.id, step_run_id: step.id, state: 'cancelled' })
      } else if (step.state === 'pending') {
        step.state = 'skipped'
        emitPipelineState({ pipeline_run_id: run.id, step_run_id: step.id, state: 'skipped' })
      }
    }
    run.state = 'cancelled'
    run.finished_at = iso(0)
    emitPipelineState({ pipeline_run_id: run.id, state: 'cancelled' })
    return new HttpResponse(null, { status: 202 })
  }),

  http.post('/api/v1/pipeline-runs/:id/retry-failed', ({ params }) => {
    const run = pipelineRuns.find((r) => r.id === params.id)
    if (!run) return HttpResponse.json(errorBody('not_found', 'No such pipeline run.'), { status: 404 })
    const failed = stepRuns.filter((s) => s.pipeline_run_id === run.id && s.state === 'failed')
    for (const step of failed) {
      const attempts = stepRuns.filter((s) => s.pipeline_run_id === run.id && s.step_id === step.step_id)
      const attempt = Math.max(...attempts.map((s) => s.attempt)) + 1
      const started = startStep(run.id, step.step_id, attempt, step.item, step.index_in_fanout)
      emitPipelineState({ pipeline_run_id: run.id, step_run_id: started.id, state: 'running' })
    }
    run.state = 'running'
    run.finished_at = null
    emitPipelineState({ pipeline_run_id: run.id, state: 'running' })
    return new HttpResponse(null, { status: 202 })
  }),
]

// Referenced so the seeded ids stay reachable from this module's own tests
// and from the e2e literals that duplicate them.
export { PIPELINE_RUN_DONE_ID, PIPELINE_RUN_RUNNING_ID }
