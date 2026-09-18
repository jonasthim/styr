// The seeded pipelines, pipeline runs and step runs, split out of
// ./pipelinesHandlers so ./triggersHandlers can resolve a run's step and
// pipeline for GET /runs/{id}'s RunView without importing the module that
// imports it back (the same split ./loopsState makes for T49's loops).
// Leaf module: types only, no other mock imports.
import type { Pipeline, PipelineRun, StepRun, StructuredReport } from '../api/types'

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

export const PIPELINE_FIX_CI_ID = 'pipe-fix-ci'
export const PIPELINE_RELEASE_ID = 'pipe-release-notes'

export const PIPELINE_RUN_RUNNING_ID = 'prun-running-1'
export const PIPELINE_RUN_DONE_ID = 'prun-done-1'

// The run ids the seeded step runs point at; ./pipelinesHandlers creates the
// matching Run rows and their sessions in ./triggersHandlers's arrays.
export const STEP_RUN_IDS = {
  triage: 'run-step-triage',
  fixAttempt1: 'run-step-fix-1',
  fixAttempt2: 'run-step-fix-2',
  reviewA: 'run-step-review-a',
  reviewB: 'run-step-review-b',
  draft: 'run-step-draft',
  publish: 'run-step-publish',
} as const

/** The plan's own worked example (docs/superpowers/plans/
 * 2026-09-19-styr-v0.5-pipelines.md, "YAML definition"): four steps, three
 * dependencies, one of them a fan-out. */
export const FIX_CI_YAML = `name: fix-ci
workspace: styr
timeout: 2h
steps:
  - id: triage
    template: "CI failure triage"
    with: { alert: "{{ .payload.title }}" }
  - id: fix
    needs: [triage]
    template: "Apply fix"
    with: { plan: "{{ .steps.triage.report.proposed_action }}" }
    worktree: own
    retries: 1
  - id: verify
    needs: [fix]
    template: "Run tests and report"
    worktree: shared
  - id: review-each
    needs: [triage]
    foreach: "{{ .steps.triage.report.files }}"
    template: "Review file"
`

const RELEASE_YAML = `name: release-notes
workspace: styr
timeout: 30m
steps:
  - id: draft
    template: "Draft the release notes"
  - id: publish
    needs: [draft]
    template: "Open the release PR"
    with: { notes: "{{ .steps.draft.report.proposed_action }}" }
    worktree: shared
`

/** The skeleton "New from template" starts from: two sequential steps, named
 * so the first thing to do with it is obvious. */
export const STARTER_YAML = `name: new-pipeline
workspace: styr
timeout: 1h
steps:
  - id: investigate
    template: "CI failure triage"
  - id: act
    needs: [investigate]
    template: "Apply fix"
    with: { plan: "{{ .steps.investigate.report.proposed_action }}" }
    worktree: own
`

export const pipelines: Pipeline[] = [
  {
    id: PIPELINE_FIX_CI_ID,
    owner_id: null,
    name: 'fix-ci',
    workspace_id: 'w1',
    yaml: FIX_CI_YAML,
    created_at: iso(60 * 24 * 4),
    updated_at: iso(60 * 5),
  },
  {
    id: PIPELINE_RELEASE_ID,
    owner_id: null,
    name: 'release-notes',
    workspace_id: 'w1',
    yaml: RELEASE_YAML,
    created_at: iso(60 * 24 * 2),
    updated_at: iso(60 * 20),
  },
]

const TRIAGE_REPORT: StructuredReport = {
  severity: 'warning',
  diagnosis:
    'The pipeline job fails in TestEngineStart: a flaky integration test races the scheduler, and two of the three failures on main share the same stack.',
  evidence: [
    'internal/runs/engine_test.go:142 fails on 2 of the last 3 main builds',
    'both failures show "context deadline exceeded" inside slots.acquire',
    'the job passes on a rerun with -count=1',
  ],
  proposed_action: 'Serialise the slot acquisition in the test fixture, then re-run the suite to confirm.',
  confidence: 0.78,
  resolved_itself: false,
}

const REVIEW_REPORT: StructuredReport = {
  severity: 'info',
  diagnosis: 'internal/api/runs_handlers.go reads cleanly; the new field is serialised the same way its neighbours are.',
  evidence: ['no behaviour change outside the DTO'],
  proposed_action: 'No change needed.',
  confidence: 0.9,
  resolved_itself: false,
}

const DRAFT_REPORT: StructuredReport = {
  severity: 'info',
  diagnosis: 'Drafted notes for v0.4.1 from 23 commits since the last tag.',
  proposed_action: 'Open the release PR with these notes.',
  confidence: 0.85,
}

// One pipeline run mid-flight and one finished, so the run page has both a
// live graph and a settled one to render.
export const pipelineRuns: PipelineRun[] = [
  {
    id: PIPELINE_RUN_RUNNING_ID,
    pipeline_id: PIPELINE_FIX_CI_ID,
    origin: 'ui',
    origin_ref: '',
    input: { branch: 'main' },
    state: 'running',
    started_at: iso(22),
    finished_at: null,
    cost_usd: 0.51,
  },
  {
    id: PIPELINE_RUN_DONE_ID,
    pipeline_id: PIPELINE_RELEASE_ID,
    origin: 'schedule',
    origin_ref: 'sched-release',
    input: {},
    state: 'success',
    started_at: iso(180),
    finished_at: iso(171),
    cost_usd: 0.18,
  },
]

// The running run: triage done, fix failed once and is on its second
// attempt, verify still waiting, and the fan-out half way through its two
// items - every state the graph has to draw, in one run.
export const stepRuns: StepRun[] = [
  {
    id: 'step-triage',
    pipeline_run_id: PIPELINE_RUN_RUNNING_ID,
    step_id: 'triage',
    index_in_fanout: 0,
    item: '',
    run_id: STEP_RUN_IDS.triage,
    attempt: 1,
    state: 'success',
    report: TRIAGE_REPORT,
    started_at: iso(22),
    finished_at: iso(18),
    worktree: '/srv/styr/worktrees/fix-ci-triage',
  },
  {
    id: 'step-fix-1',
    pipeline_run_id: PIPELINE_RUN_RUNNING_ID,
    step_id: 'fix',
    index_in_fanout: 0,
    item: '',
    run_id: STEP_RUN_IDS.fixAttempt1,
    attempt: 1,
    state: 'failed',
    report: null,
    started_at: iso(18),
    finished_at: iso(11),
    worktree: '/srv/styr/worktrees/fix-ci-fix',
  },
  {
    id: 'step-fix-2',
    pipeline_run_id: PIPELINE_RUN_RUNNING_ID,
    step_id: 'fix',
    index_in_fanout: 0,
    item: '',
    run_id: STEP_RUN_IDS.fixAttempt2,
    attempt: 2,
    state: 'running',
    report: null,
    started_at: iso(10),
    finished_at: null,
    worktree: '/srv/styr/worktrees/fix-ci-fix-2',
  },
  {
    id: 'step-verify',
    pipeline_run_id: PIPELINE_RUN_RUNNING_ID,
    step_id: 'verify',
    index_in_fanout: 0,
    item: '',
    run_id: null,
    attempt: 1,
    state: 'pending',
    report: null,
    started_at: null,
    finished_at: null,
    worktree: '',
  },
  {
    id: 'step-review-0',
    pipeline_run_id: PIPELINE_RUN_RUNNING_ID,
    step_id: 'review-each',
    index_in_fanout: 0,
    item: 'internal/api/runs_handlers.go',
    run_id: STEP_RUN_IDS.reviewA,
    attempt: 1,
    state: 'success',
    report: REVIEW_REPORT,
    started_at: iso(17),
    finished_at: iso(14),
    worktree: '/srv/styr/worktrees/fix-ci-review-0',
  },
  {
    id: 'step-review-1',
    pipeline_run_id: PIPELINE_RUN_RUNNING_ID,
    step_id: 'review-each',
    index_in_fanout: 1,
    item: 'internal/runs/engine.go',
    run_id: STEP_RUN_IDS.reviewB,
    attempt: 1,
    state: 'running',
    report: null,
    started_at: iso(17),
    finished_at: null,
    worktree: '/srv/styr/worktrees/fix-ci-review-1',
  },
  {
    id: 'step-draft',
    pipeline_run_id: PIPELINE_RUN_DONE_ID,
    step_id: 'draft',
    index_in_fanout: 0,
    item: '',
    run_id: STEP_RUN_IDS.draft,
    attempt: 1,
    state: 'success',
    report: DRAFT_REPORT,
    started_at: iso(180),
    finished_at: iso(175),
    worktree: '/srv/styr/worktrees/release-draft',
  },
  {
    id: 'step-publish',
    pipeline_run_id: PIPELINE_RUN_DONE_ID,
    step_id: 'publish',
    index_in_fanout: 0,
    item: '',
    run_id: STEP_RUN_IDS.publish,
    attempt: 1,
    state: 'success',
    report: null,
    started_at: iso(175),
    finished_at: iso(171),
    worktree: '/srv/styr/worktrees/release-draft',
  },
]

export function findPipeline(id: string | null): Pipeline | null {
  if (!id) return null
  return pipelines.find((p) => p.id === id) ?? null
}

/** The step a run was started by, and the pipeline that step belongs to -
 * what GET /runs/{id} adds to a RunView so the run page can say "Step of
 * pipeline fix-ci". Both null for a run outside a pipeline. */
export function findStepContext(runId: string): { step: StepRun | null; pipeline: Pipeline | null } {
  const step = stepRuns.find((s) => s.run_id === runId) ?? null
  if (!step) return { step: null, pipeline: null }
  const run = pipelineRuns.find((r) => r.id === step.pipeline_run_id)
  return { step, pipeline: findPipeline(run?.pipeline_id ?? null) }
}
