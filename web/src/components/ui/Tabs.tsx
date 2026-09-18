// Segmented control on @radix-ui/react-tabs. Used where a control picks
// between a few equal-weight options that each show different fields below
// them (the "Add workspace" source picker), not for switching between
// destinations - so the active segment reads as pressed (accent fill)
// rather than as a page tab (underline).
import * as TabsPrimitive from '@radix-ui/react-tabs'
import clsx from 'clsx'
import type { ComponentPropsWithoutRef } from 'react'

export const Tabs = TabsPrimitive.Root
export const TabsContent = TabsPrimitive.Content

export function TabsList({ className, ...rest }: ComponentPropsWithoutRef<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={clsx(
        'inline-flex items-center gap-0.5 self-start rounded-[var(--radius-control)] border border-strong bg-surface-2 p-0.5',
        className,
      )}
      {...rest}
    />
  )
}

export function TabsTrigger({ className, ...rest }: ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={clsx(
        'h-7 shrink-0 rounded-[var(--radius-1)] px-3 text-[12px] font-medium text-fg-secondary outline-none',
        'transition-colors duration-[var(--duration-fast)] hover:text-fg-primary',
        'data-[state=active]:bg-accent data-[state=active]:text-accent-fg data-[state=active]:shadow-[var(--shadow-card)]',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]',
        className,
      )}
      {...rest}
    />
  )
}
