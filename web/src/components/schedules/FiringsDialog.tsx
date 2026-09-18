// "Firings" drawer for one schedule: every tick the scheduler took on it,
// newest first. Mirrors the triggers deliveries drawer, because they answer
// the same question - why did (or didn't) this thing start a run?
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { q } from '../../api/queries'
import type { Schedule } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { Dialog, DialogContent, Skeleton } from '../ui'
import { absoluteTime, FiringStatusChip } from './chips'

export function FiringsDialog({
  schedule,
  open,
  onOpenChange,
}: {
  schedule: Schedule | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const firings = useQuery({ ...q.scheduleFirings(schedule?.id ?? ''), enabled: open && !!schedule })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={schedule ? `Firings — ${schedule.name}` : 'Firings'}
        srOnlyDescription="Every tick the scheduler took on this schedule, newest first."
        width={640}
      >
        <div className="flex max-h-[60vh] flex-col gap-2 overflow-y-auto">
          {firings.isLoading && (
            <>
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </>
          )}

          {firings.isSuccess && firings.data.length === 0 && (
            <p className="py-6 text-center text-[13px] text-fg-secondary">
              This schedule hasn't fired yet.
            </p>
          )}

          {firings.data?.map((firing) => (
            <div
              key={firing.id}
              data-testid="firing-row"
              className="flex flex-col gap-1.5 rounded-[var(--radius-control)] border border-hairline bg-surface-1 px-3 py-2.5"
            >
              <div className="flex flex-wrap items-center gap-2">
                <FiringStatusChip status={firing.status} />
                <span
                  title={absoluteTime(firing.fired_at)}
                  className="font-mono text-[11px] tabular-nums text-fg-muted"
                >
                  {relativeTime(firing.fired_at)} ago
                </span>
                {firing.run_id && (
                  <Link
                    to="/runs/$id"
                    params={{ id: firing.run_id }}
                    className="ml-auto text-[12px] text-accent no-underline hover:underline"
                  >
                    View run
                  </Link>
                )}
              </div>
              {firing.reason && <p className="text-[12px] text-fg-secondary">{firing.reason}</p>}
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
