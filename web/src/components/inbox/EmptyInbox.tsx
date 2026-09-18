// Calm empty state for the "Needs you" section once every approval has been
// decided. One secondary action, per the design system's "empty states with
// one action" table-stake.
import { useNavigate } from '@tanstack/react-router'
import { CheckCheck } from 'lucide-react'
import { Button, EmptyState } from '../ui'

export function EmptyInbox() {
  const navigate = useNavigate()
  return (
    <EmptyState
      data-testid="empty-inbox"
      className="rounded-[var(--radius-panel)] border border-hairline bg-surface-1 py-12 shadow-[var(--shadow-card)]"
      icon={<CheckCheck size={18} aria-hidden />}
      title="Nothing needs you"
      description="Every permission prompt has been answered. New ones land here the moment an agent asks."
      action={<Button onClick={() => void navigate({ to: '/sessions', search: { new: 1 } })}>Start a session</Button>}
    />
  )
}
