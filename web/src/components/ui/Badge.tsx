// Small status pill. `soft` tints the state colour for a label on a surface;
// `solid` fills it, for counts that have to be spotted at a glance (the inbox
// badge). Numbers inside a badge are tabular so a count never jitters.
import type { ReactNode } from 'react'
import clsx from 'clsx'

export type BadgeTone = 'neutral' | 'accent' | 'running' | 'attention' | 'failed'
export type BadgeVariant = 'soft' | 'solid' | 'outline'

const SOFT: Record<BadgeTone, string> = {
  neutral: 'border-hairline bg-surface-2 text-fg-secondary',
  accent: 'border-[var(--accent-border)] bg-accent-subtle text-accent',
  running: 'border-state-running/30 bg-state-running/10 text-state-running',
  attention: 'border-state-attention/30 bg-state-attention/10 text-state-attention',
  failed: 'border-state-failed/30 bg-state-failed/10 text-fg-danger',
}

const SOLID: Record<BadgeTone, string> = {
  neutral: 'border-transparent bg-surface-3 text-fg-primary',
  accent: 'border-transparent bg-accent text-accent-fg',
  running: 'border-transparent bg-state-running text-on-attention',
  attention: 'border-transparent bg-state-attention text-on-attention',
  failed: 'border-transparent bg-state-failed text-on-danger',
}

const OUTLINE: Record<BadgeTone, string> = {
  neutral: 'border-hairline bg-transparent text-fg-secondary',
  accent: 'border-[var(--accent-border)] bg-transparent text-accent',
  running: 'border-state-running/40 bg-transparent text-state-running',
  attention: 'border-state-attention/40 bg-transparent text-state-attention',
  failed: 'border-state-failed/40 bg-transparent text-fg-danger',
}

export interface BadgeProps {
  tone?: BadgeTone
  variant?: BadgeVariant
  /** Pill for counts, rounded rect for word labels. */
  pill?: boolean
  className?: string
  children: ReactNode
  'data-testid'?: string
  'aria-label'?: string
}

export function Badge({ tone = 'neutral', variant = 'soft', pill = true, className, children, ...rest }: BadgeProps) {
  const tones = variant === 'solid' ? SOLID : variant === 'outline' ? OUTLINE : SOFT
  return (
    <span
      className={clsx(
        'inline-flex shrink-0 items-center gap-1 border px-1.5 py-0.5 text-[11px] font-medium tabular-nums leading-4',
        pill ? 'rounded-full' : 'rounded-[var(--radius-1)]',
        tones[tone],
        className,
      )}
      {...rest}
    >
      {children}
    </span>
  )
}
