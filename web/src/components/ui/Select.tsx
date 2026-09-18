// Select built on @radix-ui/react-select, replacing the native <select> whose
// OS-drawn popup and stray focus bar were the most template-looking thing in
// the app. Trigger reads as a control (strong border, chevron that rotates on
// open); the popover floats on surface-3 with a hairline border and the
// popover shadow. Full keyboard nav, type-ahead and Escape come from Radix.
import * as SelectPrimitive from '@radix-ui/react-select'
import { Check, ChevronDown } from 'lucide-react'
import clsx from 'clsx'

export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

export interface SelectProps {
  value: string
  onValueChange: (value: string) => void
  options: SelectOption[]
  placeholder?: string
  disabled?: boolean
  id?: string
  name?: string
  /** Use where no visible <Field> label is associated with the trigger. */
  'aria-label'?: string
  'aria-describedby'?: string
  className?: string
  /** Mono face for values that are paths or identifiers. */
  mono?: boolean
}

const triggerClasses =
  'group inline-flex h-8 w-full items-center justify-between gap-2 rounded-[var(--radius-control)] border border-strong bg-surface-2 px-2.5 ' +
  'text-[13px] text-fg-primary shadow-[inset_0_1px_1px_rgba(0,0,0,0.12)] outline-none ' +
  'transition-[border-color,background-color] duration-[var(--duration-fast)] ' +
  'hover:border-[var(--accent-border)] hover:bg-surface-3 ' +
  'data-[state=open]:border-accent ' +
  'focus-visible:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)] ' +
  'data-[placeholder]:text-fg-muted ' +
  'disabled:cursor-not-allowed disabled:border-hairline disabled:bg-transparent disabled:text-fg-secondary disabled:opacity-70'

export function Select({
  value,
  onValueChange,
  options,
  placeholder = 'Select…',
  disabled,
  id,
  name,
  className,
  mono,
  ...aria
}: SelectProps) {
  return (
    <SelectPrimitive.Root value={value || undefined} onValueChange={onValueChange} disabled={disabled} name={name}>
      <SelectPrimitive.Trigger
        id={id}
        aria-label={aria['aria-label']}
        aria-describedby={aria['aria-describedby']}
        className={clsx(triggerClasses, mono && 'font-mono text-[12px]', className)}
      >
        <span className="min-w-0 truncate text-left">
          <SelectPrimitive.Value placeholder={placeholder} />
        </span>
        <SelectPrimitive.Icon asChild>
          <ChevronDown
            size={14}
            aria-hidden
            className="shrink-0 text-fg-muted transition-transform duration-[var(--duration-fast)] group-data-[state=open]:rotate-180"
          />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>

      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={6}
          className="styr-panel z-50 max-h-[min(320px,var(--radix-select-content-available-height))] min-w-[var(--radix-select-trigger-width)] overflow-hidden rounded-[var(--radius-2)] border border-hairline bg-surface-3 shadow-[var(--shadow-popover)]"
        >
          <SelectPrimitive.Viewport className="p-1">
            {options.map((option) => (
              <SelectPrimitive.Item
                key={option.value}
                value={option.value}
                disabled={option.disabled}
                className={clsx(
                  'relative flex h-8 cursor-pointer select-none items-center gap-2 rounded-[var(--radius-control)] py-0 pl-2 pr-7 text-[13px] text-fg-primary outline-none',
                  'data-[highlighted]:bg-accent-subtle data-[highlighted]:text-fg-primary',
                  'data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
                  mono && 'font-mono text-[12px]',
                )}
              >
                <SelectPrimitive.ItemText>{option.label}</SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="absolute right-2 flex items-center">
                  <Check size={13} aria-hidden className="text-accent" />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
}
