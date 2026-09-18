// A read-only row for a session that recently reached `closed` or `failed`.
// No keyboard triage (j/k/a/d/e/s/o) applies here — only the "Needs you"
// cards are actionable.
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import type { Session } from '../../api/types'
import { relativeTime } from './format'

const STATE_LABEL: Record<'closed' | 'failed', string> = { closed: 'closed', failed: 'failed' }
const STATE_CLASS: Record<'closed' | 'failed', string> = {
  closed: 'text-fg-muted',
  failed: 'text-state-failed',
}

export function FyiRow({ session, workspaceName }: { session: Session; workspaceName: string }) {
  const state = session.state === 'failed' ? 'failed' : 'closed'
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-[var(--radius-2)] border border-hairline bg-surface-1 px-3 py-2 min-[640px]:px-4">
      <Link
        to="/sessions/$id"
        params={{ id: session.id }}
        className="min-w-0 flex-1 truncate text-[13px] font-medium text-fg-primary hover:underline"
      >
        {session.title}
      </Link>
      <span className={clsx('shrink-0 font-mono text-[11px] font-medium tabular-nums', STATE_CLASS[state])}>
        {STATE_LABEL[state]}
      </span>
      <span className="shrink-0 font-mono text-[11px] text-fg-secondary">{workspaceName}</span>
      <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-muted">
        {relativeTime(session.last_active_at)}
      </span>
    </div>
  )
}
