// Multi-line input. Same skin as Input, but height comes from `rows` (or an
// autosize effect at the call site), so no fixed h-8 here.
import { forwardRef, type TextareaHTMLAttributes } from 'react'
import clsx from 'clsx'

export const textareaClasses =
  'w-full resize-none rounded-[var(--radius-control)] border border-strong bg-surface-2 px-2.5 py-2 text-[13px] leading-5 text-fg-primary ' +
  'shadow-[inset_0_1px_1px_rgba(0,0,0,0.12)] outline-none ' +
  'transition-[border-color,box-shadow] duration-[var(--duration-fast)] ' +
  'placeholder:text-fg-muted hover:border-[var(--accent-border)] ' +
  'focus-visible:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)] ' +
  'aria-[invalid=true]:border-state-failed disabled:cursor-not-allowed disabled:opacity-50'

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  mono?: boolean
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { className, mono, ...rest },
  ref,
) {
  return <textarea ref={ref} className={clsx(textareaClasses, mono && 'font-mono text-[12px]', className)} {...rest} />
})
