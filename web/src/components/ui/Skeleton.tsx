// Content-shaped loading block. Pulses opacity only, and the pulse is
// neutralised under prefers-reduced-motion by the global rule in base.css.
import clsx from 'clsx'

export function Skeleton({ className }: { className?: string }) {
  return <span aria-hidden className={clsx('block animate-pulse rounded-[var(--radius-1)] bg-surface-3', className)} />
}
