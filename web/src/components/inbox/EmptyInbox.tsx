// Calm empty state for the "Needs you" section once every approval has been
// decided. One secondary action, per the design system's "empty states with
// one action" table-stake.
import { useNavigate } from '@tanstack/react-router'

export function EmptyInbox() {
  const navigate = useNavigate()
  return (
    <div data-testid="empty-inbox" className="flex flex-col items-center gap-3 px-4 py-12 text-center">
      <p className="text-[13px] text-fg-secondary">Nothing needs you</p>
      <button
        type="button"
        onClick={() => void navigate({ to: '/sessions', search: { new: 1 } })}
        className="inline-flex h-8 items-center rounded-[var(--radius-1)] border border-hairline bg-surface-1 px-3 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2"
      >
        Start a session
      </button>
    </div>
  )
}
