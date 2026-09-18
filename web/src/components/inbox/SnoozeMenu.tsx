// Snooze dropdown: 10 min, 1 h, tomorrow 09:00. Controlled by the parent so
// the `s` keyboard shortcut can open the menu for the currently focused card.
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'

export type SnoozeOption = '10m' | '1h' | 'tomorrow'

const OPTIONS: Array<{ value: SnoozeOption; label: string }> = [
  { value: '10m', label: '10 min' },
  { value: '1h', label: '1 h' },
  { value: 'tomorrow', label: 'Tomorrow 09:00' },
]

/** Resolves a snooze option to the ISO timestamp it snoozes until. */
export function snoozeUntil(option: SnoozeOption, now = new Date()): string {
  if (option === '10m') return new Date(now.getTime() + 10 * 60_000).toISOString()
  if (option === '1h') return new Date(now.getTime() + 60 * 60_000).toISOString()
  const tomorrow = new Date(now)
  tomorrow.setDate(tomorrow.getDate() + 1)
  tomorrow.setHours(9, 0, 0, 0)
  return tomorrow.toISOString()
}

export function SnoozeMenu({
  open,
  onOpenChange,
  onSelect,
  disabled,
  buttonClassName,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (option: SnoozeOption) => void
  disabled?: boolean
  buttonClassName: string
}) {
  return (
    <DropdownMenu.Root open={open} onOpenChange={onOpenChange}>
      <DropdownMenu.Trigger asChild>
        <button type="button" disabled={disabled} className={buttonClassName}>
          Snooze
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="start"
          sideOffset={4}
          className="z-50 min-w-[160px] rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-1 shadow-2xl"
        >
          {OPTIONS.map((option) => (
            <DropdownMenu.Item
              key={option.value}
              onSelect={() => onSelect(option.value)}
              className="flex h-8 cursor-pointer items-center rounded-[var(--radius-1)] px-2 text-[13px] text-fg-primary outline-none data-[highlighted]:bg-surface-2"
            >
              {option.label}
            </DropdownMenu.Item>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
