// Desktop (>= 900px) left rail: 56px icons-only, expands to 220px on hover
// or when pinned (`[`). Active item gets a 2px accent bar on the left.
// Settings only shows for admins. Profile (with avatar) anchors the bottom,
// above the live-connection dot.
import { useState, type ComponentType } from 'react'
import { Link, useRouterState } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import * as Tooltip from '@radix-ui/react-tooltip'
import { FolderKanban, Inbox as InboxIcon, MessagesSquare, Settings as SettingsIcon } from 'lucide-react'
import clsx from 'clsx'
import { q } from '../../api/queries'
import { useMe } from '../../hooks/useMe'
import { useUiStore } from '../../store/ui'
import { useLiveStatusStore } from '../../store/live'
import { ThemeToggle } from './ThemeToggle'

interface NavItemDef {
  to: string
  label: string
  icon: ComponentType<{ size?: number; className?: string; 'aria-hidden'?: boolean }>
  badge?: number
}

function RailLink({ item, expanded, active }: { item: NavItemDef; expanded: boolean; active: boolean }) {
  const Icon = item.icon
  return (
    <Link
      to={item.to}
      data-testid={item.to === '/inbox' ? 'rail-inbox-link' : undefined}
      className={clsx(
        'group relative flex h-9 items-center gap-3 rounded-[var(--radius-1)] px-2.5 text-[13px] font-medium text-fg-secondary transition-colors duration-150 hover:bg-surface-2 hover:text-fg-primary',
        active && 'text-fg-primary',
      )}
    >
      {active && <span className="absolute bottom-1 left-0 top-1 w-0.5 rounded-full bg-accent" />}
      <Icon size={18} className="shrink-0" aria-hidden />
      <span
        className={clsx(
          'min-w-0 flex-1 overflow-hidden whitespace-nowrap transition-opacity duration-150',
          expanded ? 'opacity-100' : 'w-0 opacity-0',
        )}
      >
        {item.label}
      </span>
      {!!item.badge && (
        <span
          data-testid="inbox-badge"
          className="ml-auto flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-state-attention px-1 font-mono text-[10px] font-semibold tabular-nums text-on-attention"
        >
          {item.badge}
        </span>
      )}
    </Link>
  )
}

function LiveDot() {
  const connected = useLiveStatusStore((s) => s.connected)
  return (
    <Tooltip.Root delayDuration={200}>
      <Tooltip.Trigger asChild>
        <span
          data-testid="live-dot"
          aria-label={connected ? 'Live updates connected' : 'Reconnecting to live updates'}
          className={clsx(
            'absolute bottom-2 left-2 h-1.5 w-1.5 rounded-full',
            connected ? 'bg-state-running' : 'bg-state-attention',
          )}
        />
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content
          side="right"
          sideOffset={8}
          className="rounded-[var(--radius-1)] border border-hairline bg-surface-3 px-2 py-1 text-[12px] text-fg-primary shadow-[var(--shadow-popover)]"
        >
          {connected ? 'Live updates connected' : 'Reconnecting…'}
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip.Root>
  )
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase())
    .join('')
}

export function Rail() {
  const [hovering, setHovering] = useState(false)
  const pinned = useUiStore((s) => s.railPinned)
  const expanded = pinned || hovering
  const { data: me } = useMe()
  const approvals = useQuery(q.approvals())
  const pendingCount = approvals.data?.length ?? 0
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  const items: NavItemDef[] = [
    { to: '/inbox', label: 'Inbox', icon: InboxIcon, badge: pendingCount },
    { to: '/sessions', label: 'Sessions', icon: MessagesSquare },
    { to: '/workspaces', label: 'Workspaces', icon: FolderKanban },
  ]
  if (me?.role === 'admin') {
    items.push({ to: '/settings', label: 'Settings', icon: SettingsIcon })
  }

  return (
    <Tooltip.Provider>
      <nav
        data-testid="rail"
        aria-label="Primary"
        onMouseEnter={() => setHovering(true)}
        onMouseLeave={() => setHovering(false)}
        className={clsx(
          'fixed inset-y-0 left-0 z-40 hidden flex-col border-r border-hairline bg-surface-1 transition-[width] duration-150 ease-out min-[900px]:flex',
          expanded ? 'w-[220px]' : 'w-14',
        )}
      >
        <div className="flex flex-1 flex-col gap-1 overflow-hidden px-2 py-3">
          {items.map((item) => (
            <RailLink key={item.to} item={item} expanded={expanded} active={pathname.startsWith(item.to)} />
          ))}
        </div>

        <div className="flex flex-col gap-1 border-t border-hairline px-2 py-3">
          <div className={clsx('flex items-center', expanded ? 'justify-end' : 'justify-center')}>
            <ThemeToggle />
          </div>
          <RailLink
            item={{
              to: '/profile',
              label: me?.display_name ?? 'Profile',
              icon: () => (
                <span className="flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full bg-accent text-[9px] font-semibold text-accent-fg">
                  {initials(me?.display_name ?? '?')}
                </span>
              ),
            }}
            expanded={expanded}
            active={pathname.startsWith('/profile')}
          />
        </div>

        <LiveDot />
      </nav>
    </Tooltip.Provider>
  )
}
