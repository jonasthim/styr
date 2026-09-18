// A key cap. Sized to sit inline in a 12px line of text without pushing it
// around; the inset highlight is what makes it read as a key rather than a
// bordered box.
import type { ReactNode } from 'react'
import clsx from 'clsx'

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={clsx(
        'inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[var(--radius-1)] border border-strong bg-surface-2 px-1',
        'font-mono text-[11px] font-medium leading-none text-fg-secondary shadow-[inset_0_-1px_0_rgba(0,0,0,0.25)]',
        className,
      )}
    >
      {children}
    </kbd>
  )
}

/** `⌘` on Apple hardware, `Ctrl` everywhere else, resolved once at load. */
export const MOD_KEY =
  typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent) ? '⌘' : 'Ctrl'
