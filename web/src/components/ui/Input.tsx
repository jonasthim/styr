// Text input. 32px tall to match a row and a md Button; the ring is the same
// 2px accent outline every other primitive uses.
import { forwardRef, type InputHTMLAttributes } from 'react'
import clsx from 'clsx'

export const inputClasses =
  'h-8 w-full rounded-[var(--radius-control)] border border-strong bg-surface-2 px-2.5 text-[13px] text-fg-primary ' +
  'shadow-[inset_0_1px_1px_rgba(0,0,0,0.12)] outline-none ' +
  'transition-[border-color,box-shadow] duration-[var(--duration-fast)] ' +
  'placeholder:text-fg-muted hover:border-[var(--accent-border)] ' +
  'focus-visible:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)] ' +
  'aria-[invalid=true]:border-state-failed disabled:cursor-not-allowed disabled:opacity-50'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  /** Mono face for paths, tokens and ids. */
  mono?: boolean
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ className, mono, ...rest }, ref) {
  return <input ref={ref} className={clsx(inputClasses, mono && 'font-mono text-[12px]', className)} {...rest} />
})
