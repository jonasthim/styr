// In-memory mock of the v0.4 "Schedules and loops" API contract
// (docs/superpowers/plans/2026-09-19-styr-v0.4-schedules-loops.md, "API"):
// schedules and their firings, the cron preview, loops, the manual template
// run, and the two read-only /stats routes behind the fleet Gantt and the
// cost dashboard. Spread into handlers.ts's exported `handlers` array.
//
// Seed data: two schedules (one enabled with recent firings including a
// skipped_overlap, one disabled), a loop template with a running loop two
// iterations in on its own session, Gantt lanes derived from every seeded
// session, and 30 days of costs.
import { http, HttpResponse } from 'msw'
import type {
  ApiErrorBody,
  CostBreakdown,
  CostDay,
  CostStats,
  GanttKind,
  GanttLane,
  GanttSegment,
  GanttStats,
  Loop,
  LoopState,
  LoopView,
  Run,
  RunStartedResult,
  Schedule,
  ScheduleFiring,
  Session,
  StructuredReport,
} from '../api/types'
import { DEV_USER_ID, sessions } from './sessionsState'
import { LOOP_ID, LOOP_SESSION_ID, TEMPLATE_LOOP_ID, loops } from './loopsState'
import { pipelines } from './pipelinesState'
import { runs, startRun, templates } from './triggersHandlers'
import { previewCron, parseCron, nextRuns } from './cron'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

function isoIn(minutesAhead: number): string {
  return new Date(Date.now() + minutesAhead * 60_000).toISOString()
}

function errorBody(code: string, message: string): ApiErrorBody {
  return { error: { code, message } }
}

let idSeq = 1
function nextId(prefix: string): string {
  idSeq += 1
  return `${prefix}-${idSeq}`
}

/** The schedule's next firing, or null when it is disabled or the cron can
 * never match again - the same rule the real scheduler applies when it
 * recomputes `next_run_at` after a tick. */
function computeNextRun(cron: string, enabled: boolean): string | null {
  if (!enabled) return null
  const parsed = parseCron(cron)
  if (!parsed) return null
  return nextRuns(parsed, new Date(), 1)[0]?.toISOString() ?? null
}

// --- the loop template, its session and its two runs ------------------------

const LOOP_REPORT_SCHEMA = {
  type: 'object',
  required: ['done', 'diagnosis'],
  properties: {
    done: { type: 'boolean' },
    diagnosis: { type: 'string' },
    evidence: { type: 'array', items: { type: 'string' } },
    proposed_action: { type: 'string' },
    resolved_itself: { type: 'boolean' },
  },
}

export const TEMPLATE_LOOP_NAME = 'Fix failing CI until green'

templates.push({
  id: TEMPLATE_LOOP_ID,
  owner_id: DEV_USER_ID,
  name: TEMPLATE_LOOP_NAME,
  workspace_id: 'w1',
  profile_id: 'interactive',
  title_template: 'CI is red on {{ .payload.branch }}',
  prompt_template:
    'The CI run on {{ .payload.branch }} is failing. Find the cause, fix it, run the tests, and report whether they pass.',
  system_prompt: '',
  report_schema: JSON.stringify(LOOP_REPORT_SCHEMA, null, 2),
  loop_until: 'done',
  loop_max: 3,
  created_at: iso(60 * 24 * 6),
  updated_at: iso(60 * 24),
})

sessions.push({
  id: LOOP_SESSION_ID,
  owner_id: DEV_USER_ID,
  title: 'CI is red on main',
  workspace_id: 'w1',
  profile_id: 'interactive',
  harness: 'claude',
  state: 'running',
  origin: 'ui',
  origin_ref: LOOP_ID,
  worktree: '',
  branch: '',
  base_ref: '',
  diff_add: 12,
  diff_del: 4,
  created_at: iso(34),
  last_active_at: iso(1),
  num_turns: 7,
  cost_usd: 0.58,
  tokens_in: 21400,
  tokens_out: 3100,
  now_line: 'Re-running the test suite',
  model: 'claude-fable-5-1',
  effort: 'medium',
  slash_commands: [],
})

const ITERATION_1_REPORT: StructuredReport & { done: boolean } = {
  done: false,
  diagnosis:
    'The flake is in TestScheduler_Tick: it asserts on wall-clock ordering. Pinned it to the fake clock, but three assertions are still failing after the change.',
  evidence: ['go test ./internal/schedules/... -run TestScheduler_Tick: 3 failures', 'the failures are all in the ordering assertions'],
  proposed_action: 'Rewrite the ordering assertions against the fake clock and run again.',
}

const ITERATION_2_REPORT: StructuredReport & { done: boolean } = {
  done: false,
  diagnosis:
    'Ordering is fixed, but two tests are still failing: the fixture writes its rows with a second-resolution timestamp, so two firings land in the same bucket.',
  evidence: ['go test ./internal/schedules/...: 2 failures', 'both failures compare firings recorded in the same second'],
  proposed_action: 'Give the fixture millisecond timestamps and run the suite once more.',
}

const LOOP_RUN_1_ID = 'run-loop-ci-1'
const LOOP_RUN_2_ID = 'run-loop-ci-2'

runs.unshift(
  {
    id: LOOP_RUN_1_ID,
    session_id: LOOP_SESSION_ID,
    template_id: TEMPLATE_LOOP_ID,
    trigger_id: null,
    delivery_id: null,
    origin: 'loop',
    started_at: iso(34),
    finished_at: iso(24),
    outcome: 'success',
    report: ITERATION_1_REPORT,
    summary: 'Three assertions still failing after pinning the fake clock.',
    cost_usd: 0.26,
    loop_id: LOOP_ID,
    iteration: 1,
    step_run_id: null,
  },
  {
    id: LOOP_RUN_2_ID,
    session_id: LOOP_SESSION_ID,
    template_id: TEMPLATE_LOOP_ID,
    trigger_id: null,
    delivery_id: null,
    origin: 'loop',
    started_at: iso(20),
    finished_at: iso(4),
    outcome: 'success',
    report: ITERATION_2_REPORT,
    summary: "Two tests still failing: the fixture's timestamps collide.",
    cost_usd: 0.32,
    loop_id: LOOP_ID,
    iteration: 2,
    step_run_id: null,
  },
)

// --- schedules and firings --------------------------------------------------

const SCHEDULE_NIGHTLY_ID = 'sched-nightly'
const SCHEDULE_WEEKLY_ID = 'sched-weekly'

const schedules: Schedule[] = [
  {
    id: SCHEDULE_NIGHTLY_ID,
    owner_id: DEV_USER_ID,
    name: 'Nightly dependency sweep',
    template_id: TEMPLATE_LOOP_ID,
    pipeline_id: null,
    cron: '0 3 * * *',
    enabled: true,
    vars: { branch: 'main', depth: 'full' },
    last_run_at: iso(45),
    last_outcome: 'started',
    next_run_at: isoIn(60 * 7),
    created_at: iso(60 * 24 * 9),
    updated_at: iso(45),
  },
  {
    id: SCHEDULE_WEEKLY_ID,
    owner_id: DEV_USER_ID,
    name: 'Weekly changelog digest',
    template_id: templates[0]!.id,
    pipeline_id: null,
    cron: '0 9 * * 1',
    enabled: false,
    vars: {},
    last_run_at: iso(60 * 24 * 5),
    last_outcome: 'failed',
    next_run_at: null,
    created_at: iso(60 * 24 * 20),
    updated_at: iso(60 * 24 * 5),
  },
]

const firings: ScheduleFiring[] = [
  {
    id: 'fire-1',
    schedule_id: SCHEDULE_NIGHTLY_ID,
    fired_at: iso(45),
    status: 'started',
    reason: '',
    run_id: 'run-success-1',
  },
  {
    id: 'fire-2',
    schedule_id: SCHEDULE_NIGHTLY_ID,
    fired_at: iso(30),
    status: 'skipped_overlap',
    reason: 'the run this schedule started at 03:00 is still running',
    run_id: null,
  },
  {
    id: 'fire-3',
    schedule_id: SCHEDULE_NIGHTLY_ID,
    fired_at: iso(60 * 24),
    status: 'started',
    reason: '',
    run_id: 'run-needs-human-1',
  },
  {
    id: 'fire-4',
    schedule_id: SCHEDULE_WEEKLY_ID,
    fired_at: iso(60 * 24 * 5),
    status: 'failed',
    reason: 'template render error: missing field payload.branch',
    run_id: null,
  },
]

// --- fleet Gantt ------------------------------------------------------------

/** Deterministic per-session noise: the same session always draws the same
 * lane, but two sessions in the same state don't draw identical ones - a
 * fleet view where every lane has the same shape reads as a mock-up rather
 * than a fleet. (mulberry32 over an FNV-1a hash of the session id.) */
function seededRandom(id: string): () => number {
  let hash = 0x811c9dc5
  for (let i = 0; i < id.length; i += 1) {
    hash ^= id.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  let state = hash >>> 0
  return () => {
    state = (state + 0x6d2b79f5) >>> 0
    let t = state
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** What the session is doing at the live edge: the lane's last segment, so
 * the Gantt agrees with the sessions list about the here and now. */
const TAIL_KIND: Record<Session['state'], GanttKind> = {
  waiting: 'waiting',
  running: 'running',
  open: 'idle',
  closed: 'idle',
  failed: 'idle',
}

function laneFor(session: Session, from: number, to: number): GanttLane {
  const random = seededRandom(session.id)
  const span = to - from
  // A session contributes nothing before it existed, and a finished one
  // stops at its last activity rather than running to the live edge.
  const start = Math.max(from, Date.parse(session.created_at))
  const end =
    session.state === 'closed' || session.state === 'failed'
      ? Math.min(to, Math.max(start, Date.parse(session.last_active_at)))
      : to

  const segments: GanttSegment[] = []
  let cursor = start
  let kind: GanttKind = 'running'
  for (let guard = 0; cursor < end && guard < 40; guard += 1) {
    const fraction =
      kind === 'running' ? 0.05 + random() * 0.16 : kind === 'waiting' ? 0.03 + random() * 0.07 : 0.04 + random() * 0.13
    const next = Math.min(end, cursor + span * fraction)
    segments.push({ kind, start: new Date(cursor).toISOString(), end: new Date(next).toISOString() })
    cursor = next
    kind = kind === 'running' ? (random() < 0.32 ? 'waiting' : 'idle') : 'running'
  }
  const last = segments[segments.length - 1]
  if (last) last.kind = TAIL_KIND[session.state]

  return {
    session_id: session.id,
    title: session.title,
    owner: session.owner_id ? 'Jonas Thim' : 'Unattended',
    state: session.state,
    segments,
  }
}

// --- costs ------------------------------------------------------------------

function dayKey(daysAgo: number): string {
  const d = new Date()
  d.setHours(12, 0, 0, 0)
  d.setDate(d.getDate() - daysAgo)
  return d.toISOString().slice(0, 10)
}

/** A stable-per-day pseudo-random spend: enough shape for a bar chart to be
 * worth drawing, identical on every reload so screenshots and assertions
 * don't move. */
function costFor(daysAgo: number): number {
  const wave = Math.sin(daysAgo * 0.7) * 0.9 + Math.cos(daysAgo * 0.31) * 0.6
  const weekend = [0, 6].includes(new Date(`${dayKey(daysAgo)}T12:00:00Z`).getUTCDay()) ? 0.35 : 1
  return Math.round(Math.max(0.12, (2.4 + wave) * weekend) * 100) / 100
}

function costDays(days: number): CostDay[] {
  const out: CostDay[] = []
  for (let i = days - 1; i >= 0; i -= 1) {
    const usd = costFor(i)
    out.push({ day: dayKey(i), usd, sessions: Math.max(1, Math.round(usd * 3)) })
  }
  return out
}

function scaleBreakdown(rows: Array<[string, number, number]>, total: number): CostBreakdown[] {
  const weight = rows.reduce((sum, [, share]) => sum + share, 0)
  return rows
    .map(([name, share, count]) => ({ name, usd: Math.round((total * share) / weight * 100) / 100, count }))
    .sort((a, b) => b.usd - a.usd)
}

function costStats(days: number): CostStats {
  const rows = costDays(days)
  const total = Math.round(rows.reduce((sum, d) => sum + d.usd, 0) * 100) / 100
  return {
    days: rows,
    by_user: scaleBreakdown(
      [
        ['Jonas Thim', 6, 41],
        ['Unattended', 4, 28],
      ],
      total,
    ),
    by_origin: scaleBreakdown(
      [
        ['ui', 5, 22],
        ['webhook', 3, 19],
        ['schedule', 2, 17],
        ['loop', 1, 11],
      ],
      total,
    ),
    top_templates: scaleBreakdown(
      [
        [TEMPLATE_LOOP_NAME, 5, 14],
        ['Grafana alert investigation', 4, 21],
        ['Ad hoc investigation', 1, 6],
      ],
      total,
    ),
    total_usd: total,
    window: days,
  }
}

// --- helpers ----------------------------------------------------------------

function scheduleRunTitle(templateId: string): string {
  return templates.find((t) => t.id === templateId)?.name ?? 'Scheduled run'
}

/** Starts a run the way the scheduler would, records the firing, and moves
 * the schedule's own last_run_at/last_outcome - so "Run now" leaves the row
 * looking like a real firing did it. */
function fireSchedule(schedule: Schedule): Run {
  const run = startRun(schedule.template_id, null, null, scheduleRunTitle(schedule.template_id), 'schedule')
  firings.unshift({
    id: nextId('fire'),
    schedule_id: schedule.id,
    fired_at: iso(0),
    status: 'started',
    reason: '',
    run_id: run.id,
  })
  schedule.last_run_at = run.started_at
  // The scheduler stamps the firing's own word, not the run's outcome - the
  // run it just started has not finished (internal/schedules/tick.go).
  schedule.last_outcome = 'started'
  schedule.updated_at = iso(0)
  return run
}

export const schedulesHandlers = [
  // --- schedules ------------------------------------------------------------
  http.get('/api/v1/schedules', () => HttpResponse.json(schedules)),

  http.post('/api/v1/schedules', async ({ request }) => {
    const body = (await request.json()) as Partial<Schedule>
    if (!body.name?.trim()) {
      return HttpResponse.json(errorBody('invalid', 'Name is required.'), { status: 422 })
    }
    // T54: a schedule starts a template or a pipeline, never both.
    const wantsPipeline = !!body.pipeline_id
    if (wantsPipeline && !pipelines.some((p) => p.id === body.pipeline_id)) {
      return HttpResponse.json(errorBody('invalid', 'Choose a pipeline.'), { status: 422 })
    }
    if (!wantsPipeline && (!body.template_id || !templates.some((t) => t.id === body.template_id))) {
      return HttpResponse.json(errorBody('invalid', 'Choose a template.'), { status: 422 })
    }
    if (!body.cron || !parseCron(body.cron)) {
      return HttpResponse.json(errorBody('invalid', 'That is not a 5-field cron expression.'), { status: 422 })
    }
    const now = iso(0)
    const enabled = body.enabled ?? true
    const schedule: Schedule = {
      id: nextId('sched'),
      owner_id: DEV_USER_ID,
      name: body.name,
      template_id: wantsPipeline ? '' : (body.template_id ?? ''),
      pipeline_id: body.pipeline_id ?? null,
      cron: body.cron,
      enabled,
      vars: body.vars ?? {},
      last_run_at: null,
      last_outcome: '',
      next_run_at: computeNextRun(body.cron, enabled),
      created_at: now,
      updated_at: now,
    }
    schedules.push(schedule)
    return HttpResponse.json(schedule, { status: 201 })
  }),

  http.post('/api/v1/schedules/preview', async ({ request }) => {
    const body = (await request.json()) as { cron?: string }
    const preview = previewCron(body.cron ?? '')
    // The handler answers an unparseable expression with a 422 carrying the
    // dedicated `invalid_cron` code, so the dialog can point the error at
    // the cron field rather than at the form.
    if (!preview) {
      return HttpResponse.json(errorBody('invalid_cron', 'That is not a 5-field cron expression.'), { status: 422 })
    }
    return HttpResponse.json(preview)
  }),

  http.get('/api/v1/schedules/:id', ({ params }) => {
    const schedule = schedules.find((s) => s.id === params.id)
    if (!schedule) return HttpResponse.json(errorBody('not_found', 'schedule not found'), { status: 404 })
    return HttpResponse.json(schedule)
  }),

  http.patch('/api/v1/schedules/:id', async ({ request, params }) => {
    const schedule = schedules.find((s) => s.id === params.id)
    if (!schedule) return HttpResponse.json(errorBody('not_found', 'schedule not found'), { status: 404 })
    const body = (await request.json()) as Partial<Schedule>
    if (body.cron !== undefined && !parseCron(body.cron)) {
      return HttpResponse.json(errorBody('invalid', 'That is not a 5-field cron expression.'), { status: 422 })
    }
    Object.assign(schedule, body, { updated_at: iso(0) })
    schedule.next_run_at = computeNextRun(schedule.cron, schedule.enabled)
    return HttpResponse.json(schedule)
  }),

  http.delete('/api/v1/schedules/:id', ({ params }) => {
    const index = schedules.findIndex((s) => s.id === params.id)
    if (index >= 0) schedules.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.get('/api/v1/schedules/:id/firings', ({ params, request }) => {
    const limit = Number(new URL(request.url).searchParams.get('limit') ?? '50')
    const list = firings
      .filter((f) => f.schedule_id === params.id)
      .sort((a, b) => Date.parse(b.fired_at) - Date.parse(a.fired_at))
      .slice(0, limit)
    return HttpResponse.json(list)
  }),

  http.post('/api/v1/schedules/:id/run', ({ params }) => {
    const schedule = schedules.find((s) => s.id === params.id)
    if (!schedule) return HttpResponse.json(errorBody('not_found', 'schedule not found'), { status: 404 })
    const result: RunStartedResult = { run_id: fireSchedule(schedule).id }
    return HttpResponse.json(result, { status: 202 })
  }),

  // --- manual template runs (also how a loop is started by hand) ------------
  http.post('/api/v1/templates/:id/run', ({ params }) => {
    const template = templates.find((t) => t.id === params.id)
    if (!template) return HttpResponse.json(errorBody('not_found', 'template not found'), { status: 404 })
    const run = startRun(template.id, null, null, template.name, template.loop_until ? 'loop' : 'ui')
    if (template.loop_until) {
      const now = iso(0)
      const loop: Loop = {
        id: nextId('loop'),
        template_id: template.id,
        session_id: run.session_id,
        origin: 'ui',
        origin_ref: '',
        until_field: template.loop_until,
        max_iterations: template.loop_max || 5,
        iteration: 1,
        state: 'running',
        created_at: now,
        updated_at: now,
      }
      loops.unshift(loop)
      run.loop_id = loop.id
      run.iteration = 1
    }
    const result: RunStartedResult = { run_id: run.id }
    return HttpResponse.json(result, { status: 202 })
  }),

  // --- loops ----------------------------------------------------------------
  http.get('/api/v1/loops', ({ request }) => {
    const url = new URL(request.url)
    const state = url.searchParams.get('state') as LoopState | null
    const limit = Number(url.searchParams.get('limit') ?? '100')
    let list = [...loops].sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    if (state) list = list.filter((l) => l.state === state)
    return HttpResponse.json(list.slice(0, limit))
  }),

  http.get('/api/v1/loops/:id', ({ params }) => {
    const loop = loops.find((l) => l.id === params.id)
    if (!loop) return HttpResponse.json(errorBody('not_found', 'loop not found'), { status: 404 })
    const view: LoopView = {
      loop,
      runs: runs.filter((r) => r.loop_id === loop.id).sort((a, b) => a.iteration - b.iteration),
      template: templates.find((t) => t.id === loop.template_id) ?? null,
    }
    return HttpResponse.json(view)
  }),

  http.post('/api/v1/loops/:id/stop', ({ params }) => {
    const loop = loops.find((l) => l.id === params.id)
    if (!loop) return HttpResponse.json(errorBody('not_found', 'loop not found'), { status: 404 })
    if (loop.state !== 'running') {
      return HttpResponse.json(errorBody('conflict', 'This loop is not running.'), { status: 409 })
    }
    loop.state = 'stopped'
    loop.updated_at = iso(0)
    // Stopping a loop closes the session it was resuming (plan, "Semantics").
    const session = sessions.find((s) => s.id === loop.session_id)
    if (session) {
      session.state = 'closed'
      session.now_line = ''
    }
    return new HttpResponse(null, { status: 202 })
  }),

  // --- stats ----------------------------------------------------------------
  http.get('/api/v1/stats/gantt', ({ request }) => {
    const url = new URL(request.url)
    const to = Date.parse(url.searchParams.get('to') ?? '') || Date.now()
    const from = Date.parse(url.searchParams.get('from') ?? '') || to - 6 * 3_600_000
    // Sessions that were active inside the window, newest first, capped the
    // way the real endpoint caps (200).
    const lanes = sessions
      .filter((s) => Date.parse(s.last_active_at) >= from)
      .sort((a, b) => Date.parse(b.last_active_at) - Date.parse(a.last_active_at))
      .slice(0, 200)
      .map((s) => laneFor(s, from, to))
    const stats: GanttStats = { lanes }
    return HttpResponse.json(stats)
  }),

  http.get('/api/v1/stats/costs', ({ request }) => {
    const days = Math.min(90, Math.max(1, Number(new URL(request.url).searchParams.get('days') ?? '30')))
    return HttpResponse.json(costStats(days))
  }),
]

// A seeded schedule with no firing of each status would leave the drawer
// without a row to exercise its chips; verified here rather than only by an
// e2e assertion, the same guard triggersHandlers.ts keeps over deliveries.
for (const status of ['started', 'skipped_overlap', 'failed'] as const) {
  if (!firings.some((f) => f.status === status)) {
    throw new Error(`schedulesHandlers: no seeded firing with status "${status}"`)
  }
}
