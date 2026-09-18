// One loop's iterations, each a collapsible entry holding that run's report.
// An accordion rather than a list of links because the whole point of a loop
// is reading the iterations against each other - what changed between run 1
// and run 2 - without losing your place.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronRight } from 'lucide-react'
import { q } from '../../api/queries'
import type { Loop } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { OutcomeGlyph } from '../runs/OutcomeGlyph'
import { OUTCOME_LABEL } from '../runs/outcome'
import { ReportView } from '../runs/ReportView'
import { Badge, Dialog, DialogContent, Skeleton } from '../ui'
import { LOOP_STATE_LABEL, LOOP_STATE_TONE } from './loopState'

export function LoopDrawer({
  loop,
  open,
  onOpenChange,
}: {
  loop: Loop | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const view = useQuery({ ...q.loop(loop?.id ?? ''), enabled: open && !!loop })
  const [expanded, setExpanded] = useState<string | null>(null)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={view.data?.template?.name ?? 'Loop'}
        description={
          loop
            ? `Repeats until the report's ${loop.until_field} is true, at most ${loop.max_iterations} times.`
            : undefined
        }
        width={680}
      >
        <div className="flex max-h-[60vh] flex-col gap-2 overflow-y-auto">
          {view.isLoading && (
            <>
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </>
          )}

          {view.data && (
            <div className="mb-1 flex flex-wrap items-center gap-2 text-[12px] text-fg-secondary">
              <Badge tone={LOOP_STATE_TONE[view.data.loop.state]}>{LOOP_STATE_LABEL[view.data.loop.state]}</Badge>
              <span className="font-mono tabular-nums">
                {view.data.loop.iteration}/{view.data.loop.max_iterations}
              </span>
              {view.data.loop.session_id && (
                <Link
                  to="/sessions/$id"
                  params={{ id: view.data.loop.session_id }}
                  className="text-accent no-underline hover:underline"
                >
                  Open session
                </Link>
              )}
            </div>
          )}

          {view.data?.runs.map((run) => {
            const isOpen = expanded === run.id
            return (
              <div
                key={run.id}
                role="group"
                aria-label={`Iteration ${run.iteration}`}
                data-testid="loop-iteration"
                className="overflow-hidden rounded-[var(--radius-control)] border border-hairline bg-surface-1"
              >
                <button
                  type="button"
                  aria-expanded={isOpen}
                  onClick={() => setExpanded(isOpen ? null : run.id)}
                  className="flex w-full items-center gap-2 px-3 py-2.5 text-left outline-none transition-colors duration-[var(--duration-fast)] hover:bg-surface-2 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-[var(--ring)]"
                >
                  <ChevronRight
                    size={13}
                    aria-hidden
                    className={`shrink-0 text-fg-muted transition-transform duration-[var(--duration-fast)] ${isOpen ? 'rotate-90' : ''}`}
                  />
                  <OutcomeGlyph outcome={run.outcome} />
                  <span className="shrink-0 text-[13px] font-medium text-fg-primary">Iteration {run.iteration}</span>
                  <span className="min-w-0 flex-1 truncate text-[12px] text-fg-secondary">{run.summary}</span>
                  <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-muted">
                    {OUTCOME_LABEL[run.outcome]} · {relativeTime(run.started_at)} ago
                  </span>
                </button>
                {isOpen && (
                  <div className="border-t border-hairline px-3 py-3">
                    <ReportView report={run.report} />
                    <Link
                      to="/runs/$id"
                      params={{ id: run.id }}
                      className="mt-3 inline-block text-[12px] text-accent no-underline hover:underline"
                    >
                      Open this run
                    </Link>
                  </div>
                )}
              </div>
            )
          })}

          {view.isSuccess && view.data.runs.length === 0 && (
            <p className="py-6 text-center text-[13px] text-fg-secondary">This loop has no runs yet.</p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
