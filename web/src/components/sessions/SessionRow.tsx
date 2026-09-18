// One 32px row in the sessions list (Task 19). The whole row is a link
// (rows are links; Enter opens - card rule), with a single aria-label that
// carries the row's state, workspace and stats, so StateGlyph and Sparkline
// can both stay purely decorative (aria-hidden / role=img with their own
// narrow label).
import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import type { Session, SessionEvent } from '../../api/types'
import { StateGlyph, STATE_LABEL } from './StateGlyph'
import { Sparkline } from './Sparkline'

const SPARKLINE_BUCKETS = 15 // one bucket per minute, last 15 minutes

/** True for an `assistant` envelope whose content includes a tool_use block. */
function isToolUseEnvelope(payload: unknown): boolean {
  if (typeof payload !== 'object' || payload === null) return false
  const envelope = payload as { type?: string; message?: { content?: unknown } }
  if (envelope.type !== 'assistant') return false
  const content = envelope.message?.content
  if (!Array.isArray(content)) return false
  return content.some((block) => typeof block === 'object' && block !== null && (block as { type?: string }).type === 'tool_use')
}

function toolEventsPerMinute(events: SessionEvent[], now: number): number[] {
  const buckets: number[] = Array.from({ length: SPARKLINE_BUCKETS }, () => 0)
  const windowStart = now - SPARKLINE_BUCKETS * 60_000
  for (const event of events) {
    if (!isToolUseEnvelope(event.payload)) continue
    const at = Date.parse(event.at)
    if (Number.isNaN(at) || at < windowStart || at > now) continue
    const bucket = Math.min(SPARKLINE_BUCKETS - 1, Math.floor((at - windowStart) / 60_000))
    buckets[bucket] += 1
  }
  return buckets
}

/** Reads whatever this session's events query already has cached, without
 * triggering a fetch of its own - the card's "derive... if loaded, else...
 * fallback". A session list with dozens of rows must not fire one events
 * request per row just to draw a sparkline. */
function useSparklineValues(session: Session): { values: number[]; flat: boolean } {
  const queryClient = useQueryClient()
  const cached = queryClient.getQueryData<SessionEvent[]>(['session-events', session.id])
  return useMemo(() => {
    if (cached && cached.length > 0) {
      return { values: toolEventsPerMinute(cached, Date.now()), flat: false }
    }
    return { values: [], flat: true }
  }, [cached])
}

function relativeTime(iso: string): string {
  const deltaMs = Date.now() - Date.parse(iso)
  if (Number.isNaN(deltaMs)) return ''
  const seconds = Math.max(0, Math.round(deltaMs / 1000))
  if (seconds < 60) return 'now'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h`
  const days = Math.round(hours / 24)
  return `${days}d`
}

const GRID_COLS =
  'grid-cols-[16px_minmax(0,1fr)_auto_auto] ' +
  'sm:grid-cols-[16px_minmax(0,1fr)_88px_auto_auto] ' +
  'md:grid-cols-[16px_minmax(0,1fr)_88px_minmax(0,1fr)_auto_auto] ' +
  'lg:grid-cols-[16px_minmax(0,1fr)_88px_minmax(0,1fr)_48px_auto_auto]'

export function SessionRow({ session, workspaceName }: { session: Session; workspaceName: string }) {
  const { values, flat } = useSparklineValues(session)
  const title = session.title || 'Untitled session'
  const turnsCost = `${session.num_turns} · $${session.cost_usd.toFixed(2)}`
  const ago = relativeTime(session.last_active_at)
  const ariaLabel = [title, STATE_LABEL[session.state], workspaceName, turnsCost, ago && `${ago} ago`]
    .filter(Boolean)
    .join(', ')

  return (
    <Link
      to="/sessions/$id"
      params={{ id: session.id }}
      data-testid="session-row"
      aria-label={ariaLabel}
      className={`grid h-[var(--row-h)] items-center gap-x-3 border-b border-hairline px-3 text-fg-primary no-underline transition-colors duration-150 hover:bg-surface-2 ${GRID_COLS}`}
    >
      <StateGlyph state={session.state} />
      <span aria-hidden className="min-w-0 truncate font-medium">
        {title}
      </span>
      <span aria-hidden className="hidden min-w-0 truncate font-mono text-[12px] text-fg-secondary sm:block">
        {workspaceName}
      </span>
      <span aria-hidden className="hidden min-w-0 truncate text-[12px] text-fg-secondary md:block">
        {session.now_line}
      </span>
      <Sparkline className="hidden lg:block" values={values} flat={flat} />
      <span aria-hidden className="shrink-0 text-right font-mono text-[12px] tabular-nums text-fg-secondary">
        {turnsCost}
      </span>
      <span aria-hidden className="shrink-0 text-right text-[12px] tabular-nums text-fg-muted">
        {ago}
      </span>
    </Link>
  )
}
