// Toast stack on @radix-ui/react-toast. One Toaster mounts once (Shell.tsx);
// components never render a Toast themselves - they push onto the queue via
// useToast() (hooks/useToast.ts) instead. Slides/fades in from the bottom
// right on desktop, full-width from the bottom on phone; transform/opacity
// only, matching every other Styr overlay.
import * as ToastPrimitive from '@radix-ui/react-toast'
import { AlertTriangle, Check, X } from 'lucide-react'
import clsx from 'clsx'
import type { ReactNode } from 'react'
import { useToastStore, type ToastItem } from '../../store/toast'

const TONE_ICON: Record<NonNullable<ToastItem['tone']>, ReactNode> = {
  neutral: null,
  success: <Check size={14} aria-hidden className="text-state-running" />,
  danger: <AlertTriangle size={14} aria-hidden className="text-fg-danger" />,
}

function ToastRow({ item }: { item: ToastItem }) {
  const dismiss = useToastStore((s) => s.dismiss)
  return (
    <ToastPrimitive.Root
      data-testid="toast"
      duration={4000}
      onOpenChange={(open) => {
        if (!open) dismiss(item.id)
      }}
      className={clsx(
        'styr-panel pointer-events-auto flex w-[min(360px,calc(100vw-2rem))] items-start gap-2.5 rounded-[var(--radius-panel)] border border-hairline bg-surface-2 p-3.5 shadow-[var(--shadow-popover)]',
      )}
    >
      {TONE_ICON[item.tone ?? 'neutral']}
      <div className="min-w-0 flex-1">
        <ToastPrimitive.Title className="text-[13px] font-medium text-fg-primary">{item.title}</ToastPrimitive.Title>
        {item.description && (
          <ToastPrimitive.Description className="mt-0.5 text-[12px] text-fg-secondary">
            {item.description}
          </ToastPrimitive.Description>
        )}
      </div>
      <ToastPrimitive.Close
        aria-label="Dismiss"
        className="shrink-0 rounded-[var(--radius-1)] p-0.5 text-fg-muted outline-none transition-colors duration-[var(--duration-fast)] hover:bg-surface-3 hover:text-fg-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
      >
        <X size={13} aria-hidden />
      </ToastPrimitive.Close>
    </ToastPrimitive.Root>
  )
}

export function Toaster() {
  const toasts = useToastStore((s) => s.toasts)
  return (
    <ToastPrimitive.Provider swipeDirection="right">
      {toasts.map((item) => (
        <ToastRow key={item.id} item={item} />
      ))}
      <ToastPrimitive.Viewport className="fixed bottom-0 right-0 z-[60] m-0 flex w-full flex-col gap-2 p-4 outline-none sm:w-auto" />
    </ToastPrimitive.Provider>
  )
}
