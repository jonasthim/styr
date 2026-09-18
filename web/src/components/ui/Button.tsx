// The one button in Styr. Four variants and two sizes; everything else on a
// page is a composition of these. Colours come from tokens only.
//
// `buttonClasses` is exported separately so a router <Link> or a Radix
// trigger that must render its own element can wear the same skin without a
// wrapper element (see ApprovalCard's "Open session").
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'
import clsx from 'clsx'
import { Loader2 } from 'lucide-react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md'

const BASE =
  'inline-flex shrink-0 select-none items-center justify-center gap-1.5 whitespace-nowrap rounded-[var(--radius-control)] ' +
  'font-medium tracking-[-0.005em] no-underline outline-none ' +
  'transition-[background-color,border-color,color,box-shadow,transform] duration-[var(--duration-fast)] ' +
  'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)] ' +
  'active:translate-y-px disabled:pointer-events-none disabled:opacity-45 aria-disabled:pointer-events-none aria-disabled:opacity-45'

const SIZES: Record<ButtonSize, string> = {
  sm: 'h-7 px-2.5 text-[12px]',
  md: 'h-8 px-3 text-[13px]',
}

// The inset top highlight is what stops a flat fill reading as a coloured
// rectangle; hover changes the fill itself rather than dimming it with
// opacity, which is what made the old buttons look muted.
const VARIANTS: Record<ButtonVariant, string> = {
  primary:
    'bg-accent text-accent-fg shadow-[inset_0_1px_0_rgba(255,255,255,0.18),var(--shadow-card)] ' +
    'hover:bg-accent-hover active:bg-accent-active',
  secondary:
    'border border-strong bg-surface-2 text-fg-primary shadow-[var(--shadow-card)] ' +
    'hover:border-[var(--accent-border)] hover:bg-surface-3',
  ghost: 'text-fg-secondary hover:bg-surface-2 hover:text-fg-primary',
  // Tinted rather than filled: a destructive action in a settings card should
  // read as dangerous without out-shouting the page's real primary action.
  danger:
    'border border-state-failed/40 bg-state-failed/10 text-fg-danger ' +
    'hover:border-state-failed/60 hover:bg-state-failed/20',
}

export function buttonClasses(
  variant: ButtonVariant = 'secondary',
  size: ButtonSize = 'md',
  className?: string,
): string {
  return clsx(BASE, SIZES[size], VARIANTS[variant], className)
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  /** Swaps the leading icon for a spinner and blocks the click. */
  loading?: boolean
  /** Leading icon; sized by the caller (13–14px reads right at both sizes). */
  icon?: ReactNode
  /** Trailing icon, for things like a Cmd+Enter affordance. */
  iconRight?: ReactNode
  /** Stretch to the container, for stacked mobile action rows. */
  block?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading = false, icon, iconRight, block, className, children, disabled, type, ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type ?? 'button'}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={buttonClasses(variant, size, clsx(block && 'w-full', className))}
      {...rest}
    >
      {loading ? <Loader2 size={13} className="animate-spin" aria-hidden /> : icon}
      {children}
      {iconRight}
    </button>
  )
})
