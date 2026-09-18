// Modal dialog on @radix-ui/react-dialog. The overlay blurs and fades, the
// panel fades and lifts 4px over 160ms; both are transform/opacity only and
// both are neutralised by the reduced-motion rule in base.css.
//
// The panel is one surface step above the page (surface-2) with a hairline
// border and 24px of padding, and the footer is a real row separated from the
// body rather than buttons tacked onto the end of a form.
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import clsx from 'clsx'
import type { ComponentPropsWithoutRef, ReactNode } from 'react'

export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
export const DialogClose = DialogPrimitive.Close

export interface DialogContentProps extends Omit<ComponentPropsWithoutRef<typeof DialogPrimitive.Content>, 'title'> {
  title: ReactNode
  /** Rendered under the title in fg-secondary. Pass `srOnlyDescription` instead when the title says it all. */
  description?: ReactNode
  srOnlyDescription?: string
  /** Right-aligned action row. `footerLeft` takes the quiet half (a Kbd hint). */
  footer?: ReactNode
  footerLeft?: ReactNode
  /** Panel max width; the panel is always full width below it. */
  width?: number
  showClose?: boolean
}

export function DialogContent({
  title,
  description,
  srOnlyDescription,
  footer,
  footerLeft,
  width = 480,
  showClose = true,
  className,
  children,
  ...rest
}: DialogContentProps) {
  return (
    <DialogPrimitive.Portal>
      {/* Content lives inside the overlay so the overlay's own scroll and
          padding frame it on a 390px screen; Radix still treats a click on
          the overlay as "outside". */}
      <DialogPrimitive.Overlay className="styr-overlay fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-[var(--overlay)] p-4 backdrop-blur-[3px] sm:items-center sm:p-6">
        <DialogPrimitive.Content
          className={clsx(
            'styr-panel relative my-auto w-full rounded-[var(--radius-panel)] border border-hairline bg-surface-2 p-6',
            'shadow-[var(--shadow-dialog)] outline-none',
            className,
          )}
          style={{ maxWidth: width }}
          {...rest}
        >
          <DialogPrimitive.Title className="pr-7 text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
            {title}
          </DialogPrimitive.Title>
          {description ? (
            <DialogPrimitive.Description className="mt-1 max-w-[52ch] text-[13px] text-fg-secondary">
              {description}
            </DialogPrimitive.Description>
          ) : (
            <DialogPrimitive.Description className="sr-only">{srOnlyDescription ?? ''}</DialogPrimitive.Description>
          )}

          {showClose && (
            <DialogPrimitive.Close
              aria-label="Close"
              className="absolute right-4 top-4 flex h-7 w-7 items-center justify-center rounded-[var(--radius-control)] text-fg-muted outline-none transition-colors duration-[var(--duration-fast)] hover:bg-surface-3 hover:text-fg-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
            >
              <X size={14} aria-hidden />
            </DialogPrimitive.Close>
          )}

          <div className="mt-5">{children}</div>

          {(footer || footerLeft) && (
            <div className="mt-6 flex items-center justify-between gap-3 border-t border-hairline pt-4">
              <div className="min-w-0 text-[12px] text-fg-muted">{footerLeft}</div>
              <div className="flex shrink-0 items-center gap-2">{footer}</div>
            </div>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Overlay>
    </DialogPrimitive.Portal>
  )
}
