// Thin wrapper over @radix-ui/react-tooltip so a tooltip is one element at
// the call site. Providers nest safely in Radix, so this works both inside an
// existing provider (the rail) and on its own.
import * as TooltipPrimitive from '@radix-ui/react-tooltip'
import type { ReactElement, ReactNode } from 'react'

export function Tooltip({
  label,
  children,
  side = 'top',
  delay = 250,
}: {
  label: ReactNode
  children: ReactElement
  side?: 'top' | 'right' | 'bottom' | 'left'
  delay?: number
}) {
  return (
    <TooltipPrimitive.Provider delayDuration={delay}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content
            side={side}
            sideOffset={6}
            className="styr-panel z-50 rounded-[var(--radius-control)] border border-hairline bg-surface-3 px-2 py-1 text-[12px] text-fg-primary shadow-[var(--shadow-popover)]"
          >
            {label}
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  )
}
