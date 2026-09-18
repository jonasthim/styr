// The three admin tables in Styr share one skin: a surface-2 header strip in
// 12/500 fg-secondary, hairline row rules, and a horizontal scroller so a
// wide table never pushes the page sideways on a phone.
import type { ReactNode, ThHTMLAttributes, TdHTMLAttributes } from 'react'
import clsx from 'clsx'

export function TableFrame({
  minWidth = 560,
  className,
  children,
}: {
  minWidth?: number
  className?: string
  children: ReactNode
}) {
  return (
    <div
      className={clsx(
        'overflow-x-auto rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]',
        className,
      )}
    >
      <table className="w-full border-collapse text-left" style={{ minWidth }}>
        {children}
      </table>
    </div>
  )
}

export function Th({ className, children, ...rest }: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={clsx(
        'border-b border-hairline bg-surface-2 px-3 py-2 text-[12px] font-medium tracking-[-0.005em] text-fg-secondary',
        className,
      )}
      {...rest}
    >
      {children}
    </th>
  )
}

export function Td({ className, children, ...rest }: TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td className={clsx('px-3 py-2.5 align-middle text-[13px] text-fg-primary', className)} {...rest}>
      {children}
    </td>
  )
}

export function Tr({ className, children, ...rest }: React.HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr
      className={clsx(
        'border-b border-hairline transition-colors duration-[var(--duration-fast)] last:border-b-0 hover:bg-surface-2/60',
        className,
      )}
      {...rest}
    >
      {children}
    </tr>
  )
}
