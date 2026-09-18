// Run detail (`/runs/$id`): outcome, report rendered from the template's
// schema, the delivery payload that started it (collapsed), a link to the
// session and "Re-run" (replays the delivery that started this run).
import { useState } from 'react'
import { useParams, Link, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, RotateCw } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import type { ReplayResult } from '../api/types'
import { OutcomeGlyph } from '../components/runs/OutcomeGlyph'
import { OUTCOME_LABEL } from '../components/runs/outcome'
import { ReportView } from '../components/runs/ReportView'
import { CollapsibleJson } from '../components/common/CollapsibleJson'
import { useToast } from '../hooks/useToast'
import { Badge, Button, Card } from '../components/ui'

function formatDuration(startedAt: string, finishedAt: string | null): string {
  const end = finishedAt ? Date.parse(finishedAt) : Date.now()
  const seconds = Math.max(0, Math.round((end - Date.parse(startedAt)) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${minutes}m ${rest}s`
}

export function RunDetail() {
  const { id } = useParams({ from: '/_app/runs/$id' })
  const navigate = useNavigate()
  const { toast } = useToast()
  const queryClient = useQueryClient()
  const run = useQuery(q.run(id))
  const triggers = useQuery(q.triggers())
  const [replaying, setReplaying] = useState(false)

  async function handleRerun() {
    if (!run.data?.run.delivery_id) return
    setReplaying(true)
    try {
      const result = await api<ReplayResult>(`/api/v1/deliveries/${run.data.run.delivery_id}/replay`, { method: 'POST' })
      void queryClient.invalidateQueries({ queryKey: ['runs'] })
      if (result.run_id) {
        toast({ title: 'Re-run started', description: 'Opening the new run.', tone: 'success' })
        void navigate({ to: '/runs/$id', params: { id: result.run_id } })
      } else {
        toast({ title: 'Re-run started', tone: 'success' })
      }
    } catch (err) {
      toast({
        title: 'Could not start a re-run',
        description: err instanceof ApiError ? err.message : 'Something went wrong.',
        tone: 'danger',
      })
    } finally {
      setReplaying(false)
    }
  }

  if (run.isLoading) {
    return <div className="mx-auto w-full max-w-[880px] flex-1 px-4 py-6 text-[13px] text-fg-secondary sm:px-6">Loading…</div>
  }

  if (!run.data) {
    return <div className="mx-auto w-full max-w-[880px] flex-1 px-4 py-6 text-[13px] text-fg-secondary sm:px-6">Run not found.</div>
  }

  // GET /runs/{id} answers with the run row plus the records it came from
  // (internal/api/runs_handlers.go's runViewDTO); the trigger's name is not
  // one of them, so it is resolved from the triggers list the same way the
  // runs list does it.
  const { run: detail, session, delivery, loop } = run.data
  const triggerName = detail.trigger_id ? (triggers.data?.find((t) => t.id === detail.trigger_id)?.name ?? null) : null

  return (
    <div className="mx-auto flex w-full max-w-[880px] flex-1 flex-col px-4 py-6 sm:px-6">
      <Link to="/runs" className="inline-flex w-fit items-center gap-1.5 text-[12px] text-fg-secondary no-underline hover:text-fg-primary">
        <ArrowLeft size={13} aria-hidden />
        Runs
      </Link>

      <div className="mt-3 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <OutcomeGlyph outcome={detail.outcome} />
            <h1 className="truncate text-[20px] font-semibold leading-7 tracking-[-0.02em] text-fg-primary">
              {detail.summary || OUTCOME_LABEL[detail.outcome]}
            </h1>
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-fg-secondary">
            <Badge tone={detail.outcome === 'running' ? 'running' : 'neutral'}>{OUTCOME_LABEL[detail.outcome]}</Badge>
            {/* A run inside a loop is one of several; the chip says which,
                and goes to the loop holding the rest. */}
            {loop && (
              <Link
                to="/runs/loops"
                data-testid="loop-chip"
                className="no-underline"
                title="Open the loop this run belongs to"
              >
                <Badge tone="accent" variant="outline">
                  Iteration {detail.iteration} of {loop.max_iterations}
                </Badge>
              </Link>
            )}
            {triggerName && <span className="font-mono">{triggerName}</span>}
            <span aria-hidden>·</span>
            <span className="font-mono tabular-nums">{formatDuration(detail.started_at, detail.finished_at)}</span>
            <span aria-hidden>·</span>
            <span className="font-mono tabular-nums">${detail.cost_usd.toFixed(2)}</span>
            {session && (
              <>
                <span aria-hidden>·</span>
                <Link to="/sessions/$id" params={{ id: session.id }} className="text-accent no-underline hover:underline">
                  Open session
                </Link>
              </>
            )}
          </div>
        </div>
        {detail.delivery_id && (
          <Button
            size="sm"
            icon={<RotateCw size={13} aria-hidden />}
            loading={replaying}
            onClick={() => void handleRerun()}
          >
            Re-run
          </Button>
        )}
      </div>

      <Card className="mt-5" title="Report">
        <ReportView report={detail.report} />
      </Card>

      {delivery && (
        <div className="mt-5">
          <CollapsibleJson label="Delivery payload" value={delivery.payload} />
        </div>
      )}
    </div>
  )
}
