// An empty screen is an invitation to act: a 40px icon tile, one line of
// title, one line saying what to do, one primary action. It centres itself in
// whatever box it is given, so the caller makes that box the content area
// rather than a bordered strip at the top of the page.
import type { ReactNode } from 'react'
import clsx from 'clsx'

export interface EmptyStateProps {
  icon?: ReactNode
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  className?: string
  'data-testid'?: string
}

export function EmptyState({ icon, title, description, action, className, ...rest }: EmptyStateProps) {
  return (
    <div
      className={clsx('flex flex-1 flex-col items-center justify-center px-6 py-16 text-center', className)}
      {...rest}
    >
      {icon && (
        <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-[10px] border border-hairline bg-surface-2 text-fg-secondary shadow-[var(--shadow-card)]">
          {icon}
        </div>
      )}
      <p className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{title}</p>
      {description && <p className="mt-1.5 max-w-[42ch] text-[13px] text-fg-secondary">{description}</p>}
      {action && <div className="mt-5 flex items-center gap-2">{action}</div>}
    </div>
  )
}
