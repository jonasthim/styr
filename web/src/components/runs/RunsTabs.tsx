// The three surfaces that answer "what has this box been doing?": the run
// list, the loops chaining runs together, and what all of it cost. They are
// destinations, not a filter, so they are links - the active one is filled
// the way a pressed segment is, matching TabsList elsewhere.
import { Link, useRouterState } from '@tanstack/react-router'
import clsx from 'clsx'

const ITEMS = [
  { to: '/runs', label: 'Runs', exact: true },
  { to: '/runs/loops', label: 'Loops', exact: false },
  { to: '/runs/costs', label: 'Cost', exact: false },
] as const

export function RunsTabs({ className }: { className?: string }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  return (
    <nav
      aria-label="Runs views"
      className={clsx(
        'inline-flex items-center gap-0.5 self-start rounded-[var(--radius-control)] border border-strong bg-surface-2 p-0.5',
        className,
      )}
    >
      {ITEMS.map((item) => {
        const active = item.exact ? pathname === item.to : pathname.startsWith(item.to)
        return (
          <Link
            key={item.to}
            to={item.to}
            aria-current={active ? 'page' : undefined}
            className={clsx(
              'flex h-7 shrink-0 items-center rounded-[var(--radius-1)] px-3 text-[12px] font-medium no-underline outline-none',
              'transition-colors duration-[var(--duration-fast)]',
              'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]',
              active
                ? 'bg-accent text-accent-fg shadow-[var(--shadow-card)]'
                : 'text-fg-secondary hover:text-fg-primary',
            )}
          >
            {item.label}
          </Link>
        )
      })}
    </nav>
  )
}
