// What one node of the graph actually did. A fan-out node has one entry per
// item and a retried one has an entry per attempt, so this is a short list
// rather than a single report: each attempt names its state, its item and
// its worktree, and carries the structured report the run produced - the
// same ReportView the runs surface uses, because it is the same report.
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { X } from 'lucide-react'
import { q } from '../../api/queries'
import type { PipelineNode, StepRun } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { ReportView } from '../runs/ReportView'
import { Badge } from '../ui'
import { STEP_LABEL, STEP_TONE } from './stepState'

/** A step run's session, resolved through its run: the step row carries a
 * run id, and the transcript lives on the session that run started. */
function SessionLink({ runId }: { runId: string }) {
  const run = useQuery({ ...q.run(runId), refetchInterval: false })
  const sessionId = run.data?.session?.id
  if (!sessionId) return null
  return (
    <Link
      to="/sessions/$id"
      params={{ id: sessionId }}
      className="text-[12px] text-accent no-underline hover:underline"
    >
      Open session
    </Link>
  )
}

function StepEntry({ step }: { step: StepRun }) {
  return (
    <div className="border-t border-hairline pt-3 first:border-t-0 first:pt-0">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Badge tone={STEP_TONE[step.state]}>{STEP_LABEL[step.state]}</Badge>
        {step.attempt > 1 && (
          <Badge tone="attention" variant="outline">
            Attempt {step.attempt}
          </Badge>
        )}
        {step.started_at && (
          <span className="text-[12px] tabular-nums text-fg-muted">{relativeTime(step.started_at)} ago</span>
        )}
        {step.run_id && <SessionLink runId={step.run_id} />}
      </div>

      {step.item && <p className="mt-1.5 truncate font-mono text-[12px] text-fg-secondary">{step.item}</p>}
      {step.worktree && <p className="mt-0.5 truncate font-mono text-[11px] text-fg-muted">{step.worktree}</p>}

      <div className="mt-3">
        <ReportView report={step.report} />
      </div>
    </div>
  )
}

export function StepPanel({
  node,
  steps,
  onClose,
}: {
  node: PipelineNode
  steps: StepRun[]
  onClose: () => void
}) {
  const ordered = [...steps].sort((a, b) =>
    a.index_in_fanout === b.index_in_fanout ? a.attempt - b.attempt : a.index_in_fanout - b.index_in_fanout,
  )

  return (
    <aside
      data-testid="step-panel"
      aria-label={`Step ${node.id}`}
      className="flex w-full flex-col overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)] lg:w-[340px] lg:shrink-0"
    >
      <div className="flex items-start justify-between gap-3 border-b border-hairline px-4 py-3">
        <div className="min-w-0">
          <h2 className="truncate text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{node.id}</h2>
          <p className="mt-0.5 truncate font-mono text-[12px] text-fg-secondary">{node.template}</p>
          {/* The graph carries "this step fans out", not the expression it
              fans out over (GET /pipelines/validate's GraphNode.foreach is a
              boolean); the items themselves are the entries below. */}
          {node.foreach && <p className="mt-1 text-[11px] text-fg-muted">Runs once per item of its foreach list</p>}
        </div>
        <button
          type="button"
          aria-label="Close step"
          onClick={onClose}
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-control)] text-fg-muted outline-none transition-colors duration-[var(--duration-fast)] hover:bg-surface-2 hover:text-fg-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
        >
          <X size={14} aria-hidden />
        </button>
      </div>

      <div className="flex max-h-[520px] flex-col gap-3 overflow-y-auto px-4 py-4">
        {ordered.length === 0 ? (
          <p className="text-[13px] text-fg-secondary">This step has not started yet.</p>
        ) : (
          ordered.map((step) => <StepEntry key={step.id} step={step} />)
        )}
      </div>
    </aside>
  )
}
