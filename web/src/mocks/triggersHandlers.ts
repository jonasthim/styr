// In-memory mock of every route in the v0.2 "Triggers" API contract
// (docs/superpowers/plans/2026-09-18-styr-v0.2-triggers.md, "API contract"),
// plus GET /api/v1/triggers/samples/{kind} - a contract addition this card
// made so the "Send test payload" dialog and the template Preview card have
// something to prefill/render with before T34/T35 land a real one (see this
// card's report). Spread into handlers.ts's exported `handlers` array.
//
// Seed data: one shared Grafana template, two triggers (grafana enabled,
// generic disabled), deliveries covering every status, three runs (success
// with a full report, running, needs_human) each with a webhook-origin
// session pushed into ./sessionsState's shared `sessions` array, and one
// ntfy notification channel.
import { http, HttpResponse } from 'msw'
import type {
  ApiErrorBody,
  Delivery,
  DeliveryStatus,
  NotificationChannel,
  ReplayResult,
  RunOutcome,
  RunView,
  Session,
  StructuredReport,
  Template,
  Trigger,
  TriggerCreateResult,
  TriggerKind,
  TriggerTestResult,
  Run,
} from '../api/types'
import { DEV_USER_ID, sessions } from './sessionsState'
import { renderMiniTemplate } from './renderTemplate'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

function errorBody(code: string, message: string): ApiErrorBody {
  return { error: { code, message } }
}

function slugify(name: string): string {
  const base = name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/(^-+|-+$)/g, '')
  return base || 'trigger'
}

function uniqueSlug(name: string, existing: Trigger[]): string {
  const base = slugify(name)
  let candidate = base
  let n = 2
  while (existing.some((t) => t.slug === candidate)) {
    candidate = `${base}-${n}`
    n += 1
  }
  return candidate
}

/** styr_whs_ + 32 random bytes, base64url - same shape as the real secret
 * (plan, "Secrets"), just not cryptographically sourced from the server. */
function generateSecret(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(24))
  const b64 = btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  return `styr_whs_${b64}`
}

function secretHint(secret: string): string {
  return `…${secret.slice(-6)}`
}

let idSeq = 1
function nextId(prefix: string): string {
  idSeq += 1
  return `${prefix}-${idSeq}`
}

// --- sample payloads (also GET /api/v1/triggers/samples/{kind}) ------------

const GRAFANA_SAMPLE = {
  status: 'firing',
  title: '[FIRING:1] HighMemoryUsage prod-01',
  message: 'Memory usage above 90% for 5 minutes',
  externalURL: 'https://grafana.example.com',
  commonLabels: { alertname: 'HighMemoryUsage', severity: 'critical', instance: 'prod-01' },
  commonAnnotations: { summary: 'Memory usage is critical' },
  groupLabels: { alertname: 'HighMemoryUsage' },
  alerts: [
    {
      status: 'firing',
      labels: { alertname: 'HighMemoryUsage', instance: 'prod-01', severity: 'critical' },
      annotations: {
        summary: 'Memory usage is critical',
        description: 'prod-01 has used over 90% of available memory for 5 minutes.',
      },
      startsAt: '2026-09-18T08:12:00Z',
      endsAt: '0001-01-01T00:00:00Z',
      fingerprint: 'a1b2c3d4',
      generatorURL: 'https://grafana.example.com/alerting/grafana/abc123/view',
      silenceURL: 'https://grafana.example.com/alerting/silence/new',
      dashboardURL: 'https://grafana.example.com/d/xyz',
      panelURL: 'https://grafana.example.com/d/xyz?viewPanel=2',
      values: { B: 91.4 },
    },
  ],
}

const GENERIC_SAMPLE = {
  event: 'disk.usage.high',
  host: 'prod-02',
  message: 'Disk usage is 95% on /var',
  level: 'warning',
  detected_at: '2026-09-18T08:00:00Z',
}

const GITHUB_SAMPLE = {
  action: 'completed',
  workflow_run: {
    name: 'CI',
    conclusion: 'failure',
    html_url: 'https://github.com/acme/app/actions/runs/123456',
    head_branch: 'main',
    head_sha: 'abc1234',
  },
  repository: { full_name: 'acme/app' },
}

const SAMPLES: Record<TriggerKind, Record<string, unknown>> = {
  grafana: GRAFANA_SAMPLE,
  generic: GENERIC_SAMPLE,
  github: GITHUB_SAMPLE,
}

/** Variables per kind (plan, "Template rendering"): grafana's normalised
 * Alertmanager-shaped fields sit at the top level alongside `.payload`;
 * generic/github have no normalisation defined yet, so only `.payload`. */
function templateContext(kind: TriggerKind, payload: unknown): Record<string, unknown> {
  if (kind === 'grafana' && payload && typeof payload === 'object') {
    return { payload, ...(payload as Record<string, unknown>) }
  }
  return { payload }
}

// --- templates ---------------------------------------------------------

const GRAFANA_REPORT_SCHEMA = {
  type: 'object',
  required: ['severity', 'diagnosis', 'proposed_action', 'confidence'],
  properties: {
    severity: { enum: ['info', 'warning', 'critical'] },
    diagnosis: { type: 'string' },
    evidence: { type: 'array', items: { type: 'string' } },
    proposed_action: { type: 'string' },
    confidence: { type: 'number', minimum: 0, maximum: 1 },
    resolved_itself: { type: 'boolean' },
  },
}

const TEMPLATE_GRAFANA_ID = 'tmpl-grafana'

const GRAFANA_PROMPT_TEMPLATE = `A Grafana alert is {{ .status }}. Investigate read-only and report.
{{ range .alerts }}- {{ .labels.alertname }} on {{ default "unknown" .labels.instance }}: {{ .annotations.summary }}
  {{ .annotations.description }} (since {{ .startsAt }})
{{ end }}
Use the workspace's runbooks and only read-only commands. Do not change anything.`

const templates: Template[] = [
  {
    id: TEMPLATE_GRAFANA_ID,
    owner_id: null,
    name: 'Grafana alert investigation',
    workspace_id: 'w1',
    profile_id: 'investigate',
    title_template: '{{ .status }}: {{ .commonLabels.alertname }}',
    prompt_template: GRAFANA_PROMPT_TEMPLATE,
    system_prompt: '',
    report_schema: JSON.stringify(GRAFANA_REPORT_SCHEMA, null, 2),
    created_at: iso(60 * 24 * 3),
    updated_at: iso(60 * 24),
  },
]

// --- triggers ------------------------------------------------------------

const TRIGGER_GRAFANA_ID = 'trig-grafana'
const TRIGGER_GENERIC_ID = 'trig-generic'

const triggers: Trigger[] = [
  {
    id: TRIGGER_GRAFANA_ID,
    owner_id: null,
    name: 'Prod Grafana alerts',
    slug: 'prod-grafana',
    kind: 'grafana',
    secret_hint: '…9f2a71',
    template_id: TEMPLATE_GRAFANA_ID,
    enabled: true,
    dedupe_key_template: '{{ .status }}:{{ range .alerts }}{{ .fingerprint }},{{ end }}',
    cooldown_s: 600,
    storm_cap_per_hour: 10,
    run_on_resolved: false,
    created_at: iso(60 * 24 * 3),
    updated_at: iso(60 * 24),
    last_delivery_at: iso(2),
  },
  {
    id: TRIGGER_GENERIC_ID,
    owner_id: DEV_USER_ID,
    name: 'Ad hoc webhook',
    slug: 'ad-hoc-webhook',
    kind: 'generic',
    secret_hint: '…c71ea0',
    template_id: TEMPLATE_GRAFANA_ID,
    enabled: false,
    dedupe_key_template: '',
    cooldown_s: 600,
    storm_cap_per_hour: 10,
    run_on_resolved: false,
    created_at: iso(60 * 24 * 2),
    updated_at: iso(60 * 24 * 2),
    last_delivery_at: iso(600),
  },
]

// --- runs, their webhook-origin sessions, and deliveries ------------------

const RUN_SUCCESS_ID = 'run-success-1'
const RUN_RUNNING_ID = 'run-running-1'
const RUN_NEEDS_HUMAN_ID = 'run-needs-human-1'

const SESSION_RUN_SUCCESS_ID = 'sess-run-success-1'
const SESSION_RUN_RUNNING_ID = 'sess-run-running-1'
const SESSION_RUN_NEEDS_HUMAN_ID = 'sess-run-needs-human-1'

sessions.push(
  {
    id: SESSION_RUN_SUCCESS_ID,
    owner_id: null,
    title: 'firing: HighMemoryUsage',
    workspace_id: 'w1',
    profile_id: 'investigate',
    harness: 'claude',
    state: 'closed',
    origin: 'webhook',
    origin_ref: RUN_SUCCESS_ID,
    worktree: '',
    branch: '',
    base_ref: '',
    diff_add: 0,
    diff_del: 0,
    created_at: iso(50),
    last_active_at: iso(46),
    num_turns: 6,
    cost_usd: 0.34,
    tokens_in: 8200,
    tokens_out: 1100,
    now_line: '',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: [],
  },
  {
    id: SESSION_RUN_RUNNING_ID,
    owner_id: null,
    title: 'firing: HighMemoryUsage',
    workspace_id: 'w1',
    profile_id: 'investigate',
    harness: 'claude',
    state: 'running',
    origin: 'webhook',
    origin_ref: RUN_RUNNING_ID,
    worktree: '',
    branch: '',
    base_ref: '',
    diff_add: 0,
    diff_del: 0,
    created_at: iso(2),
    last_active_at: iso(0),
    num_turns: 2,
    cost_usd: 0.06,
    tokens_in: 2100,
    tokens_out: 240,
    now_line: 'Reading journalctl output',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: [],
  },
  {
    id: SESSION_RUN_NEEDS_HUMAN_ID,
    owner_id: null,
    title: 'firing: HighMemoryUsage',
    workspace_id: 'w1',
    profile_id: 'investigate',
    harness: 'claude',
    state: 'waiting',
    origin: 'webhook',
    origin_ref: RUN_NEEDS_HUMAN_ID,
    worktree: '',
    branch: '',
    base_ref: '',
    diff_add: 0,
    diff_del: 0,
    created_at: iso(20),
    last_active_at: iso(18),
    num_turns: 4,
    cost_usd: 0.11,
    tokens_in: 3600,
    tokens_out: 410,
    now_line: 'Waiting on your decision for Bash',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: [],
  },
)

const SUCCESS_REPORT: StructuredReport = {
  severity: 'critical',
  diagnosis:
    'prod-01 memory usage climbed past 90% five minutes ago and has stayed there. The leading consumer is the styr-api process, which has grown from 400MB to 3.1GB over the last hour without a matching increase in request volume.',
  evidence: [
    'journalctl -u styr-api shows repeated "context deadline exceeded" log lines since 08:07 UTC',
    'systemctl status styr-api reports RSS 3.1G, up from a 400M baseline an hour ago',
    'no corresponding increase in nginx request rate over the same window',
  ],
  proposed_action:
    "Restart styr-api during the next maintenance window and file a leak report - the growth pattern points at the request cache never evicting, not load.",
  confidence: 0.82,
  resolved_itself: false,
}

const runs: Run[] = [
  {
    id: RUN_SUCCESS_ID,
    session_id: SESSION_RUN_SUCCESS_ID,
    template_id: TEMPLATE_GRAFANA_ID,
    trigger_id: TRIGGER_GRAFANA_ID,
    delivery_id: 'd1',
    origin: 'webhook',
    started_at: iso(50),
    finished_at: iso(46),
    outcome: 'success',
    report: SUCCESS_REPORT,
    summary: 'prod-01 memory usage climbed past 90%; styr-api looks like a slow leak, not load.',
    cost_usd: 0.34,
  },
  {
    id: RUN_RUNNING_ID,
    session_id: SESSION_RUN_RUNNING_ID,
    template_id: TEMPLATE_GRAFANA_ID,
    trigger_id: TRIGGER_GRAFANA_ID,
    delivery_id: 'd2',
    origin: 'webhook',
    started_at: iso(2),
    finished_at: null,
    outcome: 'running',
    report: null,
    summary: '',
    cost_usd: 0.06,
  },
  {
    id: RUN_NEEDS_HUMAN_ID,
    session_id: SESSION_RUN_NEEDS_HUMAN_ID,
    template_id: TEMPLATE_GRAFANA_ID,
    trigger_id: TRIGGER_GRAFANA_ID,
    delivery_id: 'd3',
    origin: 'webhook',
    started_at: iso(20),
    finished_at: null,
    outcome: 'needs_human',
    report: null,
    summary:
      "Investigation needs a decision only a human can make: the only fix available is restarting a service, which is outside the investigate profile's read-only tools.",
    cost_usd: 0.11,
  },
]

const deliveries: Delivery[] = [
  {
    id: 'd1',
    trigger_id: TRIGGER_GRAFANA_ID,
    received_at: iso(50),
    status: 'accepted',
    reason: '',
    dedupe_key: 'firing:a1b2c3d4,',
    payload: GRAFANA_SAMPLE,
    run_id: RUN_SUCCESS_ID,
  },
  {
    id: 'd2',
    trigger_id: TRIGGER_GRAFANA_ID,
    received_at: iso(2),
    status: 'accepted',
    reason: '',
    dedupe_key: 'firing:e5f6a7b8,',
    payload: GRAFANA_SAMPLE,
    run_id: RUN_RUNNING_ID,
  },
  {
    id: 'd3',
    trigger_id: TRIGGER_GRAFANA_ID,
    received_at: iso(20),
    status: 'accepted',
    reason: '',
    dedupe_key: 'firing:c9d0e1f2,',
    payload: GRAFANA_SAMPLE,
    run_id: RUN_NEEDS_HUMAN_ID,
  },
  {
    id: 'd4',
    trigger_id: TRIGGER_GRAFANA_ID,
    received_at: iso(48),
    status: 'deduped',
    reason: 'duplicate of a delivery within the dedupe window (fingerprint a1b2c3d4)',
    dedupe_key: 'firing:a1b2c3d4,',
    payload: GRAFANA_SAMPLE,
    run_id: null,
  },
  {
    id: 'd5',
    trigger_id: TRIGGER_GRAFANA_ID,
    received_at: iso(45),
    status: 'cooldown',
    reason: 'trigger is cooling down until it has been 600s since the last accepted delivery',
    dedupe_key: 'firing:a1b2c3d4,',
    payload: GRAFANA_SAMPLE,
    run_id: null,
  },
  {
    id: 'd6',
    trigger_id: TRIGGER_GRAFANA_ID,
    received_at: iso(40),
    status: 'storm',
    reason: 'storm cap reached: 10 deliveries accepted in the last hour',
    dedupe_key: 'firing:f1a2b3c4,',
    payload: GRAFANA_SAMPLE,
    run_id: null,
  },
  {
    id: 'd7',
    trigger_id: TRIGGER_GENERIC_ID,
    received_at: iso(600),
    status: 'rejected',
    reason: 'bad secret',
    dedupe_key: '',
    payload: GENERIC_SAMPLE,
    run_id: null,
  },
  {
    id: 'd8',
    trigger_id: TRIGGER_GENERIC_ID,
    received_at: iso(900),
    status: 'failed',
    reason: 'template render error: missing field payload.host',
    dedupe_key: '',
    payload: { event: 'disk.usage.high', message: 'Disk usage is 95%' },
    run_id: null,
  },
  {
    id: 'd9',
    trigger_id: TRIGGER_GENERIC_ID,
    received_at: iso(1320),
    status: 'skipped',
    reason: 'run_on_resolved is false and the event reported resolved',
    dedupe_key: '',
    payload: { ...GENERIC_SAMPLE, event: 'disk.usage.resolved' },
    run_id: null,
  },
]

// --- notification channels ------------------------------------------------

const notifications: NotificationChannel[] = [
  {
    id: 'notif-ntfy-1',
    kind: 'ntfy',
    name: 'Ops phone',
    url: 'https://ntfy.sh/styr-ops',
    events: ['run.finished', 'run.needs_human', 'run.failed'],
    enabled: true,
    token_present: true,
    created_at: iso(60 * 24 * 5),
  },
]

// --- helpers shared by test/replay -----------------------------------------

function findWorkspaceSessionOrigin(): { workspace_id: string; profile_id: string } {
  return { workspace_id: 'w1', profile_id: 'investigate' }
}

/** Starts a run "in progress" and resolves it to success ~1.2s later with a
 * synthesised report, the same shape the mock uses for workspace cloning
 * (web/src/mocks/handlers.ts's scheduleCloneOutcome) - long enough for the
 * UI's running state to be observable, short enough not to stall a test. */
function startRun(templateId: string, triggerId: string | null, deliveryId: string, title: string): Run {
  const { workspace_id, profile_id } = findWorkspaceSessionOrigin()
  const runId = nextId('run')
  const sessionId = nextId('sess-run')
  const now = iso(0)
  const session: Session = {
    id: sessionId,
    owner_id: null,
    title,
    workspace_id,
    profile_id,
    harness: 'claude',
    state: 'running',
    origin: 'webhook',
    origin_ref: runId,
    worktree: '',
    branch: '',
    base_ref: '',
    diff_add: 0,
    diff_del: 0,
    created_at: now,
    last_active_at: now,
    num_turns: 1,
    cost_usd: 0,
    tokens_in: 0,
    tokens_out: 0,
    now_line: 'Investigating…',
    model: 'claude-fable-5-1',
    effort: '',
    slash_commands: [],
  }
  sessions.push(session)

  const run: Run = {
    id: runId,
    session_id: sessionId,
    template_id: templateId,
    trigger_id: triggerId,
    delivery_id: deliveryId,
    origin: triggerId ? 'webhook' : 'test',
    started_at: now,
    finished_at: null,
    outcome: 'running',
    report: null,
    summary: '',
    cost_usd: 0,
  }
  runs.unshift(run)

  setTimeout(() => {
    const report: StructuredReport = {
      severity: 'info',
      diagnosis: `${title} - investigated read-only; nothing needed changing.`,
      evidence: ['no anomalies found in the last 15 minutes of logs'],
      proposed_action: 'No action needed; keep monitoring.',
      confidence: 0.74,
      resolved_itself: false,
    }
    run.outcome = 'success'
    run.finished_at = iso(0)
    run.report = report
    run.summary = report.diagnosis ?? ''
    run.cost_usd = 0.04
    session.state = 'closed'
    session.now_line = ''
    session.last_active_at = iso(0)
    session.cost_usd = run.cost_usd
  }, 1200)

  return run
}

/** Both run routes answer with the run row plus the records it was started
 * from, each null when unavailable - internal/api/runs_handlers.go's
 * runViewDTO (docs/openapi.yaml's RunView). */
function runView(run: Run): RunView {
  return {
    run,
    session: sessions.find((s) => s.id === run.session_id) ?? null,
    delivery: deliveries.find((d) => d.id === run.delivery_id) ?? null,
    template: templates.find((t) => t.id === run.template_id) ?? null,
  }
}

export const triggersHandlers = [
  // --- templates -----------------------------------------------------------
  http.get('/api/v1/templates', () => HttpResponse.json(templates)),

  http.post('/api/v1/templates', async ({ request }) => {
    const body = (await request.json()) as Partial<Template>
    if (!body.name || !body.name.trim()) {
      return HttpResponse.json(errorBody('invalid', 'Name is required.'), { status: 422 })
    }
    const now = iso(0)
    const template: Template = {
      id: nextId('tmpl'),
      owner_id: DEV_USER_ID,
      name: body.name,
      workspace_id: body.workspace_id ?? 'w1',
      profile_id: body.profile_id ?? 'investigate',
      title_template: body.title_template ?? '',
      prompt_template: body.prompt_template ?? '',
      system_prompt: body.system_prompt ?? '',
      report_schema: body.report_schema ?? '',
      created_at: now,
      updated_at: now,
    }
    templates.push(template)
    return HttpResponse.json(template, { status: 201 })
  }),

  http.get('/api/v1/templates/:id', ({ params }) => {
    const template = templates.find((t) => t.id === params.id)
    if (!template) return HttpResponse.json(errorBody('not_found', 'template not found'), { status: 404 })
    return HttpResponse.json(template)
  }),

  http.patch('/api/v1/templates/:id', async ({ request, params }) => {
    const template = templates.find((t) => t.id === params.id)
    if (!template) return HttpResponse.json(errorBody('not_found', 'template not found'), { status: 404 })
    const body = (await request.json()) as Partial<Template>
    Object.assign(template, body, { updated_at: iso(0) })
    return HttpResponse.json(template)
  }),

  http.delete('/api/v1/templates/:id', ({ params }) => {
    const inUse = triggers.some((t) => t.template_id === params.id)
    if (inUse) {
      return HttpResponse.json(errorBody('template_in_use', 'A trigger still uses this template.'), { status: 409 })
    }
    const index = templates.findIndex((t) => t.id === params.id)
    if (index >= 0) templates.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/v1/templates/:id/render', async ({ request, params }) => {
    const template = templates.find((t) => t.id === params.id)
    if (!template) return HttpResponse.json(errorBody('not_found', 'template not found'), { status: 404 })
    const body = (await request.json()) as {
      payload?: unknown
      kind?: TriggerKind
      // Contract addition (see this card's report): overrides so the editor's
      // Preview card can render unsaved edits, not just the last-saved
      // template - without these the "(debounced)" preview in the plan would
      // have nothing to react to while typing.
      title_template?: string
      prompt_template?: string
    }
    const kind = body.kind ?? 'grafana'
    const ctx = templateContext(kind, body.payload ?? SAMPLES[kind])
    try {
      const title = renderMiniTemplate(body.title_template ?? template.title_template, ctx)
      const prompt = renderMiniTemplate(body.prompt_template ?? template.prompt_template, ctx)
      return HttpResponse.json({ title, prompt, errors: [] })
    } catch {
      return HttpResponse.json({ title: '', prompt: '', errors: ['Could not render this template against the sample.'] })
    }
  }),

  // --- triggers --------------------------------------------------------------
  http.get('/api/v1/triggers', () => HttpResponse.json(triggers)),

  http.post('/api/v1/triggers', async ({ request }) => {
    const body = (await request.json()) as {
      name?: string
      kind?: TriggerKind
      template_id?: string
      dedupe_key_template?: string
      cooldown_s?: number
      storm_cap_per_hour?: number
      run_on_resolved?: boolean
    }
    if (!body.name || !body.name.trim()) {
      return HttpResponse.json(errorBody('invalid', 'Name is required.'), { status: 422 })
    }
    if (!body.template_id || !templates.some((t) => t.id === body.template_id)) {
      return HttpResponse.json(errorBody('invalid', 'Choose a template.'), { status: 422 })
    }
    const secret = generateSecret()
    const now = iso(0)
    const trigger: Trigger = {
      id: nextId('trig'),
      owner_id: DEV_USER_ID,
      name: body.name,
      slug: uniqueSlug(body.name, triggers),
      kind: body.kind ?? 'generic',
      secret_hint: secretHint(secret),
      template_id: body.template_id,
      enabled: true,
      dedupe_key_template: body.dedupe_key_template ?? '',
      cooldown_s: body.cooldown_s ?? 600,
      storm_cap_per_hour: body.storm_cap_per_hour ?? 10,
      run_on_resolved: body.run_on_resolved ?? false,
      created_at: now,
      updated_at: now,
      last_delivery_at: null,
    }
    triggers.push(trigger)
    const result: TriggerCreateResult = { trigger, secret }
    return HttpResponse.json(result, { status: 201 })
  }),

  http.get('/api/v1/triggers/samples/:kind', ({ params }) => {
    const kind = params.kind as TriggerKind
    if (!(kind in SAMPLES)) return HttpResponse.json(errorBody('not_found', 'unknown trigger kind'), { status: 404 })
    return HttpResponse.json(SAMPLES[kind])
  }),

  http.get('/api/v1/triggers/:id', ({ params }) => {
    const trigger = triggers.find((t) => t.id === params.id)
    if (!trigger) return HttpResponse.json(errorBody('not_found', 'trigger not found'), { status: 404 })
    return HttpResponse.json(trigger)
  }),

  http.patch('/api/v1/triggers/:id', async ({ request, params }) => {
    const trigger = triggers.find((t) => t.id === params.id)
    if (!trigger) return HttpResponse.json(errorBody('not_found', 'trigger not found'), { status: 404 })
    const body = (await request.json()) as Partial<Trigger>
    Object.assign(trigger, body, { updated_at: iso(0) })
    return HttpResponse.json(trigger)
  }),

  http.delete('/api/v1/triggers/:id', ({ params }) => {
    const index = triggers.findIndex((t) => t.id === params.id)
    if (index >= 0) triggers.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/v1/triggers/:id/rotate-secret', ({ params }) => {
    const trigger = triggers.find((t) => t.id === params.id)
    if (!trigger) return HttpResponse.json(errorBody('not_found', 'trigger not found'), { status: 404 })
    const secret = generateSecret()
    trigger.secret_hint = secretHint(secret)
    trigger.updated_at = iso(0)
    return HttpResponse.json({ secret })
  }),

  http.get('/api/v1/triggers/:id/deliveries', ({ params, request }) => {
    const url = new URL(request.url)
    const limit = Number(url.searchParams.get('limit') ?? '50')
    const list = deliveries
      .filter((d) => d.trigger_id === params.id)
      .sort((a, b) => Date.parse(b.received_at) - Date.parse(a.received_at))
      .slice(0, limit)
    return HttpResponse.json(list)
  }),

  http.post('/api/v1/triggers/:id/test', async ({ request, params }) => {
    const trigger = triggers.find((t) => t.id === params.id)
    if (!trigger) return HttpResponse.json(errorBody('not_found', 'trigger not found'), { status: 404 })
    const body = (await request.json()) as { payload?: unknown; force?: boolean }
    const payload = body.payload ?? SAMPLES[trigger.kind]
    const ctx = templateContext(trigger.kind, payload)
    let dedupeKey = ''
    try {
      dedupeKey = trigger.dedupe_key_template ? renderMiniTemplate(trigger.dedupe_key_template, ctx) : ''
    } catch {
      dedupeKey = ''
    }

    if (!body.force && dedupeKey) {
      const recentMatch = deliveries.find(
        (d) => d.trigger_id === trigger.id && d.status === 'accepted' && d.dedupe_key === dedupeKey,
      )
      if (recentMatch) {
        const delivery: Delivery = {
          id: nextId('del'),
          trigger_id: trigger.id,
          received_at: iso(0),
          status: 'deduped',
          reason: 'duplicate of a delivery within the dedupe window',
          dedupe_key: dedupeKey,
          payload,
          run_id: null,
        }
        deliveries.unshift(delivery)
        const result: TriggerTestResult = { delivery_id: delivery.id, status: 'deduped' }
        return HttpResponse.json(result, { status: 202 })
      }
    }

    const delivery: Delivery = {
      id: nextId('del'),
      trigger_id: trigger.id,
      received_at: iso(0),
      status: 'accepted',
      reason: '',
      dedupe_key: dedupeKey,
      payload,
      run_id: null,
    }
    deliveries.unshift(delivery)
    trigger.last_delivery_at = delivery.received_at

    const run = startRun(trigger.template_id, trigger.id, delivery.id, `test: ${trigger.name}`)
    delivery.run_id = run.id

    const result: TriggerTestResult = { delivery_id: delivery.id, status: 'accepted', run_id: run.id }
    return HttpResponse.json(result, { status: 202 })
  }),

  // --- deliveries --------------------------------------------------------
  http.post('/api/v1/deliveries/:id/replay', ({ params }) => {
    const original = deliveries.find((d) => d.id === params.id)
    if (!original) return HttpResponse.json(errorBody('not_found', 'delivery not found'), { status: 404 })
    const trigger = triggers.find((t) => t.id === original.trigger_id)
    if (!trigger) return HttpResponse.json(errorBody('not_found', 'trigger not found'), { status: 404 })

    const replayed: Delivery = {
      id: nextId('del'),
      trigger_id: trigger.id,
      received_at: iso(0),
      status: 'accepted',
      reason: 'replayed',
      dedupe_key: original.dedupe_key,
      payload: original.payload,
      run_id: null,
    }
    deliveries.unshift(replayed)
    trigger.last_delivery_at = replayed.received_at

    const run = startRun(trigger.template_id, trigger.id, replayed.id, `replay: ${trigger.name}`)
    replayed.run_id = run.id

    const result: ReplayResult = { run_id: run.id }
    return HttpResponse.json(result, { status: 202 })
  }),

  // --- runs ----------------------------------------------------------------
  http.get('/api/v1/runs', ({ request }) => {
    const url = new URL(request.url)
    const outcome = url.searchParams.get('outcome') as RunOutcome | null
    const triggerId = url.searchParams.get('trigger')
    const limit = Number(url.searchParams.get('limit') ?? '100')
    let list = [...runs].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))
    if (outcome) list = list.filter((r) => r.outcome === outcome)
    if (triggerId) list = list.filter((r) => r.trigger_id === triggerId)
    return HttpResponse.json(list.slice(0, limit).map(runView))
  }),

  http.get('/api/v1/runs/:id', ({ params }) => {
    const run = runs.find((r) => r.id === params.id)
    if (!run) return HttpResponse.json(errorBody('not_found', 'run not found'), { status: 404 })
    return HttpResponse.json(runView(run))
  }),

  // --- notification channels ------------------------------------------------
  http.get('/api/v1/notifications', () => HttpResponse.json(notifications)),

  http.post('/api/v1/notifications', async ({ request }) => {
    const body = (await request.json()) as {
      kind?: NotificationChannel['kind']
      name?: string
      url?: string
      token?: string
      events?: NotificationChannel['events']
    }
    if (!body.name || !body.name.trim()) {
      return HttpResponse.json(errorBody('invalid', 'Name is required.'), { status: 422 })
    }
    if (!body.url || !body.url.trim()) {
      return HttpResponse.json(errorBody('invalid', 'URL is required.'), { status: 422 })
    }
    const channel: NotificationChannel = {
      id: nextId('notif'),
      kind: body.kind ?? 'webhook',
      name: body.name,
      url: body.url,
      events: body.events && body.events.length > 0 ? body.events : ['run.finished', 'run.needs_human', 'run.failed'],
      enabled: true,
      token_present: !!body.token,
      created_at: iso(0),
    }
    notifications.push(channel)
    return HttpResponse.json(channel, { status: 201 })
  }),

  http.patch('/api/v1/notifications/:id', async ({ request, params }) => {
    const channel = notifications.find((n) => n.id === params.id)
    if (!channel) return HttpResponse.json(errorBody('not_found', 'notification channel not found'), { status: 404 })
    const body = (await request.json()) as Partial<NotificationChannel> & { token?: string }
    if (body.enabled !== undefined) channel.enabled = body.enabled
    if (body.name !== undefined) channel.name = body.name
    if (body.url !== undefined) channel.url = body.url
    if (body.events !== undefined) channel.events = body.events
    if (body.token !== undefined) channel.token_present = !!body.token
    return HttpResponse.json(channel)
  }),

  http.delete('/api/v1/notifications/:id', ({ params }) => {
    const index = notifications.findIndex((n) => n.id === params.id)
    if (index >= 0) notifications.splice(index, 1)
    return new HttpResponse(null, { status: 204 })
  }),

  http.post('/api/v1/notifications/:id/test', ({ params }) => {
    const channel = notifications.find((n) => n.id === params.id)
    if (!channel) return HttpResponse.json(errorBody('not_found', 'notification channel not found'), { status: 404 })
    return HttpResponse.json({ ok: true })
  }),
]

// Every status a delivery can carry, verified at seed time rather than only
// by an e2e assertion - a typo here would otherwise silently leave the
// deliveries drawer without a row to exercise one of its status chips.
const seededStatuses = new Set<DeliveryStatus>(deliveries.map((d) => d.status))
const ALL_STATUSES: DeliveryStatus[] = ['accepted', 'deduped', 'cooldown', 'storm', 'rejected', 'failed', 'skipped']
for (const status of ALL_STATUSES) {
  if (!seededStatuses.has(status)) throw new Error(`triggersHandlers: no seeded delivery with status "${status}"`)
}
