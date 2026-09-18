// A surface-1 panel with a hairline border and the card shadow. `title`
// renders the standard 15/600 section head with an optional description and a
// right-aligned action slot; pass none of them for a bare container.
import type { ReactNode } from 'react'
import clsx from 'clsx'

export interface CardProps {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  /** Drop the padding for tables and lists that manage their own insets. */
  flush?: boolean
  className?: string
  children?: ReactNode
  'data-testid'?: string
}

export function Card({ title, description, actions, flush, className, children, ...rest }: CardProps) {
  const hasHead = Boolean(title || description || actions)
  return (
    <section
      className={clsx(
        'overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]',
        className,
      )}
      {...rest}
    >
      {hasHead && (
        <div
          className={clsx(
            'flex flex-wrap items-start justify-between gap-x-4 gap-y-3',
            flush ? 'p-4' : 'px-5 pt-5',
            // With no body below it the head *is* the card, so it closes
            // itself off rather than leaning on a body's padding.
            !flush && (children == null ? 'pb-5' : 'pb-0'),
          )}
        >
          <div className="min-w-0">
            {title && <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{title}</h2>}
            {description && <p className="mt-1 max-w-[60ch] text-[13px] text-fg-secondary">{description}</p>}
          </div>
          {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
        </div>
      )}
      {children != null && <div className={clsx(flush ? '' : 'p-5', hasHead && !flush && 'pt-4')}>{children}</div>}
    </section>
  )
}
