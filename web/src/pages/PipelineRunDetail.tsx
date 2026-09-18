// Pipeline run (`/pipeline-runs/$id`): the graph live, a step's own report
// beside it, and the flat log underneath. The header carries the only three
// numbers that matter while a chain of agents is running - state, elapsed,
// spend - and the two things you can do about it.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, RotateCw, Square } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import { PipelineGraph } from '../components/pipelines/PipelineGraph'
import { StepLog } from '../components/pipelines/StepLog'
import { StepPanel } from '../components/pipelines/StepPanel'
import { RUN_STATE_LABEL, RUN_STATE_TONE } from '../components/pipelines/stepState'
import { useToast } from '../hooks/useToast'
import { Badge, Button, Dialog, DialogContent } from '../components/ui'

function elapsed(startedAt: string, finishedAt: string | null, now: number): string {
  const end = finishedAt ? Date.parse(finishedAt) : now
  const seconds = Math.max(0, Math.round((end - Date.parse(startedAt)) / 1000))
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ${String(seconds % 60).padStart(2, '0')}s`
  return `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m`
}

export function PipelineRunDetail() {
  const { id } = useParams({ from: '/_app/pipeline-runs/$id' })
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const runQuery = useQuery(q.pipelineRun(id))
  const [activeStepId, setActiveStepId] = useState<string | null>(null)
  const [confirmCancel, setConfirmCancel] = useState(false)
  const [busy, setBusy] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  const state = runQuery.data?.run.state
  useEffect(() => {
    if (state !== 'running') return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [state])

  const steps = useMemo(() => runQuery.data?.steps ?? [], [runQuery.data])
  // Stable across the elapsed clock's re-renders, so the graph is not handed
  // a fresh node array every second.
  const selectStep = useCallback((stepId: string) => {
    setActiveStepId((prev) => (prev === stepId ? null : stepId))
  }, [])
  const activeNode = runQuery.data?.graph.nodes.find((n) => n.id === activeStepId) ?? null

  async function act(path: string, success: string) {
    setBusy(true)
    try {
      await api(`/api/v1/pipeline-runs/${id}/${path}`, { method: 'POST' })
      await queryClient.invalidateQueries({ queryKey: ['pipeline-run', id] })
      void queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
      toast({ title: success, tone: 'success' })
    } catch (err) {
      toast({
        title: 'That did not work',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setBusy(false)
      setConfirmCancel(false)
    }
  }

  if (runQuery.isLoading || !runQuery.data) {
    return (
      <div className="mx-auto w-full max-w-[1180px] flex-1 px-4 py-6 text-[13px] text-fg-secondary sm:px-6">
        {runQuery.isLoading ? 'Loading…' : 'Pipeline run not found.'}
      </div>
    )
  }

  const { run, pipeline, graph } = runQuery.data
  const hasFailed = steps.some((s) => s.state === 'failed')

  return (
    <div className="mx-auto flex w-full max-w-[1180px] flex-1 flex-col px-4 py-6 sm:px-6">
      <Link
        to="/runs/pipelines"
        className="inline-flex w-fit items-center gap-1.5 text-[12px] text-fg-secondary no-underline hover:text-fg-primary"
      >
        <ArrowLeft size={13} aria-hidden />
        Pipeline runs
      </Link>

      <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate text-[20px] font-semibold leading-7 tracking-[-0.02em] text-fg-primary">
            {pipeline?.name ?? 'Pipeline run'}
          </h1>
          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-fg-secondary">
            <span data-testid="pipeline-run-state">
              <Badge tone={RUN_STATE_TONE[run.state]}>{RUN_STATE_LABEL[run.state]}</Badge>
            </span>
            <span className="font-mono tabular-nums">{elapsed(run.started_at, run.finished_at, now)}</span>
            <span aria-hidden>·</span>
            <span className="font-mono tabular-nums">${run.cost_usd.toFixed(2)}</span>
            <span aria-hidden>·</span>
            <span className="font-mono">
              {steps.filter((s) => s.state === 'success').length}/{steps.length} steps done
            </span>
            {pipeline && (
              <Link
                to="/pipelines/$id"
                params={{ id: pipeline.id }}
                className="text-accent no-underline hover:underline"
              >
                Edit definition
              </Link>
            )}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {hasFailed && (
            <Button
              icon={<RotateCw size={13} aria-hidden />}
              loading={busy}
              onClick={() => void act('retry-failed', 'Retrying the failed steps')}
            >
              Retry failed
            </Button>
          )}
          {run.state === 'running' && (
            <Button variant="danger" icon={<Square size={13} aria-hidden />} onClick={() => setConfirmCancel(true)}>
              Cancel run
            </Button>
          )}
        </div>
      </div>

      <div className="mt-5 flex flex-col gap-4 lg:flex-row lg:items-start">
        <PipelineGraph
          // Only flex-1 once the panel is beside it: in the phone's column
          // layout a flex-1 basis would fight the graph's own height.
          className="min-w-0 lg:flex-1"
          graph={graph}
          steps={steps}
          activeStepId={activeStepId}
          onSelect={selectStep}
          height={440}
        />
        {activeNode && (
          <StepPanel
            node={activeNode}
            steps={steps.filter((s) => s.step_id === activeNode.id)}
            onClose={() => setActiveStepId(null)}
          />
        )}
      </div>

      <h2 className="mt-8 text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Steps</h2>
      <div className="mt-3">
        <StepLog steps={steps} onSelect={setActiveStepId} />
      </div>

      <Dialog open={confirmCancel} onOpenChange={setConfirmCancel}>
        <DialogContent
          title="Cancel this pipeline run?"
          description="Running steps are interrupted and everything still waiting is skipped. Steps that already finished keep their work."
          width={440}
          footer={
            <>
              <Button onClick={() => setConfirmCancel(false)}>Keep running</Button>
              <Button
                variant="danger"
                loading={busy}
                onClick={() => void act('cancel', 'Pipeline run cancelled')}
              >
                Cancel the run
              </Button>
            </>
          }
        />
      </Dialog>
    </div>
  )
}
