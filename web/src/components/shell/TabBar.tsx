// Below 900px the rail becomes a bottom tab bar with Inbox, Sessions and
// Profile (Shell renders exactly one of Rail/TabBar, never both). A viewer
// gets a small dot on the Profile tab (T64), the phone-width equivalent of
// Rail.tsx's "Read-only" chip - there is no room here for the text label.
import { Link, useRouterState } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Activity, Clock, Inbox as InboxIcon, MessagesSquare, User, Workflow, Zap } from 'lucide-react'
import clsx from 'clsx'
import { q } from '../../api/queries'
import { useCanWrite } from '../../hooks/useCanWrite'

const ITEMS = [
  { to: '/inbox', label: 'Inbox', icon: InboxIcon },
  { to: '/sessions', label: 'Sessions', icon: MessagesSquare },
  { to: '/runs', label: 'Runs', icon: Activity },
  { to: '/triggers', label: 'Triggers', icon: Zap },
  { to: '/schedules', label: 'Schedules', icon: Clock },
  { to: '/pipelines', label: 'Pipelines', icon: Workflow },
  { to: '/profile', label: 'Profile', icon: User },
] as const

export function TabBar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const canWrite = useCanWrite()
  const approvals = useQuery(q.approvals())
  const pendingCount = approvals.data?.length ?? 0

  return (
    <nav
      data-testid="tabbar"
      aria-label="Primary"
      className="fixed inset-x-0 bottom-0 z-40 flex h-14 items-stretch border-t border-hairline bg-surface-1 min-[900px]:hidden"
    >
      {ITEMS.map(({ to, label, icon: Icon }) => {
        const active = pathname.startsWith(to)
        return (
          <Link
            key={to}
            to={to}
            className={clsx(
              // min-w-0 + a truncating label: six tabs at 390px leave each
              // one ~65px, and a label must shorten rather than push the
              // page sideways.
              'flex min-w-0 flex-1 flex-col items-center justify-center gap-1 px-0.5 text-[11px] font-medium transition-colors duration-150',
              active ? 'text-fg-primary' : 'text-fg-muted',
            )}
          >
            <span className="relative">
              <Icon size={20} aria-hidden />
              {to === '/inbox' && !!pendingCount && (
                <span
                  data-testid="inbox-badge"
                  className="absolute -right-2 -top-1.5 flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-state-attention px-1 font-mono text-[9px] font-semibold tabular-nums text-on-attention"
                >
                  {pendingCount}
                </span>
              )}
              {to === '/profile' && !canWrite && (
                <span
                  data-testid="tabbar-readonly-chip"
                  aria-label="Read-only account"
                  className="absolute -right-1 -top-1 h-2 w-2 rounded-full border border-surface-1 bg-fg-muted"
                />
              )}
            </span>
            <span className="max-w-full truncate">{label}</span>
          </Link>
        )
      })}
    </nav>
  )
}
