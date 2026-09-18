// One row in the Runs list: outcome glyph, title, trigger, started (relative),
// duration and cost - the whole row is a link, matching the sessions list's
// row-is-a-link convention (components/sessions/SessionRow.tsx).
import { Link } from '@tanstack/react-router'
import type { Run } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { OutcomeGlyph } from './OutcomeGlyph'
import { OUTCOME_LABEL } from './outcome'

function formatDuration(startedAt: string, finishedAt: string | null): string {
  const end = finishedAt ? Date.parse(finishedAt) : Date.now()
  const seconds = Math.max(0, Math.round((end - Date.parse(startedAt)) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.round(minutes / 60)
  return `${hours}h`
}

const GRID_COLS =
  'grid-cols-[16px_minmax(0,1fr)_auto_auto] ' +
  'sm:grid-cols-[16px_minmax(0,1fr)_72px_auto_auto] ' +
  'md:grid-cols-[16px_minmax(0,1fr)_120px_72px_auto_auto]'

export function RunRow({ run, triggerName }: { run: Run; triggerName: string }) {
  const title = run.summary || OUTCOME_LABEL[run.outcome]
  const ago = relativeTime(run.started_at)
  const duration = formatDuration(run.started_at, run.finished_at)
  const ariaLabel = [OUTCOME_LABEL[run.outcome], title, triggerName, `started ${ago} ago`, duration, `$${run.cost_usd.toFixed(2)}`]
    .filter(Boolean)
    .join(', ')

  return (
    <Link
      to="/runs/$id"
      params={{ id: run.id }}
      data-testid="run-row"
      aria-label={ariaLabel}
      className={`grid h-[var(--row-h)] items-center gap-x-3 border-b border-hairline px-3 text-fg-primary no-underline transition-colors duration-150 last:border-b-0 hover:bg-surface-2 ${GRID_COLS}`}
    >
      <OutcomeGlyph outcome={run.outcome} />
      <span aria-hidden className="min-w-0 truncate font-medium">
        {title}
      </span>
      <span aria-hidden className="hidden min-w-0 truncate font-mono text-[12px] text-fg-secondary md:block">
        {triggerName}
      </span>
      <span aria-hidden className="hidden shrink-0 text-right font-mono text-[12px] tabular-nums text-fg-secondary sm:block">
        {duration}
      </span>
      <span aria-hidden className="shrink-0 text-right font-mono text-[12px] tabular-nums text-fg-secondary">
        ${run.cost_usd.toFixed(2)}
      </span>
      <span aria-hidden className="shrink-0 text-right text-[12px] tabular-nums text-fg-muted">
        {ago}
      </span>
    </Link>
  )
}
