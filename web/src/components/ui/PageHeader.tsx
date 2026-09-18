// One page title treatment everywhere: 20/600 with tight tracking, an
// optional one-line subtitle in fg-secondary, and a right-aligned action
// slot. Having every page share it is most of what fixes the flat hierarchy
// that came from every heading being 18px and every other line being 13px.
import type { ReactNode } from 'react'
import clsx from 'clsx'

export function PageHeader({
  title,
  description,
  actions,
  className,
}: {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}) {
  return (
    <div className={clsx('flex items-start justify-between gap-4', className)}>
      <div className="min-w-0">
        <h1 className="truncate text-[20px] font-semibold leading-7 tracking-[-0.02em] text-fg-primary">{title}</h1>
        {description && <p className="mt-1 max-w-[64ch] text-[13px] text-fg-secondary">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2 pt-0.5">{actions}</div>}
    </div>
  )
}
