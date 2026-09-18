// Toggle on @radix-ui/react-switch. The thumb moves with a transform only;
// the track changes colour. Used for per-workspace settings, where the label
// always sits outside the control.
import * as SwitchPrimitive from '@radix-ui/react-switch'
import clsx from 'clsx'

export function Switch({
  checked,
  onCheckedChange,
  disabled,
  id,
  className,
  ...aria
}: {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  id?: string
  className?: string
  'aria-label'?: string
}) {
  return (
    <SwitchPrimitive.Root
      id={id}
      checked={checked}
      disabled={disabled}
      onCheckedChange={onCheckedChange}
      aria-label={aria['aria-label']}
      className={clsx(
        'relative h-5 w-9 shrink-0 rounded-full border border-strong bg-surface-3 outline-none',
        'transition-colors duration-[var(--duration-fast)]',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]',
        'data-[state=checked]:border-transparent data-[state=checked]:bg-accent',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
    >
      <SwitchPrimitive.Thumb className="block h-3.5 w-3.5 translate-x-[3px] rounded-full bg-fg-secondary shadow-[var(--shadow-card)] transition-transform duration-[var(--duration-fast)] will-change-transform data-[state=checked]:translate-x-[18px] data-[state=checked]:bg-accent-fg" />
    </SwitchPrimitive.Root>
  )
}
