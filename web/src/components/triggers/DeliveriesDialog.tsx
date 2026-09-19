// "Deliveries" drawer for one trigger: every delivery it has received,
// newest first, with a status chip, the reason a non-accepted delivery was
// turned away, and a Replay button that re-runs the pipeline for that
// delivery (POST /deliveries/{id}/replay).
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { RotateCw } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import type { ReplayResult, Trigger } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { useToast } from '../../hooks/useToast'
import { Button, Dialog, DialogContent, Skeleton } from '../ui'
import { StatusChip } from './chips'

/** internal/triggers' PipelineRunRefPrefix: a delivery's run_id is a run
 * id, or a pipeline run id behind this prefix - one nullable column carries
 * both, so the link has to know which page it is opening. */
const PIPELINE_RUN_REF_PREFIX = 'pr:'

function RunLink({ runId }: { runId: string }) {
  const className = 'text-[12px] text-accent no-underline hover:underline'
  if (runId.startsWith(PIPELINE_RUN_REF_PREFIX)) {
    return (
      <Link
        to="/pipeline-runs/$id"
        params={{ id: runId.slice(PIPELINE_RUN_REF_PREFIX.length) }}
        className={className}
      >
        View pipeline run
      </Link>
    )
  }
  return (
    <Link to="/runs/$id" params={{ id: runId }} className={className}>
      View run
    </Link>
  )
}

function ReplayButton({ deliveryId }: { deliveryId: string }) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [pending, setPending] = useState(false)

  async function handleReplay() {
    setPending(true)
    try {
      const result = await api<ReplayResult>(`/api/v1/deliveries/${deliveryId}/replay`, { method: 'POST' })
      await queryClient.invalidateQueries({ queryKey: ['deliveries'] })
      void queryClient.invalidateQueries({ queryKey: ['runs'] })
      toast({
        title: 'Replay started',
        description: result.run_id?.startsWith(PIPELINE_RUN_REF_PREFIX)
          ? 'A new pipeline run is under way.'
          : result.run_id
            ? 'A new run is on the Runs page.'
            : undefined,
        tone: 'success',
      })
    } catch (err) {
      toast({ title: 'Replay failed', description: err instanceof ApiError ? err.message : undefined, tone: 'danger' })
    } finally {
      setPending(false)
    }
  }

  return (
    <Button size="sm" variant="ghost" icon={<RotateCw size={12} aria-hidden />} loading={pending} onClick={() => void handleReplay()}>
      Replay
    </Button>
  )
}

export function DeliveriesDialog({
  trigger,
  open,
  onOpenChange,
}: {
  trigger: Trigger | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const deliveries = useQuery({ ...q.deliveries(trigger?.id ?? ''), enabled: open && !!trigger })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={trigger ? `Deliveries — ${trigger.name}` : 'Deliveries'}
        srOnlyDescription="Every delivery this trigger has received, newest first."
        width={640}
      >
        <div className="flex max-h-[60vh] flex-col gap-2 overflow-y-auto">
          {deliveries.isLoading && (
            <>
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </>
          )}

          {deliveries.isSuccess && deliveries.data.length === 0 && (
            <p className="py-6 text-center text-[13px] text-fg-secondary">No deliveries yet.</p>
          )}

          {deliveries.data?.map((delivery) => (
            <div
              key={delivery.id}
              data-testid="delivery-row"
              className="flex flex-col gap-1.5 rounded-[var(--radius-control)] border border-hairline bg-surface-1 px-3 py-2.5"
            >
              <div className="flex flex-wrap items-center gap-2">
                <StatusChip status={delivery.status} />
                <span className="font-mono text-[11px] tabular-nums text-fg-muted">{relativeTime(delivery.received_at)} ago</span>
                {delivery.dedupe_key && (
                  <span className="min-w-0 truncate font-mono text-[11px] text-fg-muted">{delivery.dedupe_key}</span>
                )}
                <div className="ml-auto flex items-center gap-2">
                  {delivery.run_id && <RunLink runId={delivery.run_id} />}
                  <ReplayButton deliveryId={delivery.id} />
                </div>
              </div>
              {delivery.reason && <p className="text-[12px] text-fg-secondary">{delivery.reason}</p>}
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
